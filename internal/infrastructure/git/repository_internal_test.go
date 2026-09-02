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

	repo := New(dir, terminal.New(io.Discard), "")
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

func TestIsValidRefName(t *testing.T) {
	valid := []string{"master", "main", "origin/main", "release/2.1", "v1.0.0", "a"}
	// An argument starting with "-" would be read by git as an option, not a ref;
	// the rest are shapes a branch name cannot take.
	invalid := []string{"", "-", "--upload-pack=evil", "-b", "origin/main; rm -rf /", "with space", "/leading"}

	for _, ref := range valid {
		if !IsValidRefName(ref) {
			t.Errorf("expected %q to be valid", ref)
		}
	}

	for _, ref := range invalid {
		if IsValidRefName(ref) {
			t.Errorf("expected %q to be invalid", ref)
		}
	}
}

// setUpCherryPickedRelease builds the workflow this feature exists for: a fix
// lands on develop, is cherry-picked out to the release branch, and ships from
// there while develop keeps moving. It returns the range start and leaves HEAD
// on develop.
//
// The commit that ends up inside the develop range is the *original*. Git wrote
// the `cherry picked from commit …` trailer on the copy over on master, so
// nothing in the range itself records that the change already shipped.
func setUpCherryPickedRelease(t *testing.T, dir string) (rangeStart string) {
	t.Helper()

	runGit(t, dir, "init", "-b", "master")
	writeAndCommit(t, dir, "base.txt", "base", "Base commit")
	rangeStart = headHash(t, dir)

	runGit(t, dir, "checkout", "-b", "develop")
	writeAndCommit(t, dir, "fix.txt", "fix", "AB-1: urgent fix")
	original := headHash(t, dir)
	writeAndCommit(t, dir, "feature.txt", "feature", "AB-2: unrelated new work")

	runGit(t, dir, "checkout", "master")
	runGit(t, dir, "cherry-pick", "-x", original)
	runGit(t, dir, "checkout", "develop")

	return rangeStart
}

func TestLogMarksCommitsTheReleaseBranchAlreadyTook(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed; skipping integration test")
	}

	dir := t.TempDir()
	rangeStart := setUpCherryPickedRelease(t, dir)

	commits, err := New(dir, terminal.New(io.Discard), "master").Log(context.Background(), rangeStart)
	if err != nil {
		t.Fatalf("log: %v", err)
	}

	want := map[string]bool{
		"AB-1: urgent fix":         true,
		"AB-2: unrelated new work": false,
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

// TestLogWithoutAUsableShippedRef proves the feature is inert rather than noisy
// when there is no release branch to read: no ref, a ref that does not exist,
// and a ref shaped like a git option all leave every commit unflagged.
func TestLogWithoutAUsableShippedRef(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed; skipping integration test")
	}

	for name, ref := range map[string]string{
		"no ref":             "",
		"ref does not exist": "no-such-branch",
		"ref is an option":   "--upload-pack=evil",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			rangeStart := setUpCherryPickedRelease(t, dir)

			commits, err := New(dir, terminal.New(io.Discard), ref).Log(context.Background(), rangeStart)
			if err != nil {
				t.Fatalf("log: %v", err)
			}

			for _, c := range commits {
				if c.IsAlreadyShipped {
					t.Errorf("%q: flagged already shipped with no usable release ref", c.Topic)
				}
			}
		})
	}
}

// setUpDuplicateInRange builds the other shape of the same workflow: the fix is
// applied to the release branch directly, ahead of the release, and develop is
// merged in afterwards. Both copies of the change therefore sit inside one
// range, which is read from the release branch.
//
// withTrailer picks which evidence the range carries. With `-x` the copy names
// the original outright; without it the two commits are linked by nothing but
// identical content.
func setUpDuplicateInRange(t *testing.T, dir string, withTrailer bool) (rangeStart string) {
	t.Helper()

	runGit(t, dir, "init", "-b", "master")
	writeAndCommit(t, dir, "base.txt", "base", "Base commit")
	rangeStart = headHash(t, dir)

	runGit(t, dir, "checkout", "-b", "develop")
	writeAndCommit(t, dir, "fix.txt", "fix", "AB-1: urgent fix")
	original := headHash(t, dir)
	writeAndCommit(t, dir, "feature.txt", "feature", "AB-2: unrelated new work")

	runGit(t, dir, "checkout", "master")
	// Master has to move on its own before the pick. Otherwise the copy would
	// share a parent, a tree, a message and a timestamp with the original and git
	// would hand back the very same commit object — no duplicate to detect.
	writeAndCommit(t, dir, "prep.txt", "prep", "AB-3: release prep")

	if withTrailer {
		runGit(t, dir, "cherry-pick", "-x", original)
	} else {
		runGit(t, dir, "cherry-pick", original)
	}

	// The merge brings the original over, so master now holds both copies.
	runGit(t, dir, "merge", "--no-ff", "-m", "Merge branch 'develop'", "develop")

	return rangeStart
}

// TestLogMarksDuplicatesInsideTheRange covers the release-branch reading:
// the copy applied to master directly and the original that arrives with the
// merge are both flagged, so their issue has nothing left to ship. Unrelated
// work in the same range is untouched.
func TestLogMarksDuplicatesInsideTheRange(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed; skipping integration test")
	}

	for name, withTrailer := range map[string]bool{
		"cherry-picked with -x":    true,
		"cherry-picked without -x": false,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			rangeStart := setUpDuplicateInRange(t, dir, withTrailer)

			commits, err := New(dir, terminal.New(io.Discard), "master").Log(context.Background(), rangeStart)
			if err != nil {
				t.Fatalf("log: %v", err)
			}

			// A merge carries no diff of its own, so it can never pair with anything.
			want := map[string]bool{
				"AB-1: urgent fix":         true,
				"AB-2: unrelated new work": false,
				"AB-3: release prep":       false,
				"Merge branch 'develop'":   false,
			}

			var fixes int

			for _, c := range commits {
				expected, known := want[c.Topic]
				if !known {
					t.Errorf("unexpected commit in range: %q", c.Topic)

					continue
				}

				if c.Topic == "AB-1: urgent fix" {
					fixes++
				}

				if c.IsAlreadyShipped != expected {
					t.Errorf("%q: IsAlreadyShipped = %v, want %v", c.Topic, c.IsAlreadyShipped, expected)
				}
			}

			if fixes != 2 {
				t.Fatalf("expected both copies of the fix in the range, got %d", fixes)
			}
		})
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
