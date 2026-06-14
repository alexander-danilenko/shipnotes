package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRepoDirPriority(t *testing.T) {
	flagDir := t.TempDir()
	envDir := t.TempDir()

	// 1) The --repo-dir flag wins over everything.
	t.Setenv("SHIPNOTES_REPO_DIR", envDir)

	got, err := resolveRepoDir(flagDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != flagDir {
		t.Errorf("flag should win: got %q, want %q", got, flagDir)
	}

	// 2) With no flag, SHIPNOTES_REPO_DIR is used.
	got, err = resolveRepoDir("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != envDir {
		t.Errorf("env var should be used: got %q, want %q", got, envDir)
	}
}

func TestFindGitRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o750); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o750); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	// EvalSymlinks normalizes macOS's /private/var symlink so the comparison holds.
	wantRoot, _ := filepath.EvalSymlinks(root)
	if got, _ := filepath.EvalSymlinks(findGitRoot(nested)); got != wantRoot {
		t.Errorf("walk-up: got %q, want %q", got, wantRoot)
	}

	// A directory with no .git anywhere above it resolves to itself.
	noGit := t.TempDir()
	if got := findGitRoot(noGit); got != noGit {
		t.Errorf("no .git: got %q, want %q", got, noGit)
	}
}
