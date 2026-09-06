package bitbucket

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const apiBase = "/rest/api/1.0"

type Client struct {
	baseURL string
	token   string
	http    *http.Client
	user    string
}

type Options struct {
	BaseURL string
	Token   string
	CAFile  string
}

func NewClient(opts Options) (*Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()

	// Corporate instances commonly terminate TLS with an internally-issued
	// certificate. Adding the CA is the supported fix; there is deliberately no
	// option to skip verification.
	if opts.CAFile != "" {
		pem, err := os.ReadFile(opts.CAFile)
		if err != nil {
			return nil, fmt.Errorf("reading ca_file: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("ca_file %s contains no usable certificates", opts.CAFile)
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}

	return &Client{
		baseURL: strings.TrimRight(opts.BaseURL, "/"),
		token:   opts.Token,
		http:    &http.Client{Timeout: 30 * time.Second, Transport: transport},
	}, nil
}

// APIError carries the status and Bitbucket's own message, which is far more
// actionable than the status line alone.
type APIError struct {
	Status  int
	Path    string
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("bitbucket %d on %s: %s", e.Status, e.Path, e.Message)
	}
	return fmt.Sprintf("bitbucket %d on %s", e.Status, e.Path)
}

func (c *Client) do(method, path string, query url.Values, body, out any) error {
	full := c.baseURL + apiBase + path
	if len(query) > 0 {
		full += "?" + query.Encode()
	}

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequest(method, full, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	// Bitbucket echoes the authenticated username on every response. It is the
	// only way to learn the current user's slug on the 1.0 API — there is no
	// /myself endpoint — and the approve path needs it.
	if u := resp.Header.Get("X-AUSERNAME"); u != "" && u != "anonymous" {
		c.user = u
	}

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 400 {
		return &APIError{Status: resp.StatusCode, Path: path, Message: firstErrorMessage(payload)}
	}
	if out == nil || len(payload) == 0 {
		return nil
	}
	return json.Unmarshal(payload, out)
}

func firstErrorMessage(payload []byte) string {
	var envelope struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(payload, &envelope); err == nil && len(envelope.Errors) > 0 {
		return envelope.Errors[0].Message
	}
	return strings.TrimSpace(string(payload))
}

type page struct {
	Size          int             `json:"size"`
	Limit         int             `json:"limit"`
	IsLastPage    bool            `json:"isLastPage"`
	NextPageStart int             `json:"nextPageStart"`
	Values        json.RawMessage `json:"values"`
}

// paged walks Bitbucket's start/limit pagination and reports whether rows were
// left behind.
//
// A nil limit walks to the last page. That is how a caller says "every row",
// and it is a separate answer from any number, because no number expresses it
// and the caller that needs it — finding the pull request for a branch — has no
// count it could pick. A limit of zero collects nothing and issues no request,
// since asking the server for rows the caller does not want spends a round trip
// on an answer that gets discarded.
//
// The bool is the server's own stop reason carried up. Whoever renders rows
// cannot tell a full page from the end of the data, and re-deriving it from the
// row count is wrong in exactly the case where the count equals the limit.
//
// collect reports how many rows the page held and how many it kept, which are
// different on the page the limit lands in the middle of.
func (c *Client) paged(
	path string,
	query url.Values,
	limit *int,
	collect func(json.RawMessage) (held, kept int, err error),
) (bool, error) {
	if limit != nil && *limit <= 0 {
		return false, nil
	}
	if query == nil {
		query = url.Values{}
	}
	pageSize := 25
	if limit != nil && *limit < pageSize {
		pageSize = *limit
	}

	got := 0
	start := 0
	for {
		query.Set("start", strconv.Itoa(start))
		query.Set("limit", strconv.Itoa(pageSize))

		var p page
		if err := c.do(http.MethodGet, path, query, nil, &p); err != nil {
			return false, err
		}
		held, kept, err := collect(p.Values)
		if err != nil {
			return false, err
		}
		got += kept

		if limit != nil && got >= *limit {
			// The limit stopped the walk. Rows are left behind when this page
			// held more than was kept, or when the server has further pages.
			return kept < held || !p.IsLastPage, nil
		}
		if p.IsLastPage || held == 0 {
			return false, nil
		}
		start = p.NextPageStart
	}
}

// Whoami returns the authenticated username, issuing a cheap request if no
// response has populated it yet.
func (c *Client) Whoami() (string, error) {
	if c.user != "" {
		return c.user, nil
	}
	q := url.Values{"limit": []string{"1"}}
	if err := c.do(http.MethodGet, "/inbox/pull-requests", q, nil, nil); err != nil {
		return "", err
	}
	if c.user == "" {
		return "", fmt.Errorf("bitbucket did not identify the authenticated user; check the token")
	}
	return c.user, nil
}
