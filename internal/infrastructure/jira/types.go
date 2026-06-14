// Package jira fetches issue details from the Jira REST API. It is the adapter
// that implements the domain's issue.Provider port: it speaks Jira's wire format
// here and maps every response onto the clean issue.Issue entity, so nothing
// outside this package depends on Jira's JSON shape.
package jira

// apiIssue is a single issue as Jira's API v3 returns it. The JSON tags match
// the API; any fields we do not list are ignored when decoding.
type apiIssue struct {
	Key    string    `json:"key"`
	ID     string    `json:"id"`
	Fields apiFields `json:"fields"`
}

// apiFields holds the subset of issue fields the release notes use. The summary
// is the issue title; the status drives the commit status and the release
// summary.
type apiFields struct {
	Summary string     `json:"summary"`
	Status  *apiStatus `json:"status"`
}

// apiStatus is the workflow status of an issue (e.g. "Done").
type apiStatus struct {
	Name string `json:"name"`
}

// apiErrorResponse is the body Jira returns on a failed request. The search
// endpoint reports an invalid JQL query here: the human-readable reasons arrive
// in errorMessages (e.g. "Error in the JQL Query: ..."), with per-field problems
// in errors and softer notes in warningMessages. We surface these so the user
// sees Jira's own explanation instead of a raw JSON blob.
type apiErrorResponse struct {
	ErrorMessages   []string          `json:"errorMessages"`
	WarningMessages []string          `json:"warningMessages"`
	Errors          map[string]string `json:"errors"`
}
