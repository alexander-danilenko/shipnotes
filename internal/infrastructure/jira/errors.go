package jira

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// NetworkError means we could not reach the Jira API at all (DNS, connection,
// timeout, VPN interference, and so on). Its message gives detailed, actionable
// troubleshooting guidance.
type NetworkError struct {
	BaseURL string
	Cause   error
}

func (e *NetworkError) Error() string {
	hostname := "unknown"
	if parsed, err := url.Parse(e.BaseURL); err == nil && parsed.Hostname() != "" {
		hostname = parsed.Hostname()
	}

	lines := []string{
		"Failed to connect to Jira API at " + e.BaseURL,
		"",
		fmt.Sprintf("Error: %s", e.Cause),
		"",
		"Possible causes:",
		"  • Network connectivity issues",
		"  • Corporate firewall or proxy blocking the request",
		"  • Cloudflare Warp VPN may be interfering with network requests",
		"",
		"Troubleshooting steps:",
		"  1. Check your internet connection",
		"  2. Verify Jira URL is accessible: " + e.BaseURL,
		"  3. If using Cloudflare Warp VPN, try disabling it temporarily",
		"  4. Check if corporate firewall/proxy requires configuration",
		"  5. Verify DNS resolution for: " + hostname,
	}

	return strings.Join(lines, "\n")
}

func (e *NetworkError) Unwrap() error { return e.Cause }

// APIError means Jira responded, but with an error HTTP status. Its message
// gives troubleshooting guidance for the most common credential and access
// problems.
type APIError struct {
	Status     int
	StatusText string
	RequestURL string
	// IssueKeys is the key list of a key-lookup request, or nil for a free-form
	// JQL search. JQL is the query actually sent: the generated key IN (...)
	// clause for a key lookup, or the user's query for a --jql search. Both are
	// populated on the key-lookup path; requestContext prefers IssueKeys, so a
	// key lookup shows the keys and a --jql search shows the query.
	IssueKeys []string
	JQL       string
	// Messages holds Jira's own explanation, parsed from the response body (e.g.
	// "Error in the JQL Query: ..."). It is nil when the body could not be parsed,
	// in which case ResponseDetails (the raw body) is shown instead.
	Messages        []string
	ResponseDetails string
}

func (e *APIError) Error() string {
	detail := e.detailLines()
	guidance := e.guidanceLines()

	lines := make([]string, 0, headerLineCount+len(detail)+len(guidance))
	lines = append(lines,
		fmt.Sprintf("Jira API request failed with status %d %s", e.Status, e.StatusText),
		"",
		"Request URL: "+e.RequestURL,
		e.requestContext(),
		"",
	)
	lines = append(lines, detail...)
	lines = append(lines, "")
	lines = append(lines, guidance...)

	return strings.Join(lines, "\n")
}

// headerLineCount is the number of fixed lines Error() writes before the detail
// and guidance blocks (five header lines plus the blank separator after them).
const headerLineCount = 6

// detailLines shows Jira's own explanation when we managed to parse it, falling
// back to the raw response body otherwise.
func (e *APIError) detailLines() []string {
	if len(e.Messages) > 0 {
		lines := make([]string, 0, len(e.Messages)+1)
		lines = append(lines, "Jira reported:")

		for _, message := range e.Messages {
			lines = append(lines, "  • "+message)
		}

		return lines
	}

	details := e.ResponseDetails
	if details == "" {
		details = "No error details available"
	}

	return []string{"Response details:", details}
}

// guidanceLines tailors the "Possible causes" and "Troubleshooting steps" to the
// HTTP status, so a malformed JQL (400) points at the query while a 401 points at
// credentials. Unrecognized statuses get a generic catch-all.
func (e *APIError) guidanceLines() []string {
	switch e.Status {
	case http.StatusBadRequest:
		return badRequestGuidance()
	case http.StatusUnauthorized:
		return unauthorizedGuidance()
	case http.StatusForbidden:
		return forbiddenGuidance()
	case http.StatusNotFound:
		return notFoundGuidance()
	case http.StatusTooManyRequests:
		return rateLimitedGuidance()
	default:
		return genericGuidance()
	}
}

// formatGuidance renders a "Possible causes" list and a numbered "Troubleshooting
// steps" list into the lines shown at the end of an API error.
func formatGuidance(causes, steps []string) []string {
	const fixedLines = 3 // two headers plus the blank line between them.

	lines := make([]string, 0, len(causes)+len(steps)+fixedLines)

	lines = append(lines, "Possible causes:")
	for _, cause := range causes {
		lines = append(lines, "  • "+cause)
	}

	lines = append(lines, "", "Troubleshooting steps:")
	for i, step := range steps {
		lines = append(lines, fmt.Sprintf("  %d. %s", i+1, step))
	}

	return lines
}

func badRequestGuidance() []string {
	return formatGuidance(
		[]string{
			"The JQL query has a syntax error",
			"It references a field, function, or value Jira does not recognize",
			"A project or issue key in the query does not exist",
		},
		[]string{
			"Review the JQL query shown above",
			"Test the query in Jira's issue search (Filters → Advanced search) to see the exact error",
			"Check that field names and values are spelled correctly",
		},
	)
}

func unauthorizedGuidance() []string {
	return formatGuidance(
		[]string{
			"Invalid JIRA credentials (email or API token)",
			"The API token has expired or been revoked",
		},
		[]string{
			"Verify SHIPNOTES_JIRA_EMAIL and SHIPNOTES_JIRA_TOKEN environment variables",
			"Check if your API token has expired and create a new one if needed",
		},
	)
}

func forbiddenGuidance() []string {
	return formatGuidance(
		[]string{
			"Your account lacks permission to access the requested issues or projects",
			"The account is not a member of the required Jira project",
		},
		[]string{
			"Verify you can open the requested issues/projects in Jira directly",
			"Ask a Jira administrator to grant the necessary project permissions",
		},
	)
}

func notFoundGuidance() []string {
	return formatGuidance(
		[]string{
			"The Jira base URL is incorrect",
			"The Jira REST API endpoint has changed or is unavailable",
		},
		[]string{
			"Verify SHIPNOTES_JIRA_BASE_URL points to your Jira site (e.g. https://your-org.atlassian.net)",
			"Confirm the site is reachable in a browser",
		},
	)
}

func rateLimitedGuidance() []string {
	return formatGuidance(
		[]string{
			"Too many requests sent to Jira in a short period (rate limited)",
		},
		[]string{
			"Wait a short while before retrying",
			"Reduce how frequently you run the tool",
		},
	)
}

func genericGuidance() []string {
	return formatGuidance(
		[]string{
			"Invalid JIRA credentials (email or API token)",
			"Insufficient permissions to access the requested issues",
			"JIRA API endpoint changed or unavailable",
			"Rate limiting - too many requests",
		},
		[]string{
			"Verify SHIPNOTES_JIRA_EMAIL and SHIPNOTES_JIRA_TOKEN environment variables",
			"Check if your API token has expired",
			"Verify you have access to the requested issues in JIRA",
			"Check JIRA status page for service outages",
		},
	)
}

// requestContext describes what was asked for, so a failure points at the cause:
// the issue keys for a key lookup, or the JQL for a --jql search.
func (e *APIError) requestContext() string {
	if len(e.IssueKeys) > 0 {
		return "Issue keys requested: " + e.displayKeys()
	}

	if e.JQL != "" {
		return "JQL query: " + e.JQL
	}

	return "Issue keys requested: none"
}

// displayKeys shows the first five requested keys, then a count of the rest, so
// a long key list does not overwhelm the error message.
func (e *APIError) displayKeys() string {
	const limit = 5
	if len(e.IssueKeys) <= limit {
		return strings.Join(e.IssueKeys, ", ")
	}

	return fmt.Sprintf("%s (and %d more)",
		strings.Join(e.IssueKeys[:limit], ", "), len(e.IssueKeys)-limit)
}
