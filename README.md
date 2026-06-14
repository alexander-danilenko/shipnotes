# shipnotes

Generate a Markdown release-notes file from your git history, annotating each commit with the status of its linked Jira issue.

`shipnotes` reads the commits between a starting point and `HEAD`, pulls the referenced Jira issues, and writes a single Markdown report: a release summary grouped by Jira status, the participating authors, and a full commit table with links back to GitHub and Jira.

- **Single, dependency-free binary** — at runtime it needs only the `git` command and network access to the Jira REST API.
- **Zero workflow assumptions** — issues are grouped by whatever status names they happen to have, so it works with any Jira configuration.
- **Sensible defaults** — the repository and GitHub URL are inferred from your git remote; in the common case you only configure three Jira variables.

## Contents

- [Install](#install)
- [Quick start](#quick-start)
- [Configuration](#configuration)
- [Usage](#usage)
- [Output](#output)
- [Development](#development)

## Install

Requires [Go](https://go.dev/dl/) 1.26 or newer.

```bash
# Install the latest release into $GOBIN (usually ~/go/bin):
go install github.com/alexander-danilenko/shipnotes@latest
```

Or build from a clone:

```bash
git clone https://github.com/alexander-danilenko/shipnotes.git
cd shipnotes
go build -o shipnotes .
./shipnotes --help
```

You can also run it without building, straight from a checkout:

```bash
go run . <commit_hash> [options]
```

## Quick start

1. Create a Jira API token and set the three required variables (in your shell or in a `.env` file — see [Configuration](#configuration)):

   ```bash
   export SHIPNOTES_JIRA_BASE_URL=https://acme.atlassian.net
   export SHIPNOTES_JIRA_EMAIL=you@acme.com
   export SHIPNOTES_JIRA_TOKEN=your-read-scoped-api-token
   ```

2. From inside your git repository, generate notes for the last 20 commits:

   ```bash
   shipnotes HEAD~20
   ```

   This writes `SHIPNOTES.md` in the repository root. The repository org, name, and GitHub URL are inferred from your `origin` remote.

## Configuration

`shipnotes` reads six variables. Provide them as real environment variables or in a `.env` file (see [`.env.example`](.env.example)); real environment variables always take precedence over the file.

| Variable | Meaning | Required? |
|----------|---------|-----------|
| `SHIPNOTES_JIRA_BASE_URL` | Jira base URL, e.g. `https://acme.atlassian.net` | **Yes** |
| `SHIPNOTES_JIRA_EMAIL` | Jira account email (Basic auth) | **Yes** |
| `SHIPNOTES_JIRA_TOKEN` | Jira read-scoped API token | **Yes** |
| `SHIPNOTES_REPO_ORG` | GitHub organization name | Inferred¹ |
| `SHIPNOTES_REPO_NAME` | GitHub repository name | Inferred¹ |
| `SHIPNOTES_GITHUB_URL` | Repo base URL, e.g. `https://github.com/acme/widgets` | Inferred¹ |

¹ When unset, these are inferred from the repository's git remote (`origin`, then `upstream`). A custom SSH host alias (e.g. `git@github-work:org/repo.git` defined in `~/.ssh/config`) is resolved to its real hostname via `ssh -G`. Setting any variable explicitly always overrides the inferred value.

### Where the `.env` file is loaded from

- **`--env-file=/path/to/.env`** — load exactly this file. If it cannot be read, the tool stops with an error.
- **Auto-discovery (when `--env-file` is omitted)** — look for a `.env` in the current directory, then walk *up* through parent directories and use the first one found. A single `.env` in a parent folder works no matter which subdirectory you run from.

## Usage

```text
shipnotes <commit_hash> [options]
```

`<commit_hash>` is the starting point (**exclusive**); the notes cover the range `<commit_hash>..HEAD`. It accepts a full or short hash, `HEAD`, or `HEAD~N`. A tag or branch name is not accepted directly — resolve it to a hash first, e.g. `$(git rev-parse tags/v1.0.0)`.

### Options

| Flag | Default | Description |
|------|---------|-------------|
| `-o`, `--output FILE` | `SHIPNOTES.md` | Output file. A relative path is written inside the repository directory. |
| `--repo-dir DIR` | auto-detected | Git repository to read, searched from the current directory upward. |
| `--env-file FILE` | nearest `.env` | `.env` file to load. |
| `--ids "A-1,A-2"` | *prompt* | Comma-separated Jira issue keys expected in this release (the "Release summary" section). |
| `-h`, `--help` | | Show full help and exit. |

### The `--ids` list

The `--ids` flag drives the **Release summary** section:

- **Explicit list** (`--ids "CX-101,CX-102"`) — each expected issue is compared against the commits. Expected issues found in commits are grouped under their Jira status (sorted alphabetically); expected issues that never appeared are listed under **Missing**; committed issues not on the expected list appear under **Extra**.
- **Omitted** — you are prompted for the list interactively.
- **Omitted and prompt skipped** (press Enter, or run non-interactively) — the summary defaults to *every* Jira ticket referenced in the commit range, grouped by status. **Missing** and **Extra** are then always empty.

The tool never decides which statuses mean "done" — it just shows each issue's status text and lets you read release readiness from the groups.

### Examples

```bash
# Last 20 commits; prompts for the release issue list:
shipnotes HEAD~20

# Everything since a release tag, with an explicit expected list:
shipnotes $(git rev-parse tags/v1.0.0) --ids="CX-101,CX-102" -o SHIPNOTES.md

# Everything since the most recent tag:
shipnotes $(git rev-parse "$(git describe --tags --abbrev=0)")

# Since this branch diverged from main (notes for the current feature branch):
shipnotes $(git merge-base origin/main HEAD)

# With an explicit repository directory and .env file:
shipnotes HEAD~5 --repo-dir /path/to/repo --env-file /path/to/.env
```

## Output

The generated Markdown has four parts: a status-grouped **Release summary** (plus Missing/Extra and any reverted/reapplied commits), a **Participants** list, and a full **Commit history** table. For example:

```markdown
# Release Notes

Generated: 2024-01-15T10:30:00.000000

Repository: https://github.com/acme/widgets

## Release summary

### Done

- [ ] [CX-101](https://acme.atlassian.net/browse/CX-101) Add login page

### In Progress

- [ ] [CX-200](https://acme.atlassian.net/browse/CX-200) Refactor auth

## Missing

- [ ] [CX-300](https://acme.atlassian.net/browse/CX-300) Document API

---

# Participants

- `Alex Smith`
- `Jane Doe`

---

# Commit history

| Hash | Jira Key | Jira Status | Commit Message | Authors |
|------|----------|-------------|----------------|---------|
| [`abc1234`](…/commit/abc1234) | [CX-101](…/browse/CX-101) | Done | CX-101: Add login page ([#42](…/pull/42)) | `Jane Doe` |
| [`def5678`](…/commit/def5678) | N/A | No Issue | chore: tidy up | `Alex Smith`, `Jane Doe` |
```

## Development

```bash
go test ./...              # run tests (includes golden-file output tests)
go vet ./...               # built-in static analysis
golangci-lint run ./...    # strict linting (expects 0 issues)
```

See [`CLAUDE.md`](CLAUDE.md) for the architecture, project rules, and a Go primer aimed at readers new to the language.
