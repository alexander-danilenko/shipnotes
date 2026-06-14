package notes

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/alexander-danilenko/shipnotes/internal/domain/commit"
)

// CommitMatcher decides whether a commit should be excluded from the release
// notes. It wraps a compiled, case-insensitive regular expression that the
// caller supplies via --exclude-commits and tests against the commit subject.
// The subject already contains the commit's Jira key (keys are parsed from it),
// so one pattern lets a release manager drop noise by message — e.g.
// "^(chore|docs|test):" — or by ticket — e.g. "CX-42". Excluded commits are not
// deleted: they are listed in their own "Excluded commits" section so the notes
// stay auditable.
//
// Like StatusMatcher this is the caller's one opt-in opinion, and the zero value
// (and a matcher built from an empty pattern) matches nothing, so by default no
// commit is excluded and the output is unchanged. Unlike StatusMatcher, the
// pattern is NOT anchored: it matches anywhere in the text, because a commit is
// filtered by a prefix or substring (a "chore:" subject) rather than by a whole
// value.
type CommitMatcher struct {
	pattern *regexp.Regexp
}

// NewCommitMatcher compiles pattern into a case-insensitive, unanchored matcher.
// An empty (or all-whitespace) pattern yields a matcher that never matches,
// which leaves every commit in the notes. A malformed pattern returns an error
// so the caller can report it before doing any other work.
func NewCommitMatcher(pattern string) (CommitMatcher, error) {
	if strings.TrimSpace(pattern) == "" {
		return CommitMatcher{}, nil
	}

	// "(?i)" makes the match case-insensitive. There is no "^...$" anchor: the
	// pattern may match anywhere in the subject or a key, so "chore" matches a
	// "chore: tidy up" subject without the caller having to write ".*".
	compiled, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return CommitMatcher{}, fmt.Errorf("invalid --exclude-commits pattern %q: %w", pattern, err)
	}

	return CommitMatcher{pattern: compiled}, nil
}

// Matches reports whether the commit should be excluded. It tests the pattern
// against the commit subject, which covers both "exclude by message" (a "chore:"
// prefix) and "exclude by ticket" (the subject carries the Jira key). Because
// the match is unanchored, "CX-42" also matches "CX-420"; anchor with word
// boundaries (`\bCX-42\b`) to target one exact key.
func (m CommitMatcher) Matches(c commit.Commit) bool {
	return m.pattern != nil && m.pattern.MatchString(c.Topic)
}
