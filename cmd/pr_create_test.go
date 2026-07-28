package cmd

import "testing"

func TestTitleFromBranch(t *testing.T) {
	tests := []struct {
		branch string
		want   string
	}{
		{"PROJ-123-fix-the-parser", "PROJ-123 fix the parser"},
		{"feature/PROJ-123-fix-the-parser", "PROJ-123 fix the parser"},
		{"bugfix/DATA-7_null_handling", "DATA-7 null handling"},
		{"proj-123-lowercase-key", "PROJ-123 lowercase key"},
		{"chris/feature/PROJ-9-nested-prefix", "PROJ-9 nested prefix"},
		{"DATA-7", "DATA-7"},
		{"no-ticket-branch", "no ticket branch"},
		{"single", "single"},
	}

	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			if got := titleFromBranch(tt.branch); got != tt.want {
				t.Errorf("titleFromBranch(%q) = %q, want %q", tt.branch, got, tt.want)
			}
		})
	}
}
