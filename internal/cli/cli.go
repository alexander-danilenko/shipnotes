// Package cli is the interface layer: the command-line driving adapter and the
// composition root. It parses arguments, builds the concrete infrastructure
// adapters, wires them into the application service, runs the use case, and
// presents the result. It is the only place that imports every other layer.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/alexander-danilenko/shipnotes/internal/application"
	"github.com/alexander-danilenko/shipnotes/internal/domain/notes"
	"github.com/alexander-danilenko/shipnotes/internal/infrastructure/config"
	"github.com/alexander-danilenko/shipnotes/internal/infrastructure/fileoutput"
	"github.com/alexander-danilenko/shipnotes/internal/infrastructure/git"
	"github.com/alexander-danilenko/shipnotes/internal/infrastructure/jira"
	"github.com/alexander-danilenko/shipnotes/internal/infrastructure/markdown"
	"github.com/alexander-danilenko/shipnotes/internal/infrastructure/terminal"
)

// Run is the program's real entry point: it returns a process exit code. The
// version string is stamped into the binary at build time (see main.version)
// and reported by --version. Keeping Run separate from main makes the program
// straightforward to test.
func Run(args []string, version string) int {
	console := terminal.New(os.Stdout)

	options, err := parseArgs(args)
	if err != nil {
		// -h/--help is not a failure: the help text has already been printed.
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}

		return usageError(console, err)
	}

	// --version short-circuits before any git or Jira work: print and exit.
	if options.showVersion {
		console.Plain("shipnotes " + version)

		return 0
	}

	patterns, err := compileMatchers(options)
	if err != nil {
		return usageError(console, err)
	}

	repoDir, err := resolveRepoDir(options.repoDir)
	if err != nil {
		console.Failure("💥 " + err.Error())

		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	settings, err := loadSettings(ctx, console, options, repoDir)
	if err != nil {
		printConfigError(console, err)

		return 1
	}

	shippedRef, err := resolveShippedRef(ctx, console, options, repoDir)
	if err != nil {
		return usageError(console, err)
	}

	service := buildService(settings, repoDir, console, patterns, shippedRef)

	return generate(ctx, console, service, application.Input{
		CommitHash: options.commitHash,
		OutputPath: options.output,
		JQL:        options.jql,
	})
}

// usageError reports a bad argument the same way everywhere: the message, then
// the pointer to --help, then the failing exit code the caller returns.
func usageError(console *terminal.Console, err error) int {
	console.Failure("💥 " + err.Error())
	console.Dim("Run 'shipnotes --help' for usage.")

	return 1
}

// matchers are the two opt-in patterns the builder needs, compiled together.
type matchers struct {
	checked  notes.StatusMatcher
	excluded notes.CommitMatcher
}

// compileMatchers compiles --checked-statuses and --exclude-commits up front, so
// a bad pattern fails fast — before any git or Jira work — rather than partway
// through a run. The zero value of either matches nothing, which is what an
// empty pattern means.
func compileMatchers(opts options) (matchers, error) {
	checked, err := notes.NewStatusMatcher(opts.checkedStatuses)
	if err != nil {
		return matchers{}, err
	}

	excluded, err := notes.NewCommitMatcher(opts.excludeCommits)
	if err != nil {
		return matchers{}, err
	}

	return matchers{checked: checked, excluded: excluded}, nil
}

// loadSettings loads any .env file, resolves the (optional) GitHub repository,
// then loads and validates the Jira configuration. Precedence for every value is
// flag, then environment; the GitHub repository additionally falls back to the
// git remote. A flag always wins over the environment.
func loadSettings(
	ctx context.Context, console *terminal.Console, opts options, repoDir string,
) (config.Settings, error) {
	if err := config.LoadDotEnv(opts.envFile); err != nil {
		return config.Settings{}, err
	}

	githubBaseURL, err := resolveGithubBaseURL(ctx, console, opts, repoDir)
	if err != nil {
		return config.Settings{}, err
	}

	return config.Load(config.Overrides{
		JiraBaseURL: opts.jiraBaseURL,
		JiraEmail:   opts.jiraEmail,
		JiraToken:   opts.jiraToken,
	}, githubBaseURL)
}

// resolveGithubBaseURL determines the GitHub web base URL used to build commit
// and pull-request links, in precedence order: the --github-repo flag, the
// SHIPNOTES_GITHUB_REPO environment variable, then the repository's git remote.
//
// An explicit flag/env value that cannot be parsed is a fatal error — the user
// named a specific repository and we should not silently ignore a typo. When
// nothing resolves, the GitHub repository is simply absent: the function warns
// and returns an empty string, and the notes are generated without GitHub links.
func resolveGithubBaseURL(
	ctx context.Context, console *terminal.Console, opts options, repoDir string,
) (string, error) {
	if spec := firstNonEmpty(opts.githubRepo, os.Getenv(config.EnvGithubRepo)); spec != "" {
		baseURL, ok := git.ParseGithubSpec(spec)
		if !ok {
			return "", fmt.Errorf(
				"could not parse GitHub repository %q: expected a URL, an SSH remote, or \"org/repo\"", spec)
		}

		warnIfNotGitHub(console, baseURL)

		return baseURL, nil
	}

	baseURL := git.InferRemoteBaseURL(ctx, repoDir)
	if baseURL == "" {
		console.Warn("⚠️  No GitHub repository found via --github-repo, SHIPNOTES_GITHUB_REPO, " +
			"or the git remote; commit and pull-request links will be omitted.")

		return "", nil
	}

	warnIfNotGitHub(console, baseURL)

	return baseURL, nil
}

// resolveShippedRef determines which ref holds already-released work: the
// --shipped-ref flag, else the repository's default branch.
//
// An explicit value that is not a usable ref name is fatal, for the same reason
// an unparseable --github-repo is: the user named something specific and a typo
// should not be silently ignored. When nothing resolves the feature is simply
// unavailable — the function warns and returns an empty string, and the notes
// are generated without already-shipped detection.
func resolveShippedRef(
	ctx context.Context, console *terminal.Console, opts options, repoDir string,
) (string, error) {
	if ref := strings.TrimSpace(opts.shippedRef); ref != "" {
		if !git.IsValidRefName(ref) {
			return "", fmt.Errorf(
				"could not use --shipped-ref %q: expected a branch or tag name such as \"origin/main\"", ref)
		}

		return ref, nil
	}

	ref := git.InferDefaultBranchRef(ctx, repoDir)
	if ref == "" {
		console.Warn("⚠️  No release branch found via --shipped-ref or the git remote; " +
			"already-shipped detection is off.")
	}

	return ref, nil
}

// warnIfNotGitHub warns when the resolved repository is not hosted on github.com.
// The tool builds GitHub-style "/commit/" and "/pull/" link paths, so another
// host (GitLab, Bitbucket, a self-hosted GitHub Enterprise) may produce links
// that do not resolve. The notes are still generated either way.
func warnIfNotGitHub(console *terminal.Console, baseURL string) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Hostname() == "github.com" {
		return
	}

	console.Warn(fmt.Sprintf(
		"⚠️  %s is not on github.com; commit and pull-request links use GitHub's URL format "+
			"and may not resolve for this host.", baseURL))
}

// firstNonEmpty returns the first value that is not blank after trimming, or ""
// when every value is blank.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}

// generate runs the use case and presents the outcome, returning the process
// exit code.
func generate(
	ctx context.Context, console *terminal.Console, service *application.Service, input application.Input,
) int {
	console.Header()

	result, err := service.Run(ctx, input)
	if err != nil {
		console.Failure("\n💥 Failed to generate release notes\n")
		console.Plain(err.Error())

		return 1
	}

	console.Success(fmt.Sprintf("\n✨ Generated release notes for %d commits", result.CommitCount))
	console.Dim("📄 Written to: " + result.OutputPath)

	return 0
}

// buildService wires the concrete infrastructure adapters into the application
// service. This is the composition root: the one spot that knows which concrete
// implementation backs each port.
func buildService(
	settings config.Settings, repoDir string, console *terminal.Console,
	patterns matchers, shippedRef string,
) *application.Service {
	jiraClient := jira.New(settings.JiraBaseURL, settings.JiraEmail, settings.JiraReadAPIToken, console)
	builder := notes.NewBuilder(jiraClient, console, patterns.checked, patterns.excluded)
	coords := notes.Coordinates{
		GithubBaseURL: settings.GithubBaseURL,
		JiraBaseURL:   settings.JiraBaseURL,
	}

	return application.New(
		git.New(repoDir, console, shippedRef),
		builder,
		markdown.New(),
		fileoutput.New(),
		jiraClient,
		coords,
		repoDir,
	)
}
