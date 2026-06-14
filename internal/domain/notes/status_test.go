package notes_test

import (
	"testing"

	"github.com/alexander-danilenko/shipnotes/internal/domain/notes"
)

func TestStatusMatcher(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		status  string
		want    bool
	}{
		// The default pattern, exercised against real-world status names.
		{"default matches Done case-insensitively", "done|ready to release|ready for release", "Done", true},
		{"default matches Ready for Release", "done|ready to release|ready for release", "Ready for Release", true},
		{"default matches lower-case alternative", "done|ready to release|ready for release", "ready to release", true},
		{"default does not match In Progress", "done|ready to release|ready for release", "In Progress", false},

		// Anchoring: the whole status must match, not a substring of it.
		{"anchored: exact match", "done", "Done", true},
		{"anchored: substring does not match", "done", "Almost Done", false},
		{"anchored: prefix does not match", "done", "Done Deal", false},

		// An empty (or whitespace) pattern disables checking entirely.
		{"empty pattern matches nothing", "", "Done", false},
		{"whitespace pattern matches nothing", "   ", "Done", false},

		// An empty status never matches a non-empty pattern.
		{"empty status", "done", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			matcher, err := notes.NewStatusMatcher(tc.pattern)
			if err != nil {
				t.Fatalf("NewStatusMatcher(%q): unexpected error: %v", tc.pattern, err)
			}

			if got := matcher.Matches(tc.status); got != tc.want {
				t.Errorf("Matches(%q) with pattern %q: got %v, want %v", tc.status, tc.pattern, got, tc.want)
			}
		})
	}
}

// A malformed regular expression is reported as an error so the CLI can fail
// fast instead of silently checking nothing.
func TestStatusMatcherRejectsBadRegexp(t *testing.T) {
	if _, err := notes.NewStatusMatcher("("); err == nil {
		t.Error("expected an error for an unbalanced pattern, got nil")
	}
}

// The zero value is a valid matcher that checks nothing, so callers that do not
// configure --checked-statuses need no special handling.
func TestStatusMatcherZeroValueMatchesNothing(t *testing.T) {
	var matcher notes.StatusMatcher
	if matcher.Matches("Done") {
		t.Error("zero-value matcher should match nothing")
	}
}
