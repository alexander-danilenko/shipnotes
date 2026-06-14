// Package config loads and validates the environment variables the tool needs.
// It is an infrastructure adapter: it reaches out to the process environment and
// a .env file and produces a validated Settings value the rest of the program
// consumes.
//
// The flow is: load a .env file if present (without overriding variables that
// are already set), then check that every required value exists and looks
// valid. If anything is wrong we report every problem at once, so the user can
// fix them in a single pass.
package config

import (
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"strings"
)

// Settings holds the validated configuration used throughout the program.
type Settings struct {
	GitRepoOrganization string
	GitRepoName         string
	JiraBaseURL         string
	GithubBaseURL       string
	JiraEmail           string
	JiraReadAPIToken    string
}

// Defaults supplies fallback values for the repository-coordinate variables,
// typically inferred from the git remote (see the git adapter's
// InferRemoteDefaults). A blank field means "no default": the corresponding
// environment variable is then required. An environment variable, when set,
// always wins over a default.
type Defaults struct {
	GitRepoOrganization string
	GitRepoName         string
	GithubBaseURL       string
}

// Environment variable names. Keeping them as constants avoids typos and makes
// the .env.example file easy to keep in sync.
const (
	envRepoOrg     = "SHIPNOTES_REPO_ORG"
	envRepoName    = "SHIPNOTES_REPO_NAME"
	envJiraBaseURL = "SHIPNOTES_JIRA_BASE_URL"
	envGithubURL   = "SHIPNOTES_GITHUB_URL"
	envJiraEmail   = "SHIPNOTES_JIRA_EMAIL"
	envJiraToken   = "SHIPNOTES_JIRA_TOKEN" //nolint:gosec // env var name, not a hardcoded credential
)

// ValidationError lists every invalid or missing variable. Printing it shows
// the user all the problems at once instead of one at a time.
type ValidationError struct {
	Problems []FieldProblem
}

// FieldProblem describes one thing wrong with one environment variable.
type FieldProblem struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Problems))
	for _, problem := range e.Problems {
		parts = append(parts, fmt.Sprintf("%s: %s", problem.Field, problem.Message))
	}

	return "environment validation failed: " + strings.Join(parts, "; ")
}

// Load reads the .env file (if found near the current directory), then reads
// and validates the environment. Real environment variables always win over
// values in the .env file.
//
// envFilePath, when non-empty, is the explicit --env-file to load instead of
// searching for the nearest .env; a problem reading it is returned as an error.
//
// defaults provides fallbacks for the repository-coordinate variables (org,
// repo name, GitHub base URL) so they can be omitted when they are inferable
// from the git remote. The Jira variables have no defaults and stay required.
func Load(envFilePath string, defaults Defaults) (Settings, error) {
	if err := loadDotEnvFile(envFilePath); err != nil {
		return Settings{}, err
	}

	var problems []FieldProblem

	organization := requireNonEmpty(
		envRepoOrg, defaults.GitRepoOrganization, "Git organization is required", &problems,
	)
	repoName := requireNonEmpty(
		envRepoName, defaults.GitRepoName, "Git repository name is required", &problems,
	)
	jiraBaseURL := requireURL(envJiraBaseURL, "", &problems)
	githubBaseURL := requireURL(envGithubURL, defaults.GithubBaseURL, &problems)
	jiraEmail := requireEmail(envJiraEmail, &problems)
	jiraToken := requireNonEmpty(envJiraToken, "", "JIRA read API token is required", &problems)

	if len(problems) > 0 {
		return Settings{}, &ValidationError{Problems: problems}
	}

	return Settings{
		GitRepoOrganization: organization,
		GitRepoName:         repoName,
		JiraBaseURL:         jiraBaseURL,
		GithubBaseURL:       githubBaseURL,
		JiraEmail:           jiraEmail,
		JiraReadAPIToken:    jiraToken,
	}, nil
}

// requireNonEmpty returns the trimmed environment value, or fallback when the
// environment variable is unset/blank. It records a problem if both are empty.
func requireNonEmpty(name, fallback, message string, problems *[]FieldProblem) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		value = strings.TrimSpace(fallback)
	}

	if value == "" {
		*problems = append(*problems, FieldProblem{Field: name, Message: message})
	}

	return value
}

// requireURL validates that the value is an absolute http(s) URL, taking the
// environment value when set and otherwise the fallback.
func requireURL(name, fallback string, problems *[]FieldProblem) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		value = strings.TrimSpace(fallback)
	}

	if value == "" {
		*problems = append(*problems, FieldProblem{Field: name, Message: "Must be a valid URL"})

		return ""
	}

	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		*problems = append(*problems, FieldProblem{Field: name, Message: "Must be a valid URL"})

		return value
	}

	return value
}

// requireEmail validates that the value is a single email address.
func requireEmail(name string, problems *[]FieldProblem) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		*problems = append(*problems, FieldProblem{Field: name, Message: "Must be a valid email address"})

		return ""
	}

	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value {
		*problems = append(*problems, FieldProblem{Field: name, Message: "Must be a valid email address"})

		return value
	}

	return value
}

// AsValidationError unwraps err into a *ValidationError when possible, so the
// caller can print the per-field details.
func AsValidationError(err error) (*ValidationError, bool) {
	var validationErr *ValidationError
	if errors.As(err, &validationErr) {
		return validationErr, true
	}

	return nil, false
}
