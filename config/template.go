package config

// URLPlaceholder stands in for the instance URL wherever bbkt has to write a
// config without being told one.
const URLPlaceholder = "https://bitbucket.example.com"

// starterTemplate is what `bbkt config init` writes: the two fields with no
// working default, and nothing to delete. The annotated version is reference
// material — Example, printed by `bbkt config example` — because a scaffold you
// have to strip most of is worse than a short one you have to add to.
const starterTemplate = `%s
token_file = %q
`

// Example is the fully annotated config, every option exercised. Printed rather
// than written, so it can be read alongside the real file while editing it.
const Example = `# bbkt reads this file from $XDG_CONFIG_HOME/bbkt/config.toml
# (~/.config/bbkt/config.toml unless XDG_CONFIG_HOME says otherwise).
#
# Every field can be overridden per-invocation by the environment variable named
# beside it. ` + "`bbkt config show`" + ` reports which input won for each one.

# The Bitbucket Data Center base URL: scheme and host only, no /rest path and no
# trailing slash. Override: BBKT_URL
url = "https://bitbucket.corp.example"

# A file holding an HTTP access token, mode 0600. Create the token in Bitbucket
# under: profile picture -> Manage account -> HTTP access tokens.
#
# It must be a personal token. Project- and repository-scoped tokens cannot merge
# a pull request, because a merge creates a commit and that needs a real user
# identity.
#
# There is deliberately no --token flag: a flag value lands in ps output and
# shell history. Override: BBKT_TOKEN_FILE for the path, or BBKT_TOKEN to supply
# the token itself for one invocation.
token_file = "~/.config/bbkt/token"

# The branch ` + "`bbkt pr create`" + ` targets when --target is not given. Set this to
# whatever your instance actually merges into — a Data Center instance running
# gitflow usually wants "develop" here, not "main".
default_target_branch = "main"

# Only when the instance terminates TLS with an internally-issued certificate.
# The CA is added to the system pool for bbkt's requests; there is deliberately
# no option to skip verification. Override: BBKT_CA_FILE
# ca_file = "/etc/ssl/certs/corp-root-ca.pem"
`
