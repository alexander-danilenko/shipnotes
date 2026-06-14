package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/alexander-danilenko/shipnotes/internal/domain/issue"
	"github.com/alexander-danilenko/shipnotes/internal/domain/report"
)

const (
	// batchSize is how many issue keys we ask for at once. 50 is Jira's
	// practical limit for a single search.
	batchSize = 50
	// requestTimeout caps how long we wait for any single API call.
	requestTimeout = 30 * time.Second
	// searchPath is the Jira REST endpoint used to look issues up by key.
	searchPath = "/rest/api/3/search/jql"
	// issueFields lists the fields we ask Jira to return. We only display the
	// summary and status, so we request only those.
	issueFields = "summary,status"
)

// Client fetches issues from Jira using Basic authentication (email + API
// token). It implements the domain's issue.Provider port.
type Client struct {
	baseURL    string
	email      string
	apiToken   string
	httpClient *http.Client
	reporter   report.Reporter
}

// New creates a Jira client. The base URL's trailing slash, if any, is removed
// so request URLs are built consistently.
func New(baseURL, email, apiToken string, reporter report.Reporter) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		email:      email,
		apiToken:   apiToken,
		httpClient: &http.Client{Timeout: requestTimeout},
		reporter:   reporter,
	}
}

// searchResponse is the slice of Jira's response we care about. The /search/jql
// endpoint paginates with an opaque nextPageToken (it ignores the older startAt
// parameter), so that token is how we know whether more pages remain.
type searchResponse struct {
	Issues        []apiIssue `json:"issues"`
	NextPageToken string     `json:"nextPageToken"`
}

// LoadByKeys fetches every requested issue and maps it onto the domain issue
// entity, splitting the work into batches and following Jira's pagination within
// each batch.
func (c *Client) LoadByKeys(ctx context.Context, issueKeys []string) ([]issue.Issue, error) {
	if len(issueKeys) == 0 {
		return nil, nil
	}

	c.reporter.Status("Loading Jira issues...")

	var allIssues []issue.Issue

	batches := chunk(issueKeys, batchSize)
	for index, batch := range batches {
		c.reporter.Status(fmt.Sprintf(
			"%d Jira issues loaded. Batch: %d/%d in progress...",
			len(allIssues), index+1, len(batches),
		))

		batchIssues, err := c.fetchBatch(ctx, batch)
		if err != nil {
			c.reporter.Failure("✗ Failed to load Jira issues")

			return nil, err
		}

		for _, raw := range batchIssues {
			allIssues = append(allIssues, toDomain(raw))
		}
	}

	c.reporter.Success(fmt.Sprintf("✓ Loaded %d Jira issues", len(allIssues)))

	return allIssues, nil
}

// toDomain maps a Jira API issue onto the clean domain entity. A missing status
// becomes an empty string, which the shipnotes builder shows as "Unknown".
func toDomain(raw apiIssue) issue.Issue {
	status := ""
	if raw.Fields.Status != nil {
		status = raw.Fields.Status.Name
	}

	return issue.Issue{
		Key:    raw.Key,
		Title:  raw.Fields.Summary,
		Status: status,
	}
}

// fetchBatch retrieves one batch of issues, following nextPageToken pagination
// until Jira stops handing back a token (or returns an empty page).
func (c *Client) fetchBatch(ctx context.Context, issueKeys []string) ([]apiIssue, error) {
	jql := buildKeyInJQL(issueKeys)

	var issues []apiIssue

	pageToken := "" // Empty on the first request; Jira returns the next one.
	for {
		page, err := c.fetchPage(ctx, jql, pageToken, issueKeys)
		if err != nil {
			return nil, err
		}

		issues = append(issues, page.Issues...)

		if page.NextPageToken == "" || len(page.Issues) == 0 {
			return issues, nil
		}

		pageToken = page.NextPageToken
	}
}

// fetchPage makes a single API request and decodes one page of results.
func (c *Client) fetchPage(
	ctx context.Context, jql, pageToken string, issueKeys []string,
) (searchResponse, error) {
	requestURL := c.buildSearchURL(jql, pageToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return searchResponse{}, &NetworkError{BaseURL: c.baseURL, Cause: err}
	}

	req.SetBasicAuth(c.email, c.apiToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return searchResponse{}, &NetworkError{BaseURL: c.baseURL, Cause: err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return searchResponse{}, &NetworkError{BaseURL: c.baseURL, Cause: err}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return searchResponse{}, &APIError{
			Status:          resp.StatusCode,
			StatusText:      statusText(resp),
			RequestURL:      c.baseURL + searchPath,
			IssueKeys:       issueKeys,
			ResponseDetails: string(body),
		}
	}

	var decoded searchResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return searchResponse{}, &NetworkError{BaseURL: c.baseURL, Cause: err}
	}

	return decoded, nil
}

// buildSearchURL assembles the search endpoint URL with query parameters. A
// non-empty pageToken requests the next page of a multi-page result.
func (c *Client) buildSearchURL(jql, pageToken string) string {
	params := url.Values{}
	params.Set("jql", jql)
	params.Set("maxResults", strconv.Itoa(batchSize))
	params.Set("fields", issueFields)

	if pageToken != "" {
		params.Set("nextPageToken", pageToken)
	}

	return c.baseURL + searchPath + "?" + params.Encode()
}

// buildKeyInJQL builds a JQL clause like: key IN ("ABC-1","ABC-2"). Each key is
// escaped so a stray quote or backslash cannot break out of its string literal
// and alter the query, rather than relying on the caller having validated them.
func buildKeyInJQL(issueKeys []string) string {
	quoted := make([]string, len(issueKeys))
	for i, key := range issueKeys {
		quoted[i] = `"` + escapeJQLString(key) + `"`
	}

	return "key IN (" + strings.Join(quoted, ",") + ")"
}

// escapeJQLString escapes the two characters that are special inside a
// double-quoted JQL string literal: the backslash and the double quote. The
// backslash is escaped first so the quotes added next are not double-escaped.
func escapeJQLString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)

	return s
}

// statusText returns the HTTP reason phrase (e.g. "Not Found"), falling back to
// "Unknown" when none is available.
func statusText(resp *http.Response) string {
	const statusParts = 2 // "<code> <reason phrase>".
	if parts := strings.SplitN(resp.Status, " ", statusParts); len(parts) == statusParts && parts[1] != "" {
		return parts[1]
	}

	if text := http.StatusText(resp.StatusCode); text != "" {
		return text
	}

	return "Unknown"
}

// chunk splits items into consecutive slices of at most size elements.
func chunk[T any](items []T, size int) [][]T {
	var batches [][]T

	for start := 0; start < len(items); start += size {
		end := min(start+size, len(items))
		batches = append(batches, items[start:end])
	}

	return batches
}
