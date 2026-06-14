package cli

import (
	"os"
	"path/filepath"
)

// resolveRepoDir decides which directory git commands run in, using this
// priority order:
//  1. the --repo-dir flag
//  2. the SHIPNOTES_REPO_DIR environment variable
//  3. the PWD environment variable (the user's original directory)
//  4. the current working directory
//
// For options 3 and 4 it walks upward looking for a .git directory.
func resolveRepoDir(repoDirFlag string) (string, error) {
	if repoDirFlag != "" {
		return filepath.Abs(repoDirFlag)
	}

	if envDir := os.Getenv("SHIPNOTES_REPO_DIR"); envDir != "" {
		return filepath.Abs(envDir)
	}

	if pwd := os.Getenv("PWD"); pwd != "" {
		return findGitRoot(pwd), nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	return findGitRoot(cwd), nil
}

// findGitRoot walks upward from start looking for a directory that contains a
// .git entry. It returns the first match, or start itself if none is found.
func findGitRoot(start string) string {
	dir := start
	for {
		//nolint:gosec // G703: we deliberately walk the user's own directory tree to find the repo root.
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return start
		}

		dir = parent
	}
}
