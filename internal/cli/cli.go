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
	"os"
	"os/signal"
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

		console.Failure("💥 " + err.Error())
		console.Dim("Run 'shipnotes --help' for usage.")

		return 1
	}

	// --version short-circuits before any git or Jira work: print and exit.
	if options.showVersion {
		console.Plain("shipnotes " + version)

		return 0
	}

	// Compile the --checked-statuses regexp up front so a bad pattern fails fast,
	// before any git or Jira work, with the same hint as other argument errors.
	checked, err := notes.NewStatusMatcher(options.checkedStatuses)
	if err != nil {
		console.Failure("💥 " + err.Error())
		console.Dim("Run 'shipnotes --help' for usage.")

		return 1
	}

	repoDir, err := resolveRepoDir(options.repoDir)
	if err != nil {
		console.Failure("💥 " + err.Error())

		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	settings, err := loadSettings(ctx, options, repoDir)
	if err != nil {
		printConfigError(console, err)

		return 1
	}

	service := buildService(settings, repoDir, console, checked)

	return generate(ctx, console, service, application.Input{
		CommitHash: options.commitHash,
		OutputPath: options.output,
		JQL:        options.jql,
	})
}

// loadSettings infers the GitHub org/repo/base-URL from the repository's git
// remote so the user only has to configure the Jira variables, then loads and
// validates the environment. Anything set explicitly in the environment still
// overrides what we infer here.
func loadSettings(ctx context.Context, opts options, repoDir string) (config.Settings, error) {
	inferred := git.InferRemoteDefaults(ctx, repoDir)

	return config.Load(opts.envFile, config.Defaults{
		GitRepoOrganization: inferred.Organization,
		GitRepoName:         inferred.RepoName,
		GithubBaseURL:       inferred.GithubBaseURL,
	})
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
	settings config.Settings, repoDir string, console *terminal.Console, checked notes.StatusMatcher,
) *application.Service {
	jiraClient := jira.New(settings.JiraBaseURL, settings.JiraEmail, settings.JiraReadAPIToken, console)
	builder := notes.NewBuilder(jiraClient, console, checked)
	coords := notes.Coordinates{
		GithubBaseURL: settings.GithubBaseURL,
		JiraBaseURL:   settings.JiraBaseURL,
	}

	return application.New(
		git.New(repoDir, console),
		builder,
		markdown.New(),
		fileoutput.New(),
		jiraClient,
		coords,
		repoDir,
	)
}
