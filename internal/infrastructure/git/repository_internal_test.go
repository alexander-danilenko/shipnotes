package git

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// absentHash is well-formed but will not exist in a freshly created repository,
// standing in for a source commit a shallow clone or a pruned branch left behind.
const absentHash = "0123456789abcdef0123456789abcdef01234567"

func TestLogMarksAlreadyShippedCommits(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed; skipping integration test")
	}

	dir := t.TempDir()
	runGit(t, dir, "init")

	writeAndCommit(t, dir, "base.txt", "base", "Base commit")
	writeAndCommit(t, dir, "fix.txt", "fix", "AB-1: original fix")
	shipped := headHash(t, dir)

	// Everything up to and including this commit is "already released": it is
	// the ref the range is taken from, so `shipped` sits behind it.
	writeAndCommit(t, dir, "cut.txt", "cut", "Release cut")
	rangeStart := headHash(t, dir)

	writeAndCommit(t, dir, "d.txt", "d", "AB-1: re-landed fix\n\n(cherry picked from commit "+shipped+")")
	inRange := headHash(t, dir)

	writeAndCommit(t, dir, "e.txt", "e", "AB-2: picked from inside the range\n\n(cherry picked from commit "+inRange+")")
	writeAndCommit(t, dir, "f.txt", "f", "AB-3: source not in this clone\n\n(cherry picked from commit "+absentHash+")")
	writeAndCommit(t, dir, "g.txt", "g", "AB-4: backported twice\n\n(cherry picked from commit "+shipped+")\n"+
		"(cherry picked from commit "+absentHash+")")
	writeAndCommit(t, dir, "h.txt", "h", "AB-5: ordinary new work")

	commits, err := New(dir, terminal.New(io.Discard)).Log(context.Background(), rangeStart)
	if err != nil {
		t.Fatalf("log: %v", err)
	}

	want := map[string]bool{
		"AB-1: re-landed fix":                true,
		"AB-2: picked from inside the range": false,
		"AB-3: source not in this clone":     false,
		"AB-4: backported twice":             true,
		"AB-5: ordinary new work":            false,
	}

	if len(commits) != len(want) {
		t.Fatalf("expected %d commits in the range, got %d", len(want), len(commits))
	}

	for _, c := range commits {
		expected, known := want[c.Topic]
		if !known {
			t.Errorf("unexpected commit in range: %q", c.Topic)

			continue
		}

		if c.IsAlreadyShipped != expected {
			t.Errorf("%q: IsAlreadyShipped = %v, want %v", c.Topic, c.IsAlreadyShipped, expected)
		}
	}
}

func TestIsAncestorRejectsMalformedHash(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed; skipping integration test")
	}

	dir := t.TempDir()
	runGit(t, dir, "init")
	writeAndCommit(t, dir, "a.txt", "one", "First commit")

	repo := New(dir, terminal.New(io.Discard))

	// The adapter never hands git an argument it has not format-checked itself,
	// so a hash the commit domain would have rejected is refused here too.
	for _, hash := range []string{"abc1234; rm -rf /", "--upload-pack=evil", "abc12", ""} {
		if repo.isAncestor(context.Background(), hash, "HEAD") {
			t.Errorf("expected %q to be refused before reaching git", hash)
		}
	}
}

// headHash returns the full hash of the repository's current HEAD.
func headHash(t *testing.T, dir string) string {
	t.Helper()

	return gitOutput(t, dir, "rev-parse", "HEAD")
}

// gitOutput runs a git command in dir and returns its trimmed stdout.
func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v failed: %v", args, err)
	}

	return strings.TrimSpace(string(out))
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
