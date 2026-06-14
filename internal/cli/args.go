package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/alexander-danilenko/shipnotes/internal/domain/issue"
)

// options holds the parsed command-line arguments.
type options struct {
	commitHash string
	output     string
	repoDir    string
	// envFile is the explicit --env-file path, or "" to auto-discover a .env.
	envFile string
	// ids is nil when --ids was not supplied (the user is then prompted),
	// and a (possibly empty) string when it was.
	ids *string
	// showVersion is set by -v/--version; it makes the program print its
	// version and exit, without requiring the commit_hash argument.
	showVersion bool
}

// registerFlags declares every command-line flag on fs, binding it to the
// destination in opts (or the ids string, which needs separate "was it set?"
// detection). It also installs the usage message.
func registerFlags(fs *flag.FlagSet, opts *options, ids *string) {
	fs.StringVar(&opts.output, "o", "SHIPNOTES.md", "Output file path")
	fs.StringVar(&opts.output, "output", "SHIPNOTES.md", "Output file path")
	fs.StringVar(&opts.repoDir, "repo-dir", "", "Git repository directory (defaults to current directory)")
	fs.StringVar(&opts.envFile, "env-file", "", "Path to the .env file to load (defaults to the nearest .env)")
	fs.StringVar(ids, "ids", "", "Comma-separated list of Jira Issue IDs (e.g. 'ABC-123,ABC-124')")
	fs.BoolVar(&opts.showVersion, "v", false, "Show the version and exit")
	fs.BoolVar(&opts.showVersion, "version", false, "Show the version and exit")

	// Silence flag's own output. By default it writes the error message AND the
	// full usage to stderr on any parse error; we report the error (with a help
	// pointer) ourselves and print the full help only for an explicit -h/--help.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
}

// usageText is the full --help output: a description of what the tool does,
// every flag, the configuration it needs, and worked examples.
const usageText = `shipnotes — generate Markdown release notes from git history.

For each commit from <commit_hash> (exclusive) up to HEAD, it finds the Jira
issue key in the commit message, looks the issue up in Jira, and writes a
Markdown file listing every commit annotated with its issue's status. Pull
request references in the messages are linked back to GitHub.

Usage:
  shipnotes <commit_hash> [options]

Argument:
  <commit_hash>   Where the history starts (exclusive); the notes cover the
                  range <commit_hash>..HEAD. Accepts a full or short hash,
                  HEAD, or HEAD~N. A tag or branch name is not accepted
                  directly — resolve it to a hash first, e.g.
                  $(git rev-parse tags/v1.0.0).

Options:
  -o, --output FILE   Output file (default: SHIPNOTES.md). A relative path
                      is written inside the repository directory.
  --repo-dir DIR      Git repository to read (default: auto-detected from the
                      current directory upward).
  --env-file FILE     .env file to load. Defaults to the nearest .env, found by
                      searching the current directory and its parents.
  --ids "A-1,A-2"     Comma-separated Jira issue keys expected in this release;
                      they populate the "Release summary" section. If omitted,
                      you are prompted for them interactively; skip the prompt to
                      summarize every issue found in the commit range instead.
  -v, --version       Show the version and exit.
  -h, --help          Show this help and exit.

Configuration:
  Provide these as environment variables or in a .env file; real environment
  variables take precedence over the file.

  Required:
    SHIPNOTES_JIRA_BASE_URL  e.g. https://acme.atlassian.net
    SHIPNOTES_JIRA_EMAIL     Jira account email (used for Basic auth)
    SHIPNOTES_JIRA_TOKEN     Jira read-scoped API token

  Inferred from the repository's git remote when unset (set to override):
    SHIPNOTES_REPO_ORG       GitHub organization
    SHIPNOTES_REPO_NAME      GitHub repository name
    SHIPNOTES_GITHUB_URL     e.g. https://github.com/acme/widgets

Examples:
  # Last 20 commits; prompts for the release issue list:
  shipnotes HEAD~20

  # Everything since a release tag (resolve the tag to a commit first):
  shipnotes $(git rev-parse tags/v1.0.0) --ids="CX-101,CX-102" -o SHIPNOTES.md

  # Everything since the most recent tag:
  shipnotes $(git rev-parse "$(git describe --tags --abbrev=0)")

  # Since this branch diverged from main (notes for the current feature branch):
  shipnotes $(git merge-base origin/main HEAD)

  # With an explicit repository directory and .env file:
  shipnotes HEAD~5 --repo-dir /path/to/repo --env-file /path/to/.env
`

// parseArgs reads the flags and the required positional commit hash. Flags may
// appear before or after the commit hash.
func parseArgs(args []string) (options, error) {
	fs := flag.NewFlagSet("shipnotes", flag.ContinueOnError)

	var (
		opts   options
		ids    string
		idsSet bool
	)

	registerFlags(fs, &opts, &ids)

	// Everything after a literal "--" is a positional argument, never a flag.
	flagArgs, afterTerminator := splitAtTerminator(args)

	// Parse flags and the positional argument in any order. flag.Parse stops at
	// the first non-flag token, so we collect it and keep parsing the rest.
	var positionals []string

	rest := flagArgs
	for len(rest) > 0 {
		if err := fs.Parse(rest); err != nil {
			// -h/--help is reported as flag.ErrHelp; show the full help for it.
			if errors.Is(err, flag.ErrHelp) {
				fmt.Fprint(os.Stdout, usageText)
			}

			return options{}, err
		}

		rest = fs.Args()
		if len(rest) == 0 {
			break
		}

		positionals = append(positionals, rest[0])
		rest = rest[1:]
	}

	positionals = append(positionals, afterTerminator...)

	// Detect whether --ids was actually provided (vs left at its default).
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "ids" {
			idsSet = true
		}
	})

	if idsSet {
		opts.ids = &ids
	}

	// --version is handled before the positional requirement so that
	// `shipnotes --version` works on its own, without a commit hash.
	if opts.showVersion {
		return opts, nil
	}

	switch len(positionals) {
	case 0:
		return options{}, errors.New("missing required argument: commit_hash")
	case 1:
		opts.commitHash = positionals[0]

		return opts, nil
	default:
		return options{}, fmt.Errorf("unexpected extra arguments: %v", positionals[1:])
	}
}

// splitAtTerminator divides args at the first literal "--" token. Everything
// before it is subject to flag parsing; everything after it is returned as
// verbatim positional arguments. When there is no "--", after is nil.
func splitAtTerminator(args []string) (before, after []string) {
	for i, arg := range args {
		if arg == "--" {
			return args[:i], args[i+1:]
		}
	}

	return args, nil
}

// resolveReleaseIssueIDs converts the --ids flag into a slice. It returns nil to
// signal "ask the user interactively" when the flag was absent OR given as an
// empty string: an empty --ids is treated the same as no --ids at all. A
// non-empty value (even just whitespace) is parsed.
func resolveReleaseIssueIDs(ids *string) ([]string, error) {
	if ids == nil || *ids == "" {
		return nil, nil
	}

	return issue.ParseIDs(*ids)
}
