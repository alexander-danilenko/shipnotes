package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alexander-danilenko/shipnotes/internal/infrastructure/config"
)

// configVarNames lists every environment variable that affects the loader: the
// three Jira variables it validates plus the GitHub repo (resolved elsewhere,
// but cleared here so a stray value cannot leak into a test).
var configVarNames = []string{
	"SHIPNOTES_JIRA_BASE_URL",
	"SHIPNOTES_JIRA_EMAIL",
	"SHIPNOTES_JIRA_TOKEN",
	config.EnvGithubRepo,
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

// setValidJiraEnv sets every required Jira variable to a valid value.
func setValidJiraEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SHIPNOTES_JIRA_BASE_URL", "https://acme.atlassian.net")
	t.Setenv("SHIPNOTES_JIRA_EMAIL", "ci@acme.com")
	t.Setenv("SHIPNOTES_JIRA_TOKEN", "secret-token")
}

func TestLoadValid(t *testing.T) {
	clearConfigEnv(t)
	setValidJiraEnv(t)

	settings, err := config.Load(config.Overrides{}, "https://github.com/acme/widgets")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if settings.JiraEmail != "ci@acme.com" || settings.GithubBaseURL != "https://github.com/acme/widgets" {
		t.Errorf("unexpected settings: %+v", settings)
	}
}

func TestLoadReportsEveryProblem(t *testing.T) {
	// Override each Jira variable with an invalid value so the result does not
	// depend on whatever is (or isn't) in the real environment or a .env file.
	t.Setenv("SHIPNOTES_JIRA_BASE_URL", "not-a-url")
	t.Setenv("SHIPNOTES_JIRA_EMAIL", "not-an-email")
	t.Setenv("SHIPNOTES_JIRA_TOKEN", "")

	// An empty GitHub URL is allowed (it is optional), so it adds no problem.
	_, err := config.Load(config.Overrides{}, "")
	if err == nil {
		t.Fatal("expected a validation error")
	}

	validationErr, ok := config.AsValidationError(err)
	if !ok {
		t.Fatalf("expected a *ValidationError, got %T", err)
	}

	if len(validationErr.Problems) != 3 {
		t.Errorf("expected 3 problems, got %d: %v", len(validationErr.Problems), validationErr.Problems)
	}

	// The reported field names must be the Jira variable names, so the
	// user-facing error points at the variables the loader actually validates.
	reported := make(map[string]bool, len(validationErr.Problems))
	for _, problem := range validationErr.Problems {
		reported[problem.Field] = true
	}

	for _, name := range []string{"SHIPNOTES_JIRA_BASE_URL", "SHIPNOTES_JIRA_EMAIL", "SHIPNOTES_JIRA_TOKEN"} {
		if !reported[name] {
			t.Errorf("expected a problem reported for %q, got fields %v", name, reported)
		}
	}
}

func TestLoadAcceptsURLWithPath(t *testing.T) {
	clearConfigEnv(t)
	setValidJiraEnv(t)
	t.Setenv("SHIPNOTES_JIRA_BASE_URL", "https://acme.atlassian.net/jira")

	if _, err := config.Load(config.Overrides{}, ""); err != nil {
		t.Errorf("URL with a path should be valid: %v", err)
	}
}

func TestFlagOverridesEnv(t *testing.T) {
	// Every Jira variable is set in the environment; the flag overrides must win.
	setValidJiraEnv(t)

	settings, err := config.Load(config.Overrides{
		JiraBaseURL: "https://flag.atlassian.net",
		JiraEmail:   "flag@acme.com",
		JiraToken:   "flag-token",
	}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if settings.JiraBaseURL != "https://flag.atlassian.net" ||
		settings.JiraEmail != "flag@acme.com" ||
		settings.JiraReadAPIToken != "flag-token" {
		t.Errorf("flag overrides should win over the environment, got %+v", settings)
	}
}

func TestFlagSuppliesMissingValue(t *testing.T) {
	// Nothing in the environment: the flags alone must satisfy validation, so the
	// tool can run without a .env file.
	clearConfigEnv(t)

	settings, err := config.Load(config.Overrides{
		JiraBaseURL: "https://acme.atlassian.net",
		JiraEmail:   "ci@acme.com",
		JiraToken:   "secret-token",
	}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if settings.JiraEmail != "ci@acme.com" {
		t.Errorf("flags should supply missing config, got %+v", settings)
	}
}

func TestLoadStoresGithubBaseURLVerbatim(t *testing.T) {
	clearConfigEnv(t)
	setValidJiraEnv(t)

	settings, err := config.Load(config.Overrides{}, "https://example.com/org/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if settings.GithubBaseURL != "https://example.com/org/repo" {
		t.Errorf("GitHub base URL should be stored verbatim, got %q", settings.GithubBaseURL)
	}

	// An empty GitHub base URL is allowed: the repo is optional.
	if _, err := config.Load(config.Overrides{}, ""); err != nil {
		t.Errorf("an empty GitHub base URL should be allowed, got %v", err)
	}
}

func TestLoadDotEnvReadsExplicitFile(t *testing.T) {
	clearConfigEnv(t)

	// LoadDotEnv writes into the real environment via os.Setenv, which t.Setenv
	// cannot restore, so unset the variables again when the test finishes.
	t.Cleanup(func() {
		for _, name := range configVarNames {
			_ = os.Unsetenv(name)
		}
	})

	path := filepath.Join(t.TempDir(), "custom.env")
	content := "SHIPNOTES_JIRA_BASE_URL=https://acme.atlassian.net\n" +
		"SHIPNOTES_JIRA_EMAIL=ci@acme.com\n" +
		"SHIPNOTES_JIRA_TOKEN=secret-token\n"

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing temp env file: %v", err)
	}

	if err := config.LoadDotEnv(path); err != nil {
		t.Fatalf("unexpected error loading env file: %v", err)
	}

	settings, err := config.Load(config.Overrides{}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if settings.JiraEmail != "ci@acme.com" {
		t.Errorf("values were not loaded from --env-file: %+v", settings)
	}
}

func TestLoadDotEnvMissingExplicitFileIsError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.env")

	if err := config.LoadDotEnv(missing); err == nil {
		t.Fatal("expected an error when --env-file points at a missing file")
	}
}
