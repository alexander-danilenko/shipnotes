<p align="center">
  <img src="docs/assets/logo.svg" alt="shipnotes" width="120">
</p>

<h1 align="center">shipnotes</h1>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="License: MIT"></a>
</p>

> *Ship notes, not sh\*t notes* — one well-formed Markdown file beats a wall of raw `git log`.

`shipnotes` turns your git history into a Markdown release-notes file. It reads the commits in a range, looks up the Jira issue each commit references, and writes a single report. The report has three sections: a release summary grouped by Jira status, a list of participating authors, and a commit table linked to GitHub and Jira.

- **Single, dependency-free binary.** At runtime it needs only the `git` command and network access to the Jira REST API.
- **No workflow assumptions.** Issues are grouped by whatever status names they have, so it works with any Jira configuration.
- **Sensible defaults.** `shipnotes` infers the repository and GitHub URL from your git remote. In the common case, you set only three Jira variables.

## Contents

- [Install](#install)
- [Quick start](#quick-start)
- [Usage](#usage)
- [Advanced usage](#advanced-usage)
- [Development](#development)

## Install

Download a prebuilt binary from the [latest release](https://github.com/alexander-danilenko/shipnotes/releases/latest). Binaries are available for Linux, macOS, and Windows (amd64 and arm64). Extract it and put `shipnotes` on your `PATH`. Each release also publishes a `checksums.txt` to verify the download. To check your version, run `shipnotes --version`.

Or, with [Go](https://go.dev/dl/) 1.26 or newer:

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

From inside your git repository, generate notes for the last 20 commits. Pass `--jql` to declare the issues you expect in this release, along with the three required Jira values:

```bash
shipnotes HEAD~20 \
  --jql "key IN (PROJ-101, PROJ-200, PROJ-300)" \
  --jira-base-url https://acme.atlassian.net \
  --jira-email you@acme.com \
  --jira-token your-read-scoped-api-token
```

That writes `SHIPNOTES.md` to the repository root, inferring the GitHub repository from your `origin` remote. By comparing the issues `--jql` expects against the ones the commits actually reference, `shipnotes` flags any **Missing** (expected, but never committed) and **Extra** (committed, but outside the expected list) — so you can spot work that slipped or sneaked in. To avoid passing the Jira values on every run, set them as environment variables or in a `.env` file — see [Advanced usage](#advanced-usage).

Here's what the generated file looks like:

```markdown
# Release Notes

- **Generated:** 2024-01-15T10:30
- **Repository:** https://github.com/acme/widgets

## Release summary

### Done

- [ ] [PROJ-101](https://acme.atlassian.net/browse/PROJ-101) Add login page

### In Progress

- [ ] [PROJ-200](https://acme.atlassian.net/browse/PROJ-200) Refactor auth

## Missing

- [ ] [PROJ-300](https://acme.atlassian.net/browse/PROJ-300) Document API

## Extra

- [ ] [PROJ-150](https://acme.atlassian.net/browse/PROJ-150) Fix password reset

## Participants

- `Alex Smith`
- `Jane Doe`

## Commit history

| Hash | Jira Key | Jira Status | Commit Message | Authors |
|------|----------|-------------|----------------|---------|
| [`abc1234`](…/commit/abc1234) | [PROJ-101](…/browse/PROJ-101) | Done | PROJ-101: Add login page ([#42](…/pull/42)) | `Jane Doe` |
| [`9f3c2a1`](…/commit/9f3c2a1) | [PROJ-150](…/browse/PROJ-150) | Done | PROJ-150: Fix password reset | `Alex Smith` |
| [`def5678`](…/commit/def5678) | N/A | No Issue | chore: tidy up | `Alex Smith`, `Jane Doe` |
```

## Usage

```text
shipnotes <commit_hash> [options]
```

`<commit_hash>` is the starting point, and it's **exclusive**: the notes cover the range `<commit_hash>..HEAD`. It accepts a full or short hash, `HEAD`, or `HEAD~N`. It doesn't accept a tag or branch name directly. Resolve one to a hash first, for example `$(git rev-parse tags/v1.0.0)`.

### Options

- **`-o`, `--output FILE`** — Output file (default: `SHIPNOTES.md`). A relative path is written inside the repository directory.
- **`--repo-dir DIR`** — Git repository to read, searched from the current directory upward (default: auto-detected).
- **`--env-file FILE`** — `.env` file to load (default: nearest `.env`).
- **`--jql "QUERY"`** — JQL query whose matching issues become the expected release list (the "Release summary" section). When omitted, every issue found in the range is summarized.
- **`--checked-statuses REGEXP`** — Case-insensitive regexp matched against each issue's full status; matching issues render as completed (`[x]`) in the summary. Pass `""` to disable (default: `done|ready to release|ready for release`).
- **`--exclude-commits REGEXP`** — Case-insensitive (unanchored) regexp matched against each commit's subject; matching commits are dropped from the notes into an "Excluded commits" section. Empty keeps every commit (default: empty).
- **`--jira-base-url URL`** — Jira base URL. Overrides `SHIPNOTES_JIRA_BASE_URL` (default: from env).
- **`--jira-email EMAIL`** — Jira account email. Overrides `SHIPNOTES_JIRA_EMAIL` (default: from env).
- **`--jira-token TOKEN`** — Jira read-scoped API token. Overrides `SHIPNOTES_JIRA_TOKEN` (default: from env).
- **`--github-repo REPO`** — GitHub repo as a URL, SSH remote, or `org/repo`. Overrides `SHIPNOTES_GITHUB_REPO`; inferred from the git remote when unset.
- **`-v`, `--version`** — Show the version and exit.
- **`-h`, `--help`** — Show full help and exit.

### Examples

```bash
# Last 20 commits; summarizes every issue found in the range:
shipnotes HEAD~20

# Everything since a release tag, with an explicit expected list via JQL:
shipnotes $(git rev-parse tags/v1.0.0) --jql="key IN (PROJ-101, PROJ-102)" -o SHIPNOTES.md

# Select the expected issues by fix version instead of listing keys:
shipnotes $(git rev-parse tags/v1.0.0) --jql="project = PROJ AND fixVersion = 1.0.0"

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
```

## Advanced usage

### Configuration values

`shipnotes` needs four values. Provide each as a flag or an environment variable (set directly or in a `.env` file — see [`.env.example`](.env.example)). A flag wins over the environment, and a real environment variable wins over the `.env` file.

| Required? | Flag | Environment variable | Meaning |
|-----------|------|----------------------|---------|
| **Yes** | `--jira-base-url` | `SHIPNOTES_JIRA_BASE_URL` | Jira base URL, e.g. `https://acme.atlassian.net` |
| **Yes** | `--jira-email` | `SHIPNOTES_JIRA_EMAIL` | Jira account email (Basic auth) |
| **Yes** | `--jira-token` | `SHIPNOTES_JIRA_TOKEN` | Jira read-scoped API token |
| Inferred¹ | `--github-repo` | `SHIPNOTES_GITHUB_REPO` | GitHub repository: a URL, an SSH remote, or the `org/repo` shorthand |

¹ When unset, `shipnotes` infers the GitHub repository from the git remote (`origin`, then `upstream`). It resolves a custom SSH host alias (such as `git@github-work:org/repo.git` from `~/.ssh/config`) to its real hostname with `ssh -G`. A flag or environment variable you set explicitly overrides the inferred value. The GitHub repository is optional: if none can be determined, `shipnotes` warns and still writes the notes, omitting the commit and pull-request links. It also warns when the repository is not on `github.com`, since the links use GitHub's URL format.

### Where the `.env` file is loaded from

- **`--env-file=/path/to/.env`** — Loads exactly this file. If the file can't be read, `shipnotes` stops with an error.
- **Auto-discovery (when `--env-file` is omitted)** — `shipnotes` looks for a `.env` in the current directory, then walks *up* through parent directories and uses the first one it finds. A single `.env` in a parent folder works from any subdirectory.

### The `--jql` query

The `--jql` flag drives the **Release summary** section. It selects the issues expected in this release.

- **Explicit query** — for example `--jql "key IN (PROJ-101, PROJ-102)"` or `--jql "project = PROJ AND fixVersion = 1.0.0"`. `shipnotes` compares the matching issues against the commits:
  - Expected issues found in commits are grouped under their Jira status, sorted alphabetically.
  - Expected issues that never appeared are listed under **Missing**.
  - Committed issues the query didn't match appear under **Extra**.
- **Omitted** (or a query that matches nothing) — the summary defaults to *every* Jira ticket referenced in the commit range, grouped by status. **Missing** and **Extra** are then always empty.

Grouping never decides which statuses mean "done." It shows each issue's status text and lets you read release readiness from the groups.

### Checked statuses

`--checked-statuses` is one of two opt-in opinions the tool takes. Its value is a **case-insensitive regular expression** matched against each issue's *full* status text. Issues whose status matches render as a completed checkbox (`[x]`) in the summary instead of an empty one (`[ ]`).

The match is anchored to the whole status, so `done` matches `Done` but not `Almost Done`. Use alternation for several statuses; the default is `done|ready to release|ready for release`. To check nothing and keep the output status-neutral, pass an empty string: `--checked-statuses=""`. The grouping and the commit-history table are unaffected — only the checkbox state changes.

### Excluded commits

`--exclude-commits` is the other opt-in opinion, off by default. Its value is a **case-insensitive regular expression** matched against each commit's subject line. The match is **unanchored**, so it catches a prefix or substring.

One pattern can drop commits by type, such as `^(chore|docs|test|ci|build)(\(|:)`. The subject also carries the Jira key, so you can exclude by ticket. Because the match is unanchored, use word boundaries for one exact ticket: `\bPROJ-42\b` matches `PROJ-42` but not `PROJ-420`.

Matching commits leave the commit-history table and the Release summary. They aren't deleted — they move to an "Excluded commits" section, so the notes stay auditable. This section appears even when the range has no Jira issues at all. Exclusion is the first gate: a matched commit is reported only as excluded, even if it's also a revert or reapply. Pass an empty string (the default) to keep every commit.

## Development

```bash
go test ./...              # run tests (includes golden-file output tests)
go vet ./...               # built-in static analysis
golangci-lint run ./...    # strict linting (expects 0 issues)
```

For the architecture, project rules, and a Go primer aimed at readers new to the language, see [`CLAUDE.md`](CLAUDE.md).
