package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDotEnvLine(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantKey   string
		wantValue string
		wantOK    bool
	}{
		{name: "simple", line: "FOO=bar", wantKey: "FOO", wantValue: "bar", wantOK: true},
		{name: "spaces trimmed", line: "  FOO = bar  ", wantKey: "FOO", wantValue: "bar", wantOK: true},
		{name: "export prefix", line: "export FOO=bar", wantKey: "FOO", wantValue: "bar", wantOK: true},
		{name: "double quotes", line: `FOO="quoted value"`, wantKey: "FOO", wantValue: "quoted value", wantOK: true},
		{name: "single quotes", line: `FOO='single'`, wantKey: "FOO", wantValue: "single", wantOK: true},
		{name: "empty value", line: "FOO=", wantKey: "FOO", wantValue: "", wantOK: true},
		{name: "value with equals", line: "URL=https://x?a=b", wantKey: "URL", wantValue: "https://x?a=b", wantOK: true},
		{name: "comment", line: "# a comment", wantOK: false},
		{name: "blank", line: "   ", wantOK: false},
		{name: "no equals", line: "JUSTTEXT", wantOK: false},
		{name: "no name", line: "=value", wantOK: false},
		{name: "unbalanced quote kept", line: `FOO="open`, wantKey: "FOO", wantValue: `"open`, wantOK: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			key, value, ok := parseDotEnvLine(tc.line)
			if ok != tc.wantOK {
				t.Fatalf("ok: got %v, want %v", ok, tc.wantOK)
			}

			if !ok {
				return
			}

			if key != tc.wantKey || value != tc.wantValue {
				t.Errorf("got (%q, %q), want (%q, %q)", key, value, tc.wantKey, tc.wantValue)
			}
		})
	}
}

func TestFindDotEnvFileWalksUp(t *testing.T) {
	root := t.TempDir()

	envPath := filepath.Join(root, ".env")
	if err := os.WriteFile(envPath, []byte("FOO=bar\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	t.Chdir(nested)

	got, found := findDotEnvFile()
	if !found {
		t.Fatal("expected to find .env by walking up")
	}
	// Compare via EvalSymlinks to absorb macOS /private/var symlinking.
	gotResolved, _ := filepath.EvalSymlinks(got)

	wantResolved, _ := filepath.EvalSymlinks(envPath)
	if gotResolved != wantResolved {
		t.Errorf("got %q, want %q", gotResolved, wantResolved)
	}
}

func TestLoadDotEnvFilePrecedence(t *testing.T) {
	root := t.TempDir()

	envBody := "FROM_FILE_ONLY=file-value\nALREADY_SET=file-value\n"
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(envBody), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	t.Chdir(root)

	// A real environment variable must NOT be overwritten by the file.
	t.Setenv("ALREADY_SET", "env-value")
	// The file-only var would otherwise leak past the test, so clean it up.
	t.Cleanup(func() { _ = os.Unsetenv("FROM_FILE_ONLY") })

	if err := loadDotEnvFile(""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := os.Getenv("ALREADY_SET"); got != "env-value" {
		t.Errorf("env var should win over .env: got %q, want %q", got, "env-value")
	}

	if got := os.Getenv("FROM_FILE_ONLY"); got != "file-value" {
		t.Errorf("file-only var: got %q, want %q", got, "file-value")
	}
}

func TestLoadDotEnvFileExplicitPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom.env")
	if err := os.WriteFile(path, []byte("EXPLICIT_VAR=explicit-value\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	t.Cleanup(func() { _ = os.Unsetenv("EXPLICIT_VAR") })

	if err := loadDotEnvFile(path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := os.Getenv("EXPLICIT_VAR"); got != "explicit-value" {
		t.Errorf("explicit env file not loaded: got %q", got)
	}
}

func TestLoadDotEnvFileExplicitMissingIsError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.env")

	if err := loadDotEnvFile(missing); err == nil {
		t.Fatal("expected an error for a missing explicit env file")
	}
}
