package markdown_test

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexander-danilenko/shipnotes/internal/domain/commit"
	"github.com/alexander-danilenko/shipnotes/internal/domain/issue"
	"github.com/alexander-danilenko/shipnotes/internal/domain/notes"
	"github.com/alexander-danilenko/shipnotes/internal/infrastructure/markdown"
	"github.com/alexander-danilenko/shipnotes/internal/infrastructure/terminal"
)

// update, when set with `go test ./internal/infrastructure/markdown -update`,
// rewrites the golden files from the current template output instead of
// comparing against them. Use it after an intentional change to the template or
// data model, then review the diff before committing.
var update = flag.Bool("update", false, "rewrite golden files instead of comparing")

// TestRenderMatchesGolden renders every JSON fixture in testdata/cases and
// asserts the output matches its .golden file byte for byte. The golden files
// are this tool's own recorded output; regenerate them with -update after an
// intentional rendering change.
func TestRenderMatchesGolden(t *testing.T) {
	casesDir := filepath.Join("..", "..", "..", "testdata", "cases")
	goldenDir := filepath.Join("..", "..", "..", "testdata", "golden")

	entries, err := os.ReadDir(casesDir)
	if err != nil {
		t.Fatalf("read cases dir: %v", err)
	}

	renderer := markdown.New()

	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		name := entry.Name()[:len(entry.Name())-len(".json")]

		t.Run(name, func(t *testing.T) {
			caseBytes, err := os.ReadFile(filepath.Join(casesDir, name+".json"))
			if err != nil {
				t.Fatalf("read case: %v", err)
			}

			var data notes.ReleaseNotes
			if err := json.Unmarshal(caseBytes, &data); err != nil {
				t.Fatalf("unmarshal case: %v", err)
			}

			got, err := renderer.Render(data)
			if err != nil {
				t.Fatalf("render: %v", err)
			}

			goldenPath := filepath.Join(goldenDir, name+".golden")

			if *update {
				if err := os.WriteFile(goldenPath, []byte(got), 0o600); err != nil {
					t.Fatalf("write golden: %v", err)
				}

				return
			}

			wantBytes, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}

			if got != string(wantBytes) {
				t.Errorf("rendered output does not match golden (run: go test ./internal/infrastructure/markdown -update)"+
					"\n--- got ---\n%q\n--- want ---\n%q", got, string(wantBytes))
			}
		})
	}
}

// fakeProvider returns a fixed set of issues for the end-to-end test.
type fakeProvider struct {
	issues []issue.Issue
}

func (f fakeProvider) LoadByKeys(_ context.Context, _ []string) ([]issue.Issue, error) {
	return f.issues, nil
}

// TestBuildThenRender proves the builder and the renderer work together
// end-to-end (the golden tests cover the renderer alone with fixed data).
func TestBuildThenRender(t *testing.T) {
	coords := notes.Coordinates{
		GithubBaseURL: "https://github.com/acme/widgets",
		JiraBaseURL:   "https://acme.atlassian.net",
	}
	commits := []commit.Commit{
		{CanonicalHash: "h1", Hash: "h1", Topic: "PROJ-101: login", JiraIssueIDs: []string{"PROJ-101"}, Authors: []string{"Jane"}},
		{CanonicalHash: "h2", Hash: "h2", Topic: `Revert "PROJ-700: oops"`, JiraIssueIDs: []string{"PROJ-700"}, IsRevert: true, Authors: []string{"Bob"}},
		{CanonicalHash: "h3", Hash: "h3", Topic: `Reapply "PROJ-105: bring it back"`, JiraIssueIDs: []string{"PROJ-105"}, IsReapply: true, Authors: []string{"Cara"}},
	}
	provider := fakeProvider{issues: []issue.Issue{
		{Key: "PROJ-101", Title: "Login page", Status: "Done"},
		{Key: "PROJ-105", Title: "Bring it back", Status: "Done"},
	}}

	data, err := notes.NewBuilder(provider, terminal.New(io.Discard), notes.StatusMatcher{}, notes.CommitMatcher{}).
		Build(context.Background(), coords, commits, []string{"PROJ-101", "PROJ-105"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	out, err := markdown.New().Render(data)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	mustContain := []string{
		"# Release Notes",
		"## Release summary",
		"### Done",
		"- [ ] [PROJ-101](https://acme.atlassian.net/browse/PROJ-101) Login page",
		"## Reverted commits",
		"- [ ] [`h2`](https://github.com/acme/widgets/commit/h2)",
		"## Reapplied commits",
		"- [ ] [`h3`](https://github.com/acme/widgets/commit/h3)",
		"# Participants",
		"- `Bob`",
		"# Commit history",
		"| Done |",    // PROJ-101 commit, status text only
		"| Unknown |", // reverted commit whose issue was not loaded
	}
	for _, want := range mustContain {
		if !strings.Contains(out, want) {
			t.Errorf("rendered output missing %q\n---\n%s", want, out)
		}
	}
}
