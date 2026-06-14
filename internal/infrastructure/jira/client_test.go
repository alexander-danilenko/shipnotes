package jira

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alexander-danilenko/shipnotes/internal/domain/issue"
	"github.com/alexander-danilenko/shipnotes/internal/infrastructure/terminal"
)

// newTestClient builds a Client pointed at a test server.
func newTestClient(serverURL string) *Client {
	return New(serverURL, "ci@example.com", "token", terminal.New(io.Discard))
}

func TestLoadByKeysEmpty(t *testing.T) {
	// With no keys, the client must not make any request at all.
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("no HTTP request should be made for an empty key list")
	}))
	defer server.Close()

	issues, err := newTestClient(server.URL).LoadByKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(issues) != 0 {
		t.Errorf("expected no issues, got %d", len(issues))
	}
}

func TestLoadByKeysMapsFields(t *testing.T) {
	// A single issue with a status maps onto the clean domain entity.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, searchResponse{Issues: []apiIssue{
			{Key: "CX-1", Fields: apiFields{Summary: "Login", Status: &apiStatus{Name: "Done"}}},
			{Key: "CX-2", Fields: apiFields{Summary: "No status"}}, // missing status -> ""
		}})
	}))
	defer server.Close()

	issues, err := newTestClient(server.URL).LoadByKeys(context.Background(), []string{"CX-1", "CX-2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []issue.Issue{
		{Key: "CX-1", Title: "Login", Status: "Done"},
		{Key: "CX-2", Title: "No status", Status: ""},
	}
	for i, w := range want {
		if issues[i] != w {
			t.Errorf("issue %d: got %+v, want %+v", i, issues[i], w)
		}
	}
}

func TestLoadByKeysPaginates(t *testing.T) {
	// First page hands back a nextPageToken; second page clears it. The client
	// must follow the token and concatenate both pages.
	var requests int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++

		w.Header().Set("Content-Type", "application/json")

		if r.URL.Query().Get("nextPageToken") == "" {
			writeJSON(t, w, searchResponse{
				Issues:        []apiIssue{{Key: "CX-1"}, {Key: "CX-2"}},
				NextPageToken: "page-2",
			})

			return
		}

		writeJSON(t, w, searchResponse{Issues: []apiIssue{{Key: "CX-3"}}})
	}))
	defer server.Close()

	issues, err := newTestClient(server.URL).LoadByKeys(context.Background(), []string{"CX-1", "CX-2", "CX-3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if requests != 2 {
		t.Errorf("expected 2 paged requests, got %d", requests)
	}

	if got := keys(issues); strings.Join(got, ",") != "CX-1,CX-2,CX-3" {
		t.Errorf("issues: got %v, want [CX-1 CX-2 CX-3]", got)
	}
}

func TestLoadByKeysSplitsIntoBatches(t *testing.T) {
	// 51 keys exceed the batch size of 50, so the client must issue two batch
	// requests (each a fresh, single-page search).
	var requests int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++

		writeJSON(t, w, searchResponse{Issues: []apiIssue{{Key: "X-1"}}})
	}))
	defer server.Close()

	manyKeys := make([]string, 51)
	for i := range manyKeys {
		manyKeys[i] = "X-1"
	}

	if _, err := newTestClient(server.URL).LoadByKeys(context.Background(), manyKeys); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if requests != 2 {
		t.Errorf("expected 2 batch requests for 51 keys, got %d", requests)
	}
}

func TestLoadByKeysAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"errorMessages":["boom"]}`, http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := newTestClient(server.URL).LoadByKeys(context.Background(), []string{"CX-1"})

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T (%v)", err, err)
	}

	if apiErr.Status != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", apiErr.Status)
	}

	// The auth troubleshooting message must name the variables the tool actually
	// reads, so users can fix their credentials.
	message := apiErr.Error()
	for _, name := range []string{"SHIPNOTES_JIRA_EMAIL", "SHIPNOTES_JIRA_TOKEN"} {
		if !strings.Contains(message, name) {
			t.Errorf("auth error message should mention %q, got:\n%s", name, message)
		}
	}
}

func TestLoadByKeysMalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "this is not json")
	}))
	defer server.Close()

	_, err := newTestClient(server.URL).LoadByKeys(context.Background(), []string{"CX-1"})

	var netErr *NetworkError
	if !errors.As(err, &netErr) {
		t.Fatalf("expected *NetworkError for bad JSON, got %T (%v)", err, err)
	}
}

func TestBuildKeyInJQL(t *testing.T) {
	got := buildKeyInJQL([]string{"CX-1", "AB-2"})

	want := `key IN ("CX-1","AB-2")`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A key containing a quote or backslash must be escaped so it cannot break out
// of its JQL string literal and alter the query.
func TestBuildKeyInJQLEscapes(t *testing.T) {
	got := buildKeyInJQL([]string{`a"b`, `c\d`})

	want := `key IN ("a\"b","c\\d")`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildSearchURL(t *testing.T) {
	client := newTestClient("https://acme.atlassian.net")

	first := client.buildSearchURL(`key IN ("CX-1")`, "")
	if !strings.Contains(first, "/rest/api/3/search/jql?") {
		t.Errorf("missing endpoint path: %q", first)
	}

	if strings.Contains(first, "nextPageToken") {
		t.Errorf("first page should not carry a page token: %q", first)
	}

	if !strings.Contains(first, "fields=summary%2Cstatus") {
		t.Errorf("expected only summary,status fields: %q", first)
	}

	next := client.buildSearchURL(`key IN ("CX-1")`, "tok")
	if !strings.Contains(next, "nextPageToken=tok") {
		t.Errorf("expected page token in URL: %q", next)
	}
}

func TestChunk(t *testing.T) {
	got := chunk([]int{1, 2, 3, 4, 5}, 2)
	if len(got) != 3 || len(got[0]) != 2 || len(got[2]) != 1 {
		t.Errorf("unexpected chunking: %v", got)
	}

	if chunk([]int{}, 2) != nil {
		t.Error("empty input should chunk to nil")
	}
}

// --- small test helpers ---

func writeJSON(t *testing.T, w http.ResponseWriter, body searchResponse) {
	t.Helper()

	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func keys(issues []issue.Issue) []string {
	out := make([]string, len(issues))
	for i, found := range issues {
		out[i] = found.Key
	}

	return out
}
