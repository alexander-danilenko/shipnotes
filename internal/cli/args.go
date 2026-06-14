package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

// options holds the parsed command-line arguments.
type options struct {
	commitHash string
	output     string
	repoDir    string
	// envFile is the explicit --env-file path, or "" to auto-discover a .env.
	envFile string
	// jql is the optional JQL query selecting the release issue list, or "" when
	// --jql was not supplied (the builder then summarizes every issue in range).
	jql string
	// checkedStatuses is the regular expression whose matching issue statuses are
	// rendered as completed ("[x]") checklist items. Empty disables checking.
	checkedStatuses string
	// excludeCommits is the regular expression matched against each commit's
	// subject (which carries its Jira key); matching commits are dropped from the
	// notes into the "Excluded commits" section. Empty (the default) excludes
	// nothing.
	excludeCommits string
	// showVersion is set by -v/--version; it makes the program print its
	// version and exit, without requiring the commit_hash argument.
	showVersion bool
}

// defaultCheckedStatuses is the regular expression used for --checked-statuses
// when the flag is not given: issues in a "done"-like status are pre-checked.
const defaultCheckedStatuses = "done|ready to release|ready for release"

// registerFlags declares every command-line flag on fs, binding it to the
// destination in opts. It also installs the usage message.
func registerFlags(fs *flag.FlagSet, opts *options) {
	fs.StringVar(&opts.output, "o", "SHIPNOTES.md", "Output file path")
	fs.StringVar(&opts.output, "output", "SHIPNOTES.md", "Output file path")
	fs.StringVar(&opts.repoDir, "repo-dir", "", "Git repository directory (defaults to current directory)")
	fs.StringVar(&opts.envFile, "env-file", "", "Path to the .env file to load (defaults to the nearest .env)")
	fs.StringVar(&opts.jql, "jql", "", "JQL query selecting the release issues (e.g. 'key IN (PROJ-123, PROJ-456)')")
	fs.StringVar(&opts.checkedStatuses, "checked-statuses", defaultCheckedStatuses,
		"Case-insensitive regexp; issues whose status fully matches render as checked ([x]). Empty disables.")
	fs.StringVar(&opts.excludeCommits, "exclude-commits", "",
		"Case-insensitive regexp; commits whose subject matches are excluded from the notes. Empty keeps all.")
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
  --jql "QUERY"       JQL query whose matching issues populate the "Release
                      summary" section, e.g. 'key IN (PROJ-123, PROJ-456)'. If
                      omitted, every issue found in the commit range is
                      summarized instead.
  --checked-statuses REGEXP
                      Case-insensitive regular expression matched against each
                      issue's full status text; matching issues are rendered as
                      completed checklist items ("[x]") in the summary. Default:
                      'done|ready to release|ready for release'. Pass an empty
                      string ("") to disable and leave every box unchecked.
  --exclude-commits REGEXP
                      Case-insensitive regular expression matched (unanchored)
                      against each commit's subject; matching commits are dropped
                      from the commit history and the summary and listed under
                      "Excluded commits" instead. Useful for hiding noise like
                      docs/chore commits, e.g. '^(chore|docs|test|ci|build)(\(|:)'.
                      The subject carries the Jira key, so you can also exclude by
                      ticket; anchor it ('\bCX-42\b') to avoid also matching
                      CX-420. Empty (the default) keeps every commit.
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
  # Last 20 commits; summarizes every issue found in the range:
  shipnotes HEAD~20

  # Everything since a release tag (resolve the tag to a commit first), with an
  # explicit release issue list selected by JQL:
  shipnotes $(git rev-parse tags/v1.0.0) --jql="key IN (CX-101, CX-102)" -o SHIPNOTES.md

  # Select the release issues by fix version instead of listing keys:
  shipnotes $(git rev-parse tags/v1.0.0) --jql="project = CX AND fixVersion = 1.0.0"

  # Pre-check issues that are closed or verified (custom "done" statuses):
  shipnotes HEAD~20 --checked-statuses="closed|verified"

  # Drop docs/chore/test commits from the notes (they move to "Excluded commits"):
  shipnotes HEAD~20 --exclude-commits='^(chore|docs|test|ci|build)(\(|:)'

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

	var opts options

	registerFlags(fs, &opts)

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
