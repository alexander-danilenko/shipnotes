// Package issue is the issue domain: the Issue entity and the parsing of issue
// keys. It is deliberately free of any Jira-API or JSON detail — the Jira
// adapter in the infrastructure layer maps the API response onto this clean
// entity, so the rest of the program never depends on Jira's wire format.
package issue

import (
	"fmt"
	"regexp"
	"strings"
)

// Issue is a single tracked issue, reduced to the fields the release notes use.
// A missing title or status is represented by an empty string; callers decide
// how to display that (the shipnotes builder shows "Unknown").
type Issue struct {
	// Key is the issue key, e.g. "CX-123".
	Key string
	// Title is the issue summary/title, or "" when it could not be loaded.
	Title string
	// Status is the workflow status name (e.g. "Done"), or "" when unknown.
	Status string
}

// keyFormat is the accepted shape of a single issue key, e.g. "CX-123": two or
// more letters, a dash, then digits. Unlike the loose extraction pattern used on
// commit messages, this is anchored, because it validates a whole key.
var keyFormat = regexp.MustCompile(`^[A-Za-z]{2,}-\d+$`)

// ParseIDs splits a comma-separated string into validated issue keys. Blank
// entries are ignored. It returns an error naming the first key that does not
// match the expected PROJECT-123 format.
func ParseIDs(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return []string{}, nil
	}

	keys := []string{}

	for part := range strings.SplitSeq(raw, ",") {
		key := strings.TrimSpace(part)
		if key == "" {
			continue
		}

		if !keyFormat.MatchString(key) {
			return nil, fmt.Errorf(
				`invalid Jira issue key format: %q. Expected format: PROJECT-123`, key,
			)
		}

		keys = append(keys, key)
	}

	return keys, nil
}
