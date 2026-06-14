package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/alexander-danilenko/shipnotes/internal/application"
	"github.com/alexander-danilenko/shipnotes/internal/domain/commit"
	"github.com/alexander-danilenko/shipnotes/internal/domain/issue"
	"github.com/alexander-danilenko/shipnotes/internal/domain/notes"
)

// --- fakes for the ports the service depends on ---

type fakeRepo struct {
	valid   bool
	commits []commit.Commit
}

func (f fakeRepo) Validate(context.Context, string) (bool, error) { return f.valid, nil }
func (f fakeRepo) Log(context.Context, string) ([]commit.Commit, error) {
	return f.commits, nil
}

type fakeProvider struct{ issues []issue.Issue }

func (f fakeProvider) LoadByKeys(context.Context, []string) ([]issue.Issue, error) {
	return f.issues, nil
}

type fakeRenderer struct{ out string }

func (f fakeRenderer) Render(notes.ReleaseNotes) (string, error) { return f.out, nil }

type fakeWriter struct {
	gotContent string
	gotPath    string
}

func (f *fakeWriter) Write(content, path string) (string, error) {
	f.gotContent = content
	f.gotPath = path

	return path, nil
}

type fakeSearcher struct {
	called bool
	gotJQL string
	keys   []string
}

func (f *fakeSearcher) SearchByJQL(_ context.Context, jql string) ([]string, error) {
	f.called = true
	f.gotJQL = jql

	return f.keys, nil
}

// noopReporter discards the builder's progress messages in tests.
type noopReporter struct{}

func (noopReporter) Status(string)  {}
func (noopReporter) Success(string) {}
func (noopReporter) Failure(string) {}
func (noopReporter) Warn(string)    {}
func (noopReporter) Dim(string)     {}

func newService(repo commit.Repository, writer application.Writer, searcher application.IssueSearcher) *application.Service {
	builder := notes.NewBuilder(fakeProvider{}, noopReporter{}, notes.StatusMatcher{})
	coords := notes.Coordinates{GithubBaseURL: "https://github.com/acme/widgets", JiraBaseURL: "https://acme.atlassian.net"}

	return application.New(repo, builder, fakeRenderer{out: "RENDERED"}, writer, searcher, coords, "/repo")
}

func TestRunRejectsInvalidCommit(t *testing.T) {
	svc := newService(fakeRepo{valid: false}, &fakeWriter{}, &fakeSearcher{})

	_, err := svc.Run(context.Background(), application.Input{CommitHash: "deadbee"})
	if err == nil || !strings.Contains(err.Error(), "invalid commit hash") {
		t.Fatalf("expected an invalid-commit error, got %v", err)
	}
}

func TestRunSearchesWhenJQLProvided(t *testing.T) {
	repo := fakeRepo{valid: true, commits: []commit.Commit{
		{CanonicalHash: "h1", Hash: "h1", Topic: "CX-1: thing", JiraIssueIDs: []string{"CX-1"}, Authors: []string{"Jane"}},
	}}

	// A non-empty JQL -> the searcher port resolves the release issue list.
	searcher := &fakeSearcher{keys: []string{"CX-1"}}
	writer := &fakeWriter{}

	result, err := newService(repo, writer, searcher).Run(context.Background(), application.Input{
		CommitHash: "HEAD~1",
		JQL:        "key IN (CX-1)",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if !searcher.called {
		t.Error("expected the searcher port to be called when --jql is provided")
	}

	if searcher.gotJQL != "key IN (CX-1)" {
		t.Errorf("jql: got %q, want %q", searcher.gotJQL, "key IN (CX-1)")
	}

	if result.CommitCount != 1 {
		t.Errorf("commit count: got %d, want 1", result.CommitCount)
	}

	if writer.gotContent != "RENDERED" {
		t.Errorf("writer content: got %q, want RENDERED", writer.gotContent)
	}
	// A relative output path is resolved inside the working directory.
	if writer.gotPath != "/repo/SHIPNOTES.md" {
		t.Errorf("writer path: got %q, want /repo/SHIPNOTES.md", writer.gotPath)
	}
}

func TestRunSkipsSearchWhenJQLEmpty(t *testing.T) {
	repo := fakeRepo{valid: true, commits: []commit.Commit{
		{CanonicalHash: "h1", Hash: "h1", Topic: "CX-1: thing", JiraIssueIDs: []string{"CX-1"}, Authors: []string{"Jane"}},
	}}
	searcher := &fakeSearcher{}

	_, err := newService(repo, &fakeWriter{}, searcher).Run(context.Background(), application.Input{
		CommitHash: "HEAD~1",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if searcher.called {
		t.Error("searcher must not be called when --jql was not provided")
	}
}

func TestRunUsesAbsoluteOutputPathAsIs(t *testing.T) {
	repo := fakeRepo{valid: true}
	writer := &fakeWriter{}

	_, err := newService(repo, writer, &fakeSearcher{}).Run(context.Background(), application.Input{
		CommitHash: "HEAD",
		OutputPath: "/tmp/out.md",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if writer.gotPath != "/tmp/out.md" {
		t.Errorf("absolute path should be used as-is, got %q", writer.gotPath)
	}
}
