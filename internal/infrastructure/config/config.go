// Package config loads and validates the configuration the tool needs. It is an
// infrastructure adapter: it reaches out to the process environment and a .env
// file and produces a validated Settings value the rest of the program consumes.
//
// The flow is: load a .env file if present (without overriding variables that
// are already set), then check that every required value exists and looks valid,
// taking a command-line flag in preference to the environment. If anything is
// wrong we report every problem at once, so the user can fix them in a single
// pass.
//
// The GitHub base URL is the one value config does not resolve here: it is
// optional and needs git-remote parsing plus user-facing warnings, so the cli
// layer resolves it (see EnvGithubRepo) and passes the result to Load.
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
	JiraBaseURL      string
	GithubBaseURL    string
	JiraEmail        string
	JiraReadAPIToken string
}

// Overrides carries command-line flag values that take precedence over the
// environment. A blank field means "no override": the environment value (or, for
// these required variables, a validation error) applies instead.
type Overrides struct {
	JiraBaseURL string
	JiraEmail   string
	JiraToken   string
}

// EnvGithubRepo names the variable that supplies the GitHub repository. Unlike
// the others it is read by the cli's GitHub resolver, not by Load, because
// resolving it needs git-remote parsing and produces warnings.
const EnvGithubRepo = "SHIPNOTES_GITHUB_REPO"

// Environment variable names. Keeping them as constants avoids typos and makes
// the .env.example file easy to keep in sync.
const (
	envJiraBaseURL = "SHIPNOTES_JIRA_BASE_URL"
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

// LoadDotEnv copies a .env file's variables into the process environment so the
// subsequent reads — both here and in the cli's GitHub resolver — see them. Real
// environment variables are never overwritten.
//
// envFilePath, when non-empty, is the explicit --env-file to load instead of
// searching for the nearest .env; a problem reading it is returned as an error.
// Call this once, before Load and before resolving the GitHub repository.
func LoadDotEnv(envFilePath string) error {
	return loadDotEnvFile(envFilePath)
}

// Load reads and validates the Jira configuration, taking a command-line flag
// (overrides) in preference to the environment, and records the already-resolved
// GitHub base URL as-is.
//
// githubBaseURL is resolved by the caller (the cli's GitHub resolver) because it
// is optional and needs git-remote parsing plus warnings; an empty value is
// allowed and simply omits GitHub links from the notes.
//
// Call LoadDotEnv first so the environment reads include any .env values.
func Load(overrides Overrides, githubBaseURL string) (Settings, error) {
	var problems []FieldProblem

	jiraBaseURL := requireURL(overrides.JiraBaseURL, envJiraBaseURL, &problems)
	jiraEmail := requireEmail(overrides.JiraEmail, envJiraEmail, &problems)
	jiraToken := requireNonEmpty(overrides.JiraToken, envJiraToken, "JIRA read API token is required", &problems)

	if len(problems) > 0 {
		return Settings{}, &ValidationError{Problems: problems}
	}

	return Settings{
		JiraBaseURL:      jiraBaseURL,
		GithubBaseURL:    githubBaseURL,
		JiraEmail:        jiraEmail,
		JiraReadAPIToken: jiraToken,
	}, nil
}

// requireNonEmpty returns the trimmed flag override, or the environment value
// when no override was given. It records a problem (against the environment
// variable name) if both are empty.
func requireNonEmpty(override, envName, message string, problems *[]FieldProblem) string {
	value := resolve(override, envName)
	if value == "" {
		*problems = append(*problems, FieldProblem{Field: envName, Message: message})
	}

	return value
}

// requireURL validates that the value is an absolute http(s) URL, taking the
// flag override when set and otherwise the environment value.
func requireURL(override, envName string, problems *[]FieldProblem) string {
	value := resolve(override, envName)
	if value == "" {
		*problems = append(*problems, FieldProblem{Field: envName, Message: "Must be a valid URL"})

		return ""
	}

	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		*problems = append(*problems, FieldProblem{Field: envName, Message: "Must be a valid URL"})

		return value
	}

	return value
}

// requireEmail validates that the value is a single email address, taking the
// flag override when set and otherwise the environment value.
func requireEmail(override, envName string, problems *[]FieldProblem) string {
	value := resolve(override, envName)
	if value == "" {
		*problems = append(*problems, FieldProblem{Field: envName, Message: "Must be a valid email address"})

		return ""
	}

	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value {
		*problems = append(*problems, FieldProblem{Field: envName, Message: "Must be a valid email address"})

		return value
	}

	return value
}

// resolve returns the trimmed flag override, falling back to the trimmed
// environment variable when no override was given.
func resolve(override, envName string) string {
	if value := strings.TrimSpace(override); value != "" {
		return value
	}

	return strings.TrimSpace(os.Getenv(envName))
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
