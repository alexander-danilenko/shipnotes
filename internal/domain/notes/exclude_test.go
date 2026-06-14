package notes_test

import (
	"testing"

	"github.com/alexander-danilenko/shipnotes/internal/domain/commit"
	"github.com/alexander-danilenko/shipnotes/internal/domain/notes"
)

func TestCommitMatcher(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		commit  commit.Commit
		want    bool
	}{
		// Excluding by message: a conventional-commit type prefix. Unlike the status
		// matcher this is unanchored, so the pattern matches the start of the subject.
		{"type prefix matches", `^(chore|docs):`, commit.Commit{Topic: "chore: tidy up"}, true},
		{"type prefix is case-insensitive", `^chore:`, commit.Commit{Topic: "CHORE: tidy up"}, true},
		{"non-matching type is kept", `^(chore|docs):`, commit.Commit{Topic: "feat: add login"}, false},

		// Excluding by ticket: the key lives in the subject, so the pattern matches
		// it there.
		{"key in subject matches", "PROJ-42", commit.Commit{Topic: "fix: thing PROJ-42"}, true},
		{"different key is kept", "PROJ-42", commit.Commit{Topic: "fix: thing PROJ-99"}, false},
		// Unanchored, so a key prefix over-matches; word boundaries make it exact.
		{"unanchored key over-matches longer key", "PROJ-42", commit.Commit{Topic: "fix PROJ-420"}, true},
		{"word-anchored key is exact", `\bPROJ-42\b`, commit.Commit{Topic: "fix PROJ-420"}, false},

		// An empty (or whitespace) pattern excludes nothing.
		{"empty pattern matches nothing", "", commit.Commit{Topic: "chore: tidy"}, false},
		{"whitespace pattern matches nothing", "   ", commit.Commit{Topic: "chore: tidy"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			matcher, err := notes.NewCommitMatcher(tc.pattern)
			if err != nil {
				t.Fatalf("NewCommitMatcher(%q): unexpected error: %v", tc.pattern, err)
			}

			if got := matcher.Matches(tc.commit); got != tc.want {
				t.Errorf("Matches(%+v) with pattern %q: got %v, want %v", tc.commit, tc.pattern, got, tc.want)
			}
		})
	}
}

// A malformed regular expression is reported as an error so the CLI can fail
// fast instead of silently excluding nothing.
func TestCommitMatcherRejectsBadRegexp(t *testing.T) {
	if _, err := notes.NewCommitMatcher("("); err == nil {
		t.Error("expected an error for an unbalanced pattern, got nil")
	}
}

// The zero value is a valid matcher that excludes nothing, so callers that do
// not configure --exclude-commits need no special handling.
func TestCommitMatcherZeroValueMatchesNothing(t *testing.T) {
	var matcher notes.CommitMatcher
	if matcher.Matches(commit.Commit{Topic: "chore: tidy"}) {
		t.Error("zero-value matcher should match nothing")
	}
}
