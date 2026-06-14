// Package notes is the shipnotes domain: the data model that describes a set
// of release notes and the builder that assembles it from commits and issues. It
// depends on the commit and issue domains and on the report and renderer ports —
// never on git, Jira, the filesystem, or the terminal.
package notes

// Coordinates are the repository web addresses used to build links in the notes.
// They are a small domain value object so the builder never has to import the
// infrastructure config package; the caller maps its settings onto this.
type Coordinates struct {
	// GithubBaseURL is the repository's web base, e.g. https://github.com/acme/widgets.
	GithubBaseURL string
	// JiraBaseURL is the Jira site base, e.g. https://acme.atlassian.net.
	JiraBaseURL string
}

// ReleaseNotes is everything the Markdown template needs. The JSON tags double
// as the on-disk shape of the golden-test fixtures in testdata/cases, so the
// template and the tests agree on one set of field names.
type ReleaseNotes struct {
	Header  HeaderData   `json:"header"`
	Summary *SummaryData `json:"summary"`
	Commits []CommitView `json:"commits"`
	Authors []string     `json:"authors"`
}

// HeaderData is the information shown at the top of the notes.
type HeaderData struct {
	Date       string `json:"date"`
	Repository string `json:"repository"`
}

// CommitView is one commit prepared for display. The same shape is used in the
// commit history table and in the "Reverted commits" and "Reapplied commits"
// lists.
type CommitView struct {
	Hash           string `json:"hash"`
	URL            string `json:"url"`
	FormattedTopic string `json:"formattedTopic"`
	Authors        string `json:"authors"`
	// Status is the issue status text for the commit's primary issue, or a
	// placeholder ("No Issue", "Unknown") when there is none to show.
	Status string `json:"status"`
	// JiraIssueKey is empty when the commit has no associated issue.
	JiraIssueKey string `json:"jiraIssueKey"`
	JiraIssueURL string `json:"jiraIssueUrl"`
}

// IssueView is one issue prepared for the release summary.
type IssueView struct {
	Key    string `json:"key"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Status string `json:"status"`
}

// StatusGroup is a set of issues that share the same status.
type StatusGroup struct {
	Status string      `json:"status"`
	Issues []IssueView `json:"issues"`
}

// SummaryData is the optional "Release summary" section. It is only produced
// when there are release issues to show.
type SummaryData struct {
	// ByStatus: expected issues that appeared in commits, grouped by their status
	// and sorted alphabetically by status name. The tool makes no judgment about
	// which statuses count as "done" — every status is just a group — so the same
	// output works on any workflow.
	ByStatus []StatusGroup `json:"byStatus"`
	// Missing: expected issues that never appeared in any commit.
	Missing []IssueView `json:"missing"`
	// Extra: issues found in commits that were not on the expected list.
	Extra []StatusGroup `json:"extra"`
	// Reverted: commits whose topic begins with "Revert".
	Reverted []CommitView `json:"reverted"`
	// Reapplied: commits that reapply a previously reverted change (git's
	// `Reapply "…"`). Unlike reverts, these stay in the commit history and the
	// issue summary — a reapply re-ships the change — and are surfaced here only
	// as a callout for reviewers.
	Reapplied []CommitView `json:"reapplied"`
}
