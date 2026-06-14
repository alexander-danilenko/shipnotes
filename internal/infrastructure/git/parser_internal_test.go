package git

import (
	"reflect"
	"testing"

	"github.com/alexander-danilenko/shipnotes/internal/domain/commit"
)

// sampleLog uses the exact format the tool asks git to produce:
//
//	commit %H%nAuthor: %an <%ae>%nDate: %ai%n%n%B%n---%n
//
// The expected values below are derived directly from this sample log.
const sampleLog = `commit aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
Author: Jane Doe <jane@example.com>
Date: 2024-01-15 10:30:00 +0000

CX-123: Add login page (#42)

Implements the login flow.

Co-authored-by: Bob Smith <bob@example.com>
---

commit bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
Author: Alex O'Hara <alex@example.com>
Date: 2024-01-14 09:00:00 +0000

Revert "CX-99: risky change"

This reverts commit deadbeef.
---

commit cccccccccccccccccccccccccccccccccccccccc
Author: Single Name <s@example.com>
Date: 2024-01-13 08:00:00 +0000

chore: tidy up, no ticket
---

commit eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
Author: Alex O'Hara <alex@example.com>
Date: 2024-01-12 07:00:00 +0000

Reapply "CX-99: risky change"

This reverts commit cafebabe.
---
`

func TestParse(t *testing.T) {
	commits := parse(sampleLog)

	if len(commits) != 4 {
		t.Fatalf("expected 4 commits, got %d", len(commits))
	}

	assertCommit(t, commits[0], commit.Commit{
		CanonicalHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Hash:          "aaaaaaa",
		Topic:         "CX-123: Add login page (#42)",
		IsRevert:      false,
		Authors:       []string{"Jane Doe", "Bob Smith"},
		JiraIssueIDs:  []string{"CX-123"},
	})

	assertCommit(t, commits[1], commit.Commit{
		CanonicalHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Hash:          "bbbbbbb",
		Topic:         `Revert "CX-99: risky change"`,
		IsRevert:      true,
		Authors:       []string{"Alex O'Hara"},
		JiraIssueIDs:  []string{"CX-99"},
	})

	assertCommit(t, commits[2], commit.Commit{
		CanonicalHash: "cccccccccccccccccccccccccccccccccccccccc",
		Hash:          "ccccccc",
		Topic:         "chore: tidy up, no ticket",
		IsRevert:      false,
		Authors:       []string{"Single Name"},
		JiraIssueIDs:  []string{},
	})

	assertCommit(t, commits[3], commit.Commit{
		CanonicalHash: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		Hash:          "eeeeeee",
		Topic:         `Reapply "CX-99: risky change"`,
		IsReapply:     true,
		Authors:       []string{"Alex O'Hara"},
		JiraIssueIDs:  []string{"CX-99"},
	})
}

func TestParseEmptyLog(t *testing.T) {
	if commits := parse(""); len(commits) != 0 {
		t.Errorf("expected no commits for empty log, got %d", len(commits))
	}
}

func TestParseDeduplicatesJiraKeys(t *testing.T) {
	log := `commit dddddddddddddddddddddddddddddddddddddddd
Author: Dev <dev@example.com>
Date: 2024-01-10 08:00:00 +0000

AB-1 and AB-1 again plus CD-2

body
---
`

	commits := parse(log)
	if len(commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(commits))
	}

	want := []string{"AB-1", "CD-2"}
	if !reflect.DeepEqual(commits[0].JiraIssueIDs, want) {
		t.Errorf("jira ids: got %v, want %v", commits[0].JiraIssueIDs, want)
	}
}

// A commit with an empty message must not surface the format's "---" separator
// line as its topic.
func TestParseEmptyMessageTopic(t *testing.T) {
	log := "commit ffffffffffffffffffffffffffffffffffffffff\n" +
		"Author: Dev <dev@example.com>\n" +
		"Date: 2024-01-09 08:00:00 +0000\n" +
		"\n" +
		"\n" +
		"---\n"

	commits := parse(log)
	if len(commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(commits))
	}

	if commits[0].Topic != "" {
		t.Errorf("topic: got %q, want empty", commits[0].Topic)
	}
}

// assertCommit compares the fields that affect output. Authors and Jira IDs are
// compared as sets, since their order does not influence the final document.
func assertCommit(t *testing.T, got, want commit.Commit) {
	t.Helper()

	if got.CanonicalHash != want.CanonicalHash {
		t.Errorf("canonical hash: got %q, want %q", got.CanonicalHash, want.CanonicalHash)
	}

	if got.Hash != want.Hash {
		t.Errorf("hash: got %q, want %q", got.Hash, want.Hash)
	}

	if got.Topic != want.Topic {
		t.Errorf("topic: got %q, want %q", got.Topic, want.Topic)
	}

	if got.IsRevert != want.IsRevert {
		t.Errorf("is_revert: got %v, want %v", got.IsRevert, want.IsRevert)
	}

	if got.IsReapply != want.IsReapply {
		t.Errorf("is_reapply: got %v, want %v", got.IsReapply, want.IsReapply)
	}

	if !equalSet(got.Authors, want.Authors) {
		t.Errorf("authors: got %v, want %v", got.Authors, want.Authors)
	}

	if !equalSet(got.JiraIssueIDs, want.JiraIssueIDs) {
		t.Errorf("jira ids: got %v, want %v", got.JiraIssueIDs, want.JiraIssueIDs)
	}
}

// equalSet reports whether two slices contain the same elements, ignoring order.
func equalSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	counts := map[string]int{}
	for _, x := range a {
		counts[x]++
	}

	for _, x := range b {
		counts[x]--
	}

	for _, c := range counts {
		if c != 0 {
			return false
		}
	}

	return true
}
