package jira

import (
	"fmt"
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
	Status          int
	StatusText      string
	RequestURL      string
	IssueKeys       []string
	ResponseDetails string
}

func (e *APIError) Error() string {
	details := e.ResponseDetails
	if details == "" {
		details = "No error details available"
	}

	lines := []string{
		fmt.Sprintf("Jira API request failed with status %d %s", e.Status, e.StatusText),
		"",
		"Request URL: " + e.RequestURL,
		"Issue keys requested: " + e.displayKeys(),
		"",
		"Response details:",
		details,
		"",
		"Possible causes:",
		"  • Invalid JIRA credentials (email or API token)",
		"  • Insufficient permissions to access the requested issues",
		"  • JIRA API endpoint changed or unavailable",
		"  • Rate limiting - too many requests",
		"",
		"Troubleshooting steps:",
		"  1. Verify SHIPNOTES_JIRA_EMAIL and SHIPNOTES_JIRA_TOKEN environment variables",
		"  2. Check if your API token has expired",
		"  3. Verify you have access to the requested issues in JIRA",
		"  4. Check JIRA status page for service outages",
	}

	return strings.Join(lines, "\n")
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
