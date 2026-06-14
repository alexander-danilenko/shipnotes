package notes

import (
	"fmt"
	"regexp"
	"strings"
)

// StatusMatcher decides whether an issue status should be rendered as a
// completed ("checked") checklist item in the release summary. It wraps a
// compiled, case-insensitive regular expression that the whole status text must
// match.
//
// The grouping of issues by status stays status-neutral (the tool has no built-in
// notion of which status means "done"); this matcher is the one, opt-in place
// where the caller may name the statuses that count as finished. The zero value —
// and a matcher built from an empty pattern — matches nothing, so every checkbox
// stays unchecked and the default output remains status-neutral.
type StatusMatcher struct {
	pattern *regexp.Regexp
}

// NewStatusMatcher compiles pattern into a case-insensitive, fully-anchored
// matcher: the entire status must match, so "done|ready for release" checks a
// status of "Done" or "Ready for Release" but not "Almost Done". Alternation and
// any other regular-expression syntax work as usual.
//
// An empty (or all-whitespace) pattern yields a matcher that never matches, which
// disables checking. A malformed pattern returns an error so the caller can
// report it before doing any other work.
func NewStatusMatcher(pattern string) (StatusMatcher, error) {
	if strings.TrimSpace(pattern) == "" {
		return StatusMatcher{}, nil
	}

	// "(?i)" makes the match case-insensitive; "^(?:...)$" anchors it to the whole
	// status so a pattern matches a complete status name, not a substring of one.
	compiled, err := regexp.Compile("(?i)^(?:" + pattern + ")$")
	if err != nil {
		return StatusMatcher{}, fmt.Errorf("invalid --checked-statuses pattern %q: %w", pattern, err)
	}

	return StatusMatcher{pattern: compiled}, nil
}

// Matches reports whether status counts as a completed ("checked") item.
func (m StatusMatcher) Matches(status string) bool {
	return m.pattern != nil && m.pattern.MatchString(status)
}
