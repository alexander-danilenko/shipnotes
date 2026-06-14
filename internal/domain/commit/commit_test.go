package commit_test

import (
	"reflect"
	"testing"

	"github.com/alexander-danilenko/shipnotes/internal/domain/commit"
)

func TestExtractIssueIDs(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{name: "empty", text: "", want: []string{}},
		{name: "none", text: "chore: tidy up, no ticket", want: []string{}},
		{name: "single", text: "PROJ-123: Add login page", want: []string{"PROJ-123"}},
		{name: "deduplicated in first-seen order", text: "AB-1 and AB-1 again plus CD-2", want: []string{"AB-1", "CD-2"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := commit.ExtractIssueIDs(tc.text); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsRevertTopic(t *testing.T) {
	revert := []string{`Revert "PROJ-99: risky change"`, "revert: undo it", "  REVERT something"}
	notRevert := []string{"PROJ-1: add feature", `Reapply "PROJ-99: risky change"`, ""}

	for _, topic := range revert {
		if !commit.IsRevertTopic(topic) {
			t.Errorf("expected %q to be a revert", topic)
		}
	}

	for _, topic := range notRevert {
		if commit.IsRevertTopic(topic) {
			t.Errorf("expected %q not to be a revert", topic)
		}
	}
}

func TestIsReapplyTopic(t *testing.T) {
	// Only git's exact `Reapply "…"` shape counts.
	if !commit.IsReapplyTopic(`Reapply "PROJ-99: risky change"`) {
		t.Error(`expected Reapply "…" to be a reapply`)
	}

	notReapply := []string{`Revert "PROJ-99"`, "reapply the change", `Reapply PROJ-99`, ""}
	for _, topic := range notReapply {
		if commit.IsReapplyTopic(topic) {
			t.Errorf("expected %q not to be a reapply", topic)
		}
	}
}

func TestPrimaryIssueID(t *testing.T) {
	if got := (commit.Commit{JiraIssueIDs: []string{"PROJ-1", "PROJ-2"}}).PrimaryIssueID(); got != "PROJ-1" {
		t.Errorf("got %q, want PROJ-1", got)
	}

	if got := (commit.Commit{}).PrimaryIssueID(); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
