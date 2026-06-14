package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alexander-danilenko/shipnotes/internal/infrastructure/config"
)

// configVarNames lists every environment variable the loader reads.
var configVarNames = []string{
	"SHIPNOTES_REPO_ORG",
	"SHIPNOTES_REPO_NAME",
	"SHIPNOTES_JIRA_BASE_URL",
	"SHIPNOTES_GITHUB_URL",
	"SHIPNOTES_JIRA_EMAIL",
	"SHIPNOTES_JIRA_TOKEN",
}

// clearConfigEnv unsets every config variable for the duration of a test (and
// restores any previous values afterwards), so the test sees only what it sets
// itself — important when checking that values come from a .env file, which the
// loader skips for variables already present in the environment.
func clearConfigEnv(t *testing.T) {
	t.Helper()

	for _, name := range configVarNames {
		// t.Setenv records the original value and restores it when the test ends;
		// unsetting right after gives the test the clean slate it relies on.
		if _, ok := os.LookupEnv(name); ok {
			t.Setenv(name, "")
		}

		_ = os.Unsetenv(name)
	}
}

// setValidEnv sets every required variable to a valid value.
func setValidEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SHIPNOTES_REPO_ORG", "acme")
	t.Setenv("SHIPNOTES_REPO_NAME", "widgets")
	t.Setenv("SHIPNOTES_JIRA_BASE_URL", "https://acme.atlassian.net")
	t.Setenv("SHIPNOTES_GITHUB_URL", "https://github.com/acme/widgets")
	t.Setenv("SHIPNOTES_JIRA_EMAIL", "ci@acme.com")
	t.Setenv("SHIPNOTES_JIRA_TOKEN", "secret-token")
}

func TestLoadValid(t *testing.T) {
	setValidEnv(t)

	settings, err := config.Load("", config.Defaults{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if settings.GitRepoOrganization != "acme" || settings.JiraEmail != "ci@acme.com" {
		t.Errorf("unexpected settings: %+v", settings)
	}
}

func TestLoadReportsEveryProblem(t *testing.T) {
	// Override each variable with an invalid value so the result does not depend
	// on whatever is (or isn't) in the real environment or a .env file.
	t.Setenv("SHIPNOTES_REPO_ORG", "")
	t.Setenv("SHIPNOTES_REPO_NAME", "")
	t.Setenv("SHIPNOTES_JIRA_BASE_URL", "not-a-url")
	t.Setenv("SHIPNOTES_GITHUB_URL", "also-not-a-url")
	t.Setenv("SHIPNOTES_JIRA_EMAIL", "not-an-email")
	t.Setenv("SHIPNOTES_JIRA_TOKEN", "")

	_, err := config.Load("", config.Defaults{})
	if err == nil {
		t.Fatal("expected a validation error")
	}

	validationErr, ok := config.AsValidationError(err)
	if !ok {
		t.Fatalf("expected a *ValidationError, got %T", err)
	}

	if len(validationErr.Problems) != 6 {
		t.Errorf("expected 6 problems, got %d: %v", len(validationErr.Problems), validationErr.Problems)
	}

	// The reported field names must be the new SHIPNOTES_* names, so the
	// user-facing error points at the variables the loader actually reads.
	reported := make(map[string]bool, len(validationErr.Problems))
	for _, problem := range validationErr.Problems {
		reported[problem.Field] = true
	}

	for _, name := range configVarNames {
		if !reported[name] {
			t.Errorf("expected a problem reported for %q, got fields %v", name, reported)
		}
	}
}

func TestLoadAcceptsURLWithPath(t *testing.T) {
	setValidEnv(t)
	t.Setenv("SHIPNOTES_JIRA_BASE_URL", "https://acme.atlassian.net/jira")

	if _, err := config.Load("", config.Defaults{}); err != nil {
		t.Errorf("URL with a path should be valid: %v", err)
	}
}

func TestLoadUsesDefaultsWhenEnvMissing(t *testing.T) {
	// The three repository-coordinate variables are blank, so the inferred
	// defaults must be used. The Jira variables remain required.
	t.Setenv("SHIPNOTES_REPO_ORG", "")
	t.Setenv("SHIPNOTES_REPO_NAME", "")
	t.Setenv("SHIPNOTES_GITHUB_URL", "")
	t.Setenv("SHIPNOTES_JIRA_BASE_URL", "https://acme.atlassian.net")
	t.Setenv("SHIPNOTES_JIRA_EMAIL", "ci@acme.com")
	t.Setenv("SHIPNOTES_JIRA_TOKEN", "secret-token")

	settings, err := config.Load("", config.Defaults{
		GitRepoOrganization: "acme",
		GitRepoName:         "widgets",
		GithubBaseURL:       "https://github.com/acme/widgets",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if settings.GitRepoOrganization != "acme" || settings.GitRepoName != "widgets" {
		t.Errorf("expected inferred org/repo, got %+v", settings)
	}

	if settings.GithubBaseURL != "https://github.com/acme/widgets" {
		t.Errorf("expected inferred GitHub base URL, got %q", settings.GithubBaseURL)
	}
}

func TestLoadEnvOverridesDefaults(t *testing.T) {
	// Every variable is set in the environment; the defaults must be ignored.
	setValidEnv(t)

	settings, err := config.Load("", config.Defaults{
		GitRepoOrganization: "other",
		GitRepoName:         "thing",
		GithubBaseURL:       "https://github.com/other/thing",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if settings.GithubBaseURL != "https://github.com/acme/widgets" {
		t.Errorf("environment should win over defaults, got %q", settings.GithubBaseURL)
	}
}

func TestLoadReadsExplicitEnvFile(t *testing.T) {
	clearConfigEnv(t)

	path := filepath.Join(t.TempDir(), "custom.env")
	content := "SHIPNOTES_REPO_ORG=acme\n" +
		"SHIPNOTES_REPO_NAME=widgets\n" +
		"SHIPNOTES_JIRA_BASE_URL=https://acme.atlassian.net\n" +
		"SHIPNOTES_GITHUB_URL=https://github.com/acme/widgets\n" +
		"SHIPNOTES_JIRA_EMAIL=ci@acme.com\n" +
		"SHIPNOTES_JIRA_TOKEN=secret-token\n"

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing temp env file: %v", err)
	}

	settings, err := config.Load(path, config.Defaults{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if settings.GitRepoName != "widgets" || settings.JiraEmail != "ci@acme.com" {
		t.Errorf("values were not loaded from --env-file: %+v", settings)
	}
}

func TestLoadExplicitEnvFileMissingIsError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.env")

	if _, err := config.Load(missing, config.Defaults{}); err == nil {
		t.Fatal("expected an error when --env-file points at a missing file")
	}
}
