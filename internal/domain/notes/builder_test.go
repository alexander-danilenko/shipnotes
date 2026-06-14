package notes_test

import (
	"context"
	"strings"
	"testing"

	"github.com/alexander-danilenko/shipnotes/internal/domain/commit"
	"github.com/alexander-danilenko/shipnotes/internal/domain/issue"
	"github.com/alexander-danilenko/shipnotes/internal/domain/notes"
)

// fakeProvider returns a fixed set of issues, so the builder can be tested
// without touching the network.
type fakeProvider struct {
	issues []issue.Issue
}

func (f fakeProvider) LoadByKeys(_ context.Context, _ []string) ([]issue.Issue, error) {
	return f.issues, nil
}

// noopReporter discards progress messages. Using one here keeps the domain
// tests free of any infrastructure import.
type noopReporter struct{}

func (noopReporter) Status(string)  {}
func (noopReporter) Success(string) {}
func (noopReporter) Failure(string) {}
func (noopReporter) Warn(string)    {}
func (noopReporter) Dim(string)     {}

// warnRecorder captures the warnings the builder emits, so a test can assert
// whether the "summarizing all issues" fallback warning appeared.
type warnRecorder struct {
	noopReporter

	warns []string
}

func (w *warnRecorder) Warn(message string) { w.warns = append(w.warns, message) }

func TestBuildFallbackWarningOnlyWhenNoSelection(t *testing.T) {
	commits := []commit.Commit{
		{CanonicalHash: "h1", Hash: "h1", Topic: "PROJ-101: login", JiraIssueIDs: []string{"PROJ-101"}, Authors: []string{"Jane"}},
	}
	issues := []issue.Issue{issueWithStatus("PROJ-101", "Login", "Done")}

	cases := []struct {
		name     string
		release  []string
		wantWarn bool
	}{
		// No --jql at all: a nil list means no selection was made, so a warning
		// explains the fallback to summarizing the whole range.
		{"no selection (nil)", nil, true},
		// --jql ran but matched nothing: a non-nil empty list. SearchByJQL already
		// warned, so the builder must stay quiet to avoid a second message.
		{"empty selection (non-nil)", []string{}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reporter := &warnRecorder{}
			builder := notes.NewBuilder(fakeProvider{issues: issues}, reporter, notes.StatusMatcher{}, notes.CommitMatcher{})

			if _, err := builder.Build(context.Background(), testCoords(), commits, tc.release); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			gotWarn := strings.Contains(strings.Join(reporter.warns, "\n"), "summarizing all")
			if gotWarn != tc.wantWarn {
				t.Errorf("fallback warning: got %v, want %v (warns: %v)", gotWarn, tc.wantWarn, reporter.warns)
			}
		})
	}
}

func testCoords() notes.Coordinates {
	return notes.Coordinates{
		GithubBaseURL: "https://github.com/acme/widgets",
		JiraBaseURL:   "https://acme.atlassian.net",
	}
}

func issueWithStatus(key, title, status string) issue.Issue {
	return issue.Issue{Key: key, Title: title, Status: status}
}

func newBuilder(issues []issue.Issue) *notes.Builder {
	return notes.NewBuilder(fakeProvider{issues: issues}, noopReporter{}, notes.StatusMatcher{}, notes.CommitMatcher{})
}

// newBuilderExcluding is newBuilder with an --exclude-commits pattern compiled in.
func newBuilderExcluding(issues []issue.Issue, pattern string) *notes.Builder {
	matcher, err := notes.NewCommitMatcher(pattern)
	if err != nil {
		panic(err)
	}

	return notes.NewBuilder(fakeProvider{issues: issues}, noopReporter{}, notes.StatusMatcher{}, matcher)
}

func TestBuildSummaryCategories(t *testing.T) {
	commits := []commit.Commit{
		{CanonicalHash: "h1", Hash: "h1", Topic: "PROJ-101: login", JiraIssueIDs: []string{"PROJ-101"}, Authors: []string{"Jane Doe"}},
		{CanonicalHash: "h2", Hash: "h2", Topic: "PROJ-200: wip", JiraIssueIDs: []string{"PROJ-200"}, Authors: []string{"alex"}},
		{CanonicalHash: "h3", Hash: "h3", Topic: `Revert "PROJ-700: oops"`, JiraIssueIDs: []string{"PROJ-700"}, IsRevert: true, Authors: []string{"Bob"}},
		{CanonicalHash: "h4", Hash: "h4", Topic: "chore: tidy", JiraIssueIDs: []string{}, Authors: []string{"alex"}},
	}
	builder := newBuilder([]issue.Issue{
		issueWithStatus("PROJ-101", "Login page", "Done"),
		issueWithStatus("PROJ-200", "Work in progress", "In Progress"),
		issueWithStatus("PROJ-300", "Docs", "To Do"),
		issueWithStatus("PROJ-700", "Oops", "Done"),
	})

	data, err := builder.Build(context.Background(), testCoords(), commits, []string{"PROJ-101", "PROJ-300"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if data.Summary == nil {
		t.Fatal("expected a summary, got nil")
	}

	// PROJ-101 is in commits and on the release list -> grouped by its status.
	assertSingleIssue(t, data.Summary.ByStatus, "Done", "PROJ-101")
	// PROJ-300 was expected but never appeared in a commit -> missing.
	if len(data.Summary.Missing) != 1 || data.Summary.Missing[0].Key != "PROJ-300" {
		t.Errorf("missing: got %v, want [PROJ-300]", data.Summary.Missing)
	}
	// PROJ-200 is in commits but not on the release list -> extra.
	assertSingleIssue(t, data.Summary.Extra, "In Progress", "PROJ-200")
	// The revert commit shows up under reverted.
	if len(data.Summary.Reverted) != 1 || data.Summary.Reverted[0].Hash != "h3" {
		t.Errorf("reverted: got %v, want one commit h3", data.Summary.Reverted)
	}
}

// TestBuildMarksCheckedStatuses proves the status matcher flows into every issue
// list of the summary: a matching status is checked wherever the issue appears
// (in-commits, missing, or extra), and a non-matching status is not.
func TestBuildMarksCheckedStatuses(t *testing.T) {
	commits := []commit.Commit{
		{CanonicalHash: "h1", Hash: "h1", Topic: "PROJ-101: login", JiraIssueIDs: []string{"PROJ-101"}, Authors: []string{"Jane"}},
		{CanonicalHash: "h2", Hash: "h2", Topic: "PROJ-200: wip", JiraIssueIDs: []string{"PROJ-200"}, Authors: []string{"alex"}},
	}

	matcher, err := notes.NewStatusMatcher("done|ready to release|ready for release")
	if err != nil {
		t.Fatalf("compile matcher: %v", err)
	}

	provider := fakeProvider{issues: []issue.Issue{
		issueWithStatus("PROJ-101", "Login", "Done"),              // in commits + release list
		issueWithStatus("PROJ-200", "WIP", "In Progress"),         // in commits, not on release list -> extra
		issueWithStatus("PROJ-300", "Ready", "Ready for Release"), // on release list, never shipped -> missing
	}}
	builder := notes.NewBuilder(provider, noopReporter{}, matcher, notes.CommitMatcher{})

	data, err := builder.Build(context.Background(), testCoords(), commits, []string{"PROJ-101", "PROJ-300"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	// "Done" matches (case-insensitively) -> checked, in the ByStatus list.
	if got := data.Summary.ByStatus[0].Issues[0]; !got.Checked {
		t.Errorf("PROJ-101 (Done): expected checked, got %+v", got)
	}
	// "Ready for Release" matches -> checked, in the Missing list.
	if got := data.Summary.Missing[0]; !got.Checked {
		t.Errorf("PROJ-300 (Ready for Release): expected checked, got %+v", got)
	}
	// "In Progress" does not match -> unchecked, in the Extra list.
	if got := data.Summary.Extra[0].Issues[0]; got.Checked {
		t.Errorf("PROJ-200 (In Progress): expected unchecked, got %+v", got)
	}
}

func TestBuildSummaryDefaultsToCommitIssues(t *testing.T) {
	commits := []commit.Commit{
		{CanonicalHash: "h1", Hash: "h1", Topic: "PROJ-101: login", JiraIssueIDs: []string{"PROJ-101"}, Authors: []string{"Jane Doe"}},
		{CanonicalHash: "h2", Hash: "h2", Topic: "PROJ-200: wip", JiraIssueIDs: []string{"PROJ-200"}, Authors: []string{"alex"}},
		{CanonicalHash: "h3", Hash: "h3", Topic: `Revert "PROJ-700: oops"`, JiraIssueIDs: []string{"PROJ-700"}, IsRevert: true, Authors: []string{"Bob"}},
		{CanonicalHash: "h4", Hash: "h4", Topic: "chore: tidy", JiraIssueIDs: []string{}, Authors: []string{"alex"}},
	}
	builder := newBuilder([]issue.Issue{
		issueWithStatus("PROJ-101", "Login page", "Done"),
		issueWithStatus("PROJ-200", "Work in progress", "In Progress"),
		issueWithStatus("PROJ-700", "Oops", "Done"),
	})

	// nil release IDs => default to every issue referenced by the non-revert
	// commits in the range.
	data, err := builder.Build(context.Background(), testCoords(), commits, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if data.Summary == nil {
		t.Fatal("expected a default summary, got nil")
	}

	// Both non-revert issues appear, each grouped under its own status.
	assertSingleIssue(t, data.Summary.ByStatus[:1], "Done", "PROJ-101")
	assertSingleIssue(t, data.Summary.ByStatus[1:], "In Progress", "PROJ-200")

	if len(data.Summary.Missing) != 0 {
		t.Errorf("missing: got %v, want none", data.Summary.Missing)
	}

	if len(data.Summary.Extra) != 0 {
		t.Errorf("extra: got %v, want none", data.Summary.Extra)
	}

	// The revert keeps its own section and is not counted among the issues.
	if len(data.Summary.Reverted) != 1 || data.Summary.Reverted[0].Hash != "h3" {
		t.Errorf("reverted: got %v, want one commit h3", data.Summary.Reverted)
	}
}

func TestBuildReappliedCommitsStayInFlow(t *testing.T) {
	commits := []commit.Commit{
		{CanonicalHash: "h1", Hash: "h1", Topic: "PROJ-101: login", JiraIssueIDs: []string{"PROJ-101"}, Authors: []string{"Jane"}},
		{CanonicalHash: "h2", Hash: "h2", Topic: `Reapply "PROJ-105: bring it back"`, JiraIssueIDs: []string{"PROJ-105"}, IsReapply: true, Authors: []string{"Bob"}},
	}
	builder := newBuilder([]issue.Issue{
		issueWithStatus("PROJ-101", "Login", "Done"),
		issueWithStatus("PROJ-105", "Bring it back", "Done"),
	})

	data, err := builder.Build(context.Background(), testCoords(), commits, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if data.Summary == nil {
		t.Fatal("expected a default summary, got nil")
	}

	// The reapply is flagged in its own section.
	if len(data.Summary.Reapplied) != 1 || data.Summary.Reapplied[0].Hash != "h2" {
		t.Errorf("reapplied: got %v, want one commit h2", data.Summary.Reapplied)
	}

	// Unlike a revert, the reapply's issue still counts toward the summary.
	if !issueInGroups(data.Summary.ByStatus, "PROJ-105") {
		t.Error("reapplied commit's issue PROJ-105 should be counted in the summary")
	}

	// And the reapply still appears in the commit history table.
	if len(data.Commits) != 2 {
		t.Fatalf("expected 2 commits in history, got %d", len(data.Commits))
	}
}

// TestBuildExcludesCommits proves the --exclude-commits filter is the first
// gate: matched commits leave the history table and their issues leave the
// summary, they are listed under Excluded instead, and the rule wins over the
// revert classification (an excluded revert is reported only as excluded).
func TestBuildExcludesCommits(t *testing.T) {
	commits := []commit.Commit{
		{CanonicalHash: "h1", Hash: "h1", Topic: "PROJ-101: login", JiraIssueIDs: []string{"PROJ-101"}, Authors: []string{"Jane"}},
		{CanonicalHash: "h2", Hash: "h2", Topic: "chore: tidy", JiraIssueIDs: []string{}, Authors: []string{"alex"}},
		{CanonicalHash: "h3", Hash: "h3", Topic: "docs: PROJ-200 readme", JiraIssueIDs: []string{"PROJ-200"}, Authors: []string{"alex"}},
		{CanonicalHash: "h4", Hash: "h4", Topic: `Revert "chore: tidy"`, JiraIssueIDs: []string{}, IsRevert: true, Authors: []string{"Bob"}},
	}
	// An unanchored pattern, so "chore" also matches inside `Revert "chore: tidy"`
	// — that is how the excluded revert (h4) is caught despite its "Revert" prefix.
	builder := newBuilderExcluding([]issue.Issue{
		issueWithStatus("PROJ-101", "Login page", "Done"),
		issueWithStatus("PROJ-200", "Readme", "Done"),
	}, `chore|docs`)

	data, err := builder.Build(context.Background(), testCoords(), commits, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	// Only the non-excluded commit remains in the history table.
	if len(data.Commits) != 1 || data.Commits[0].Hash != "h1" {
		t.Fatalf("commit history: got %v, want only h1", data.Commits)
	}

	if data.Summary == nil {
		t.Fatal("expected a summary, got nil")
	}

	// The chore, the docs commit, and the excluded revert all land under Excluded,
	// in their original order.
	wantExcluded := []string{"h2", "h3", "h4"}
	if len(data.Summary.Excluded) != len(wantExcluded) {
		t.Fatalf("excluded: got %v, want %v", data.Summary.Excluded, wantExcluded)
	}

	for i, want := range wantExcluded {
		if data.Summary.Excluded[i].Hash != want {
			t.Errorf("excluded[%d]: got %q, want %q", i, data.Summary.Excluded[i].Hash, want)
		}
	}

	// The excluded revert is not also reported under Reverted.
	if len(data.Summary.Reverted) != 0 {
		t.Errorf("reverted: got %v, want none (the revert was excluded)", data.Summary.Reverted)
	}

	// The docs commit's issue (PROJ-200) must not appear in the summary at all.
	if issueInGroups(data.Summary.ByStatus, "PROJ-200") {
		t.Error("PROJ-200 belonged to an excluded commit and must not be summarized")
	}

	// The kept commit's issue is still summarized.
	if !issueInGroups(data.Summary.ByStatus, "PROJ-101") {
		t.Error("PROJ-101 belonged to a kept commit and should be summarized")
	}
}

// TestBuildExcludedCommitsSurfaceWithoutIssues guards the auditability promise:
// even when the range carries no issue keys (so there is no release summary to
// speak of) and no --jql is given, excluded commits must still be reported under
// "Excluded commits" rather than silently vanishing. Their authors, however,
// leave the Participants list along with the commit.
func TestBuildExcludedCommitsSurfaceWithoutIssues(t *testing.T) {
	commits := []commit.Commit{
		{CanonicalHash: "h1", Hash: "h1", Topic: "chore: bump deps", JiraIssueIDs: []string{}, Authors: []string{"bot"}},
		{CanonicalHash: "h2", Hash: "h2", Topic: "fix: a thing", JiraIssueIDs: []string{}, Authors: []string{"Jane"}},
	}
	builder := newBuilderExcluding(nil, `^chore`)

	data, err := builder.Build(context.Background(), testCoords(), commits, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if data.Summary == nil {
		t.Fatal("expected a summary so the excluded commit is still shown, got nil")
	}

	if len(data.Summary.Excluded) != 1 || data.Summary.Excluded[0].Hash != "h1" {
		t.Errorf("excluded: got %v, want one commit h1", data.Summary.Excluded)
	}

	// The kept commit stays in the history; the excluded one does not.
	if len(data.Commits) != 1 || data.Commits[0].Hash != "h2" {
		t.Errorf("commit history: got %v, want only h2", data.Commits)
	}

	// "bot" only authored the excluded commit, so it is not a participant.
	for _, author := range data.Authors {
		if author == "bot" {
			t.Errorf("participants should not include the excluded commit's author: %v", data.Authors)
		}
	}
}

func TestBuildFlatCommitStatuses(t *testing.T) {
	commits := []commit.Commit{
		{CanonicalHash: "h1", Hash: "h1", Topic: "PROJ-101: login", JiraIssueIDs: []string{"PROJ-101"}, Authors: []string{"Jane"}},
		{CanonicalHash: "h2", Hash: "h2", Topic: "PROJ-200: wip", JiraIssueIDs: []string{"PROJ-200"}, Authors: []string{"Jane"}},
		{CanonicalHash: "h3", Hash: "h3", Topic: `Revert "PROJ-700: oops"`, JiraIssueIDs: []string{"PROJ-700"}, IsRevert: true, Authors: []string{"Jane"}},
		{CanonicalHash: "h4", Hash: "h4", Topic: "chore: tidy", JiraIssueIDs: []string{}, Authors: []string{"Jane"}},
		{CanonicalHash: "h5", Hash: "h5", Topic: "ZZ-9: unknown", JiraIssueIDs: []string{"ZZ-9"}, Authors: []string{"Jane"}},
	}
	builder := newBuilder([]issue.Issue{
		issueWithStatus("PROJ-101", "Login", "Done"),
		issueWithStatus("PROJ-200", "WIP", "In Progress"),
		issueWithStatus("PROJ-700", "Oops", "Ready for Release"),
	})

	data, err := builder.Build(context.Background(), testCoords(), commits, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	// With no release IDs given, the summary defaults to the issues found in the
	// commit range rather than being omitted.
	if data.Summary == nil {
		t.Error("expected a default summary when no release IDs are given")
	}

	want := []string{
		"Done",              // issue in a "done" status
		"In Progress",       // issue still in progress
		"Ready for Release", // reverted commit's issue (no special marker now)
		"No Issue",          // no Jira key at all
		"Unknown",           // Jira key present but issue not returned
	}
	if len(data.Commits) != len(want) {
		t.Fatalf("expected %d commits, got %d", len(want), len(data.Commits))
	}

	for i, w := range want {
		if data.Commits[i].Status != w {
			t.Errorf("commit %d status: got %q, want %q", i, data.Commits[i].Status, w)
		}
	}
}

func TestBuildFormatsLinksAndAuthors(t *testing.T) {
	commits := []commit.Commit{{
		CanonicalHash: "abc1234567890abc1234567890abc1234567890a",
		Hash:          "abc1234",
		Topic:         "PROJ-101: Add login (#42)",
		JiraIssueIDs:  []string{"PROJ-101"},
		Authors:       []string{"zoe", "Anna"},
	}}
	builder := newBuilder([]issue.Issue{issueWithStatus("PROJ-101", "Login", "Done")})

	data, err := builder.Build(context.Background(), testCoords(), commits, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	view := data.Commits[0]

	wantTopic := "[PROJ-101](https://acme.atlassian.net/browse/PROJ-101): Add login " +
		"([#42](https://github.com/acme/widgets/pull/42))"
	if view.FormattedTopic != wantTopic {
		t.Errorf("formatted topic:\n got %q\nwant %q", view.FormattedTopic, wantTopic)
	}

	if view.URL != "https://github.com/acme/widgets/commit/abc1234567890abc1234567890abc1234567890a" {
		t.Errorf("commit url: got %q", view.URL)
	}

	if view.JiraIssueURL != "https://acme.atlassian.net/browse/PROJ-101" {
		t.Errorf("jira url: got %q", view.JiraIssueURL)
	}
	// Authors are sorted case-insensitively and wrapped in backticks.
	if view.Authors != "`Anna`, `zoe`" {
		t.Errorf("authors: got %q, want %q", view.Authors, "`Anna`, `zoe`")
	}

	if len(data.Authors) != 2 || data.Authors[0] != "Anna" || data.Authors[1] != "zoe" {
		t.Errorf("participant list: got %v", data.Authors)
	}
}

// A pipe in a commit subject must be escaped so it cannot break out of its
// Markdown table cell.
func TestBuildEscapesPipesInTopic(t *testing.T) {
	commits := []commit.Commit{{
		CanonicalHash: "abc1234567890abc1234567890abc1234567890a",
		Hash:          "abc1234",
		Topic:         "feat: support a|b syntax",
		Authors:       []string{"dev"},
	}}
	builder := newBuilder(nil)

	data, err := builder.Build(context.Background(), testCoords(), commits, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	want := `feat: support a\|b syntax`
	if got := data.Commits[0].FormattedTopic; got != want {
		t.Errorf("formatted topic:\n got %q\nwant %q", got, want)
	}
}

// When the GitHub repo segment looks like a Jira key (name-digits), the PR-link
// URL it appears in must not be rewritten a second time by the Jira conversion.
func TestBuildDoesNotRewritePRURLAsJira(t *testing.T) {
	coords := notes.Coordinates{
		GithubBaseURL: "https://github.com/acme/widget-2",
		JiraBaseURL:   "https://acme.atlassian.net",
	}
	commits := []commit.Commit{{
		CanonicalHash: "abc1234567890abc1234567890abc1234567890a",
		Hash:          "abc1234",
		Topic:         "fix: a bug (#42)",
		Authors:       []string{"dev"},
	}}
	builder := newBuilder(nil)

	data, err := builder.Build(context.Background(), coords, commits, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	want := "fix: a bug ([#42](https://github.com/acme/widget-2/pull/42))"
	if got := data.Commits[0].FormattedTopic; got != want {
		t.Errorf("formatted topic:\n got %q\nwant %q", got, want)
	}
}

// With no GitHub base URL the notes must degrade gracefully: the commit gets no
// link and a "(#42)" reference is left as written rather than turned into a
// broken relative link.
func TestBuildWithoutGithubBaseURL(t *testing.T) {
	coords := notes.Coordinates{GithubBaseURL: "", JiraBaseURL: "https://acme.atlassian.net"}
	commits := []commit.Commit{{
		CanonicalHash: "abc1234567890abc1234567890abc1234567890a",
		Hash:          "abc1234",
		Topic:         "fix: a bug (#42)",
		Authors:       []string{"dev"},
	}}
	builder := newBuilder(nil)

	data, err := builder.Build(context.Background(), coords, commits, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if data.Header.Repository != "" {
		t.Errorf("repository should be empty, got %q", data.Header.Repository)
	}

	view := data.Commits[0]
	if view.URL != "" {
		t.Errorf("commit url should be empty without a GitHub base URL, got %q", view.URL)
	}

	if view.FormattedTopic != "fix: a bug (#42)" {
		t.Errorf("PR reference should be left as written, got %q", view.FormattedTopic)
	}
}

// issueInGroups reports whether any group holds an issue with the given key.
func issueInGroups(groups []notes.StatusGroup, key string) bool {
	for _, group := range groups {
		for _, view := range group.Issues {
			if view.Key == key {
				return true
			}
		}
	}

	return false
}

// assertSingleIssue checks that groups contains exactly one group with the given
// status holding exactly one issue with the given key.
func assertSingleIssue(t *testing.T, groups []notes.StatusGroup, status, key string) {
	t.Helper()

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d (%v)", len(groups), groups)
	}

	if groups[0].Status != status {
		t.Errorf("group status: got %q, want %q", groups[0].Status, status)
	}

	if len(groups[0].Issues) != 1 || groups[0].Issues[0].Key != key {
		t.Errorf("group issues: got %v, want [%s]", groups[0].Issues, key)
	}
}
