package git

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/alexander-danilenko/shipnotes/internal/infrastructure/terminal"
)

func TestIsValidHashFormat(t *testing.T) {
	valid := []string{"HEAD", "HEAD~1", "HEAD~25", "abc1234", "ABCDEF0", "0123456789abcdef0123456789abcdef01234567", "abc1234~3"}
	invalid := []string{"", "   ", "HEAD~", "HEAD ", "abc12", "xyz1234", "abc1234;rm -rf", "0123456789abcdef0123456789abcdef012345678"}

	for _, ref := range valid {
		if !isValidHashFormat(ref) {
			t.Errorf("expected %q to be valid", ref)
		}
	}

	for _, ref := range invalid {
		if isValidHashFormat(ref) {
			t.Errorf("expected %q to be invalid", ref)
		}
	}
}

func TestIsNotARepoError(t *testing.T) {
	if !isNotARepoError(errors.New("fatal: not a git repository (or any parent)")) {
		t.Error("should detect 'not a git repository'")
	}

	if !isNotARepoError(errors.New("git: command not found")) {
		t.Error("should detect 'not found'")
	}

	if isNotARepoError(errors.New("some other failure")) {
		t.Error("should not match an unrelated error")
	}

	if isNotARepoError(nil) {
		t.Error("nil is not an error")
	}
}

func TestRepositoryAgainstRealRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed; skipping integration test")
	}

	dir := t.TempDir()
	runGit(t, dir, "init")
	writeAndCommit(t, dir, "a.txt", "one", "First commit")
	writeAndCommit(t, dir, "b.txt", "two", "AB-7: Second commit")

	repo := New(dir, terminal.New(io.Discard))
	ctx := context.Background()

	valid, err := repo.Validate(ctx, "HEAD")
	if err != nil || !valid {
		t.Fatalf("HEAD should be valid: valid=%v err=%v", valid, err)
	}

	// Well-formed but non-existent ref: (false, nil), not an error. We use
	// HEAD~50 here because git treats it as a syntactically valid reference
	// without checking whether that many ancestors actually exist.
	missing, err := repo.Validate(ctx, "HEAD~50")
	if err != nil {
		t.Fatalf("unexpected error for missing commit: %v", err)
	}

	if missing {
		t.Error("a non-existent ref should not validate")
	}

	// Badly-formatted ref is a usage error.
	if _, err := repo.Validate(ctx, "nope; rm -rf"); err == nil {
		t.Error("expected a format error for an invalid ref")
	}

	commits, err := repo.Log(ctx, "HEAD~1")
	if err != nil {
		t.Fatalf("log: %v", err)
	}

	if len(commits) != 1 {
		t.Fatalf("expected 1 commit since HEAD~1, got %d", len(commits))
	}

	if commits[0].Topic != "AB-7: Second commit" {
		t.Errorf("topic: got %q", commits[0].Topic)
	}

	if len(commits[0].JiraIssueIDs) != 1 || commits[0].JiraIssueIDs[0] != "AB-7" {
		t.Errorf("jira ids: got %v, want [AB-7]", commits[0].JiraIssueIDs)
	}
}

// runGit runs a git command in dir with a fixed identity, failing the test on
// error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	full := append([]string{
		"-c", "user.email=test@example.com",
		"-c", "user.name=Test User",
		"-c", "commit.gpgsign=false",
	}, args...)
	cmd := exec.CommandContext(t.Context(), "git", full...)

	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

// writeAndCommit writes a file and commits it.
func writeAndCommit(t *testing.T, dir, name, content, message string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	runGit(t, dir, "add", name)
	runGit(t, dir, "commit", "-m", message)
}
