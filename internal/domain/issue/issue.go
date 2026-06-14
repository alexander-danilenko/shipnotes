// Package issue is the issue domain: the Issue entity. It is deliberately free
// of any Jira-API or JSON detail — the Jira adapter in the infrastructure layer
// maps the API response onto this clean entity, so the rest of the program never
// depends on Jira's wire format.
package issue

// Issue is a single tracked issue, reduced to the fields the release notes use.
// A missing title or status is represented by an empty string; callers decide
// how to display that (the shipnotes builder shows "Unknown").
type Issue struct {
	// Key is the issue key, e.g. "PROJ-123".
	Key string
	// Title is the issue summary/title, or "" when it could not be loaded.
	Title string
	// Status is the workflow status name (e.g. "Done"), or "" when unknown.
	Status string
}
