# shipnotes — Project Rules & Guide

This file tells Claude Code (and any human) how to work in this project.

## What this is

`shipnotes` generates a Markdown release notes file from git history, annotating each commit with the status of its linked Jira issue.

It is a single, dependency-free binary that runs anywhere Go is installed, with no virtual environment or package install step — the only things it needs at runtime are the `git` command and network access to the Jira REST API.

> **Workflow-agnostic by design.** The tool makes no assumptions about your Jira workflow. Grouping has no notion of which statuses mean "done" — issues are grouped by whatever status names they happen to have (sorted alphabetically), and each commit shows its issue's status text as-is. There are two opt-in opinions, each disabled by an empty pattern. `--checked-statuses` is a case-insensitive regexp whose matching statuses render as completed (`[x]`) checkboxes in the summary; it defaults to `done|ready to release|ready for release`. `--exclude-commits` is a case-insensitive (unanchored) regexp, matched against each commit's subject (which carries its Jira key), that drops matching commits from the notes into an "Excluded commits" section — relocated, not deleted, so the notes stay auditable; it is empty by default. The same algorithm works on any repo and any Jira setup, with zero configuration.

## Keep ARCHITECTURE.md in sync (mandatory)

[`ARCHITECTURE.md`](./ARCHITECTURE.md) (arc42-light) is the contract for this codebase's structure. **When a change crosses an architectural boundary, update `ARCHITECTURE.md` in the same change** — it's part of the task, not a follow-up. Boundary-crossing means: adding/removing/renaming/moving a package; changing a **port** (interface in `internal/domain/*` or `internal/application/app.go`) or its adapter; changing the flow in `app.go`/`cli.go`; changing runtime/build/config (env vars, `.env`, remote inference); changing `go.mod` deps; changing the output contract (template or golden files); or recording a design decision.

`ARCHITECTURE.md` §12 maps each change to the exact section to edit. Keep edits surgical. Cosmetic edits (local renames, typos, intra-package refactors) need none.

## Guiding principles (read before changing anything)

1. **Optimize for the reader, not the writer.** This code is meant to be understood by people who have *never written Go*. Prefer a few extra lines of obvious code over one clever line. Every exported type and function has a doc comment that says what it does in plain English.
2. **Use the standard library as hard as possible.** This project has **zero external dependencies** (see `go.mod` — there is no `require` block). Every capability is built on a Go built-in:

   | Need | Go standard library |
   |------|---------------------|
   | templating | `text/template` |
   | HTTP requests | `net/http` |
   | URL / email validation | `net/url`, `net/mail` |
   | `.env` parsing | a tiny hand-written parser (`internal/infrastructure/config/dotenv.go`) |
   | colored output | a small ANSI helper (`internal/infrastructure/terminal`) |
   | CLI flags | `flag` |

   Adding a dependency requires a real justification. Beyond the Go toolchain the project uses only dev-only tooling that never ships in the binary: `golangci-lint` (Go linter) and `markdownlint-cli2` (Markdown linter, run via `npx`, needs Node).
3. **The output is a stable contract.** The exact Markdown the tool produces is locked by golden-file tests (below). If you change rendering, regenerate the golden files and explain why in the commit — a rendering change should always be a deliberate, reviewed diff, never an accident.
4. **Always reach for idiomatic Go.** In every aspect of Go development and the Go ecosystem — package and file layout, naming, error handling, interfaces, concurrency, generics, table-driven tests, build tooling, and module management — propose and write the approach an experienced Go author would recognize as idiomatic. Follow [Effective Go](https://go.dev/doc/effective_go), the [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments), and the conventions of the standard library; let `gofmt`/`gofumpt`, `go vet`, and `golangci-lint` be the arbiters. When more than one idiomatic option exists, recommend the one that best serves principle 1 (the non-Go reader) and explain the trade-off rather than silently picking the cleverest form. Reject non-idiomatic patterns even when they would technically work.

## How to run, build, and test

All commands run from this directory (`shipnotes/`).

```bash
go run . <commit_hash> [options]   # Run without building a binary
go build -o shipnotes .        # Build a standalone binary
go test ./...                      # Run all tests
go vet ./...                       # Built-in static checks
golangci-lint run ./...            # Full strict lint (must report 0 issues)
gofmt -l .                         # List unformatted files (should be empty)
```

Before considering any task complete, **all of these must pass**: `go build`, `go vet`, `go test ./...`, and `golangci-lint run ./...` with zero issues.

### Using the tool

```bash
shipnotes <commit_hash> \
  -o SHIPNOTES.md \                  # output file (default: SHIPNOTES.md)
  --repo-dir /path/to/repo \         # git repo to read (default: auto-detected)
  --env-file /path/to/.env \         # .env file to load (default: nearest .env)
  --jql "key IN (CX-101, CX-102)" \  # optional JQL selecting the release issues for the summary
  --checked-statuses "done|qa" \     # regexp of statuses to render as [x] (default: done|ready to release|ready for release)
  --exclude-commits '^(chore|docs):' \ # regexp; matching commits move to "Excluded commits" (default: empty, keep all)
  --jira-base-url https://acme.atlassian.net \ # overrides SHIPNOTES_JIRA_BASE_URL
  --jira-email me@acme.com \         # overrides SHIPNOTES_JIRA_EMAIL
  --jira-token "$JIRA_TOKEN" \       # overrides SHIPNOTES_JIRA_TOKEN
  --github-repo acme/widgets         # URL, SSH remote, or org/repo; overrides SHIPNOTES_GITHUB_REPO
```

It needs four configuration values, each settable as a flag or an environment variable (see `.env.example`). A flag wins over the environment; with `--env-file` you load a specific file (a read error is fatal); otherwise a `.env` file in the current directory — or any parent, found by walking up — is loaded automatically; a real environment variable always wins over the file.

The three Jira values are required. The fourth, `SHIPNOTES_GITHUB_REPO` (or `--github-repo`), is the GitHub repository used to build commit and pull-request links; it accepts a URL, an SSH remote, or the `org/repo` shorthand, and is **inferred from the repository's git remote** when left unset, so in the common case you configure only the three Jira variables. `internal/infrastructure/git/remote.go` reads `origin` (then `upstream`), parses the org and repo out of the remote URL, and builds `https://<host>/<org>/<repo>` (`ParseGithubSpec` does the same for an explicit value, assuming `github.com` for the bare `org/repo` form). A custom SSH host alias (e.g. `git@github-work:org/repo.git` from `~/.ssh/config`) is resolved to its real hostname via `ssh -G`. The GitHub repository is optional: the cli's resolver (`cli.go`) warns and continues — omitting the links — when none resolves, and warns when the host is not `github.com`; an explicit value that cannot be parsed is fatal.

## Architecture

The architecture is documented in [`ARCHITECTURE.md`](./ARCHITECTURE.md) (arc42-light) — the single source of truth for the DDD / hexagonal layering (`cli → application → domain ← infrastructure`), the ports and their adapters, the runtime flow, and the design decisions. Read it before changing structure, and keep it in sync (see [Keep ARCHITECTURE.md in sync](#keep-architecturemd-in-sync-mandatory) above).

When reading the code, start at `internal/application/app.go` — the use case that orchestrates the whole flow through the ports. The file map below is the quick index for *where things live*; ARCHITECTURE.md explains *what and why*.

### Navigating the code

Prefer the **Go language server (`gopls`) via the LSP tool** when it is available — `goToDefinition`, `findReferences`, `documentSymbol`/`workspaceSymbol`, and `goToImplementation` (e.g. "which adapter implements this port?") are faster and more precise than text search across this small hexagonal codebase. It is optional: if `gopls` is not on `PATH`, fall back to `rg`/`fd`. Install it once with `go install golang.org/x/tools/gopls@latest` (it never ships in the binary — dev-only tooling, like `golangci-lint`).

## Directory map

```text
shipnotes/
├── main.go                          # Thin entry point: calls cli.Run.
├── internal/
│   ├── domain/                       # THE CORE — entities + ports, no I/O.
│   │   ├── commit/   commit.go       #   Commit entity + revert/reapply/key rules.
│   │   │             repository.go   #   Port: read commits (Validate, Log).
│   │   ├── issue/    issue.go        #   Issue entity.
│   │   │             provider.go     #   Port: load issues by key.
│   │   ├── notes/    model.go        #   The shipnotes data model + Coordinates.
│   │   │             builder.go      #   Domain service: commits + issues → model.
│   │   │             renderer.go     #   Port: render the model to text.
│   │   └── report/   reporter.go     #   Port: progress messages.
│   ├── application/ app.go           # Use case: orchestrates the whole flow via
│   │                                 #   ports (+ Writer and IssueSearcher ports).
│   ├── infrastructure/               # ADAPTERS — implement the ports.
│   │   ├── git/         repository.go #   Runs `git`; validates refs (commit.Repository).
│   │   │               parser.go     #     Raw `git log` text → commit.Commit values.
│   │   │               remote.go     #     Infers/parses the GitHub base URL (remote or --github-repo).
│   │   ├── jira/       client.go     #   Jira REST API → issue.Issue (issue.Provider + IssueSearcher).
│   │   │               types.go      #     Jira API response types (JSON only).
│   │   │               errors.go     #     Friendly network / API error messages.
│   │   ├── markdown/   renderer.go   #   text/template render (notes.Renderer).
│   │   │               templates/shipnotes.tmpl  # The Markdown template.
│   │   ├── config/     config.go     #   Loads + validates the 3 Jira vars (flag or
│   │   │               dotenv.go     #     env); stores the resolved GitHub URL. .env reader.
│   │   ├── terminal/   terminal.go   #   Colored console output (report.Reporter).
│   │   └── fileoutput/ writer.go     #   Writes the Markdown file (application.Writer).
│   └── cli/         cli.go           # Interface layer + composition root.
│                    args.go          #   Flag parsing, usage; --jql/--checked-statuses/--exclude-commits, --jira-*/--github-repo flags.
│                    repo.go          #   Resolves the repository directory.
│                    errors.go        #   Prints config-validation problems.
├── testdata/                         # Test fixtures + golden output (see below).
└── docs/solutions/                   # Documented solutions to past problems (bugs,
                                      #   best practices, design patterns), organized by
                                      #   category with YAML frontmatter (module, tags,
                                      #   problem_type). Relevant when implementing or
                                      #   debugging in documented areas.
```

## The golden-file strategy

`testdata/cases/*.json` are sample inputs (each one is a serialized `notes.ReleaseNotes`). `testdata/golden/*.golden` are the **exact** Markdown the template produces for those inputs. `internal/infrastructure/markdown/renderer_test.go` renders each case and asserts the bytes match its golden file. This is how an unintended rendering change is caught.

To regenerate the golden files after an intentional template or model change:

```bash
go test ./internal/infrastructure/markdown -update
```

Then review the resulting diff before committing it — the diff *is* the record of what your change did to the output.

## Coding rules

- **Formatting:** `gofmt`/`gofumpt` decide formatting. Never hand-format.
- **Linting:** The linter is configured to be as strict as is practical (`.golangci.yml` enables *every* linter, then disables a documented few). Fix issues rather than adding `//nolint` — and when a `//nolint` is truly warranted, it must include a `// reason`.
- **Naming:** Files are lowercase, one clear responsibility each. Exported names (capitalized) are part of a package's public surface; keep them small.
- **Errors:** Return errors, don't panic (the one exception is `template.Must` at startup, where a broken bundled template is a build-time bug). Handle errors with the early-return guard pattern. User-facing error text is intentionally descriptive prose.
- **No globals with state.** Package-level `var`s are only used for compiled regular expressions and the parsed template (immutable, shared, idiomatic).
- **Comments explain *why*,** not *what* the next line literally does. Assume the reader knows less Go than you; spell out non-obvious intent.

## Markdown & docs style

- **One line per paragraph — do not hard-wrap prose.** Write each paragraph as a single line and let editors soft-wrap it for display. This keeps diffs clean (an edit changes one line, not a whole reflowed block).
- A real line break inside a paragraph is only for genuine structure (list items, table rows, code blocks), never to fit a column width.
- The one-line-per-paragraph rule is a convention the linter cannot enforce, so follow it by hand. It does **not** apply to the byte-locked output files (`internal/.../templates/*.tmpl`, `testdata/golden/*.golden`) — never reflow those.
- **Lint and auto-format Markdown with `markdownlint-cli2`** (config: `.markdownlint-cli2.yaml`). After editing any `*.md`, run the fixer and leave it reporting zero errors:

  ```bash
  npx markdownlint-cli2 --fix   # auto-format, then report anything it can't fix
  npx markdownlint-cli2         # check only, no changes
  ```

  The config disables the line-length rule (`MD013`, to allow long single-line paragraphs) and table-padding rule (`MD060`), and skips generated/locked files (`.remember/`, `testdata/`). ASCII diagrams use ` ```text ` fences.

## Go concepts for non-Go readers

- **Package:** a folder of `.go` files sharing a name. Code in `internal/` is private to this project.
- **Exported vs unexported:** Capitalized names (`Build`, `Commit`) are public; lowercase names (`parseCommit`) are private to their package.
- **`error` return:** Functions return an `error` as their last value; `nil` means success. Callers check `if err != nil { ... }`.
- **Pointer for "optional":** `*SummaryData` can be `nil`, which the template reads as "no summary section."
- **Struct tags:** the `` `json:"..."` `` text on struct fields maps Go field names to JSON keys. Ours intentionally match the Jira API (for the `jira` types) and the on-disk golden-test fixtures (for the `notes` model), so do not "tidy" them.
- **`text/template` whitespace:** `{{-` trims spaces/newlines before a tag and `-}}` trims after. That is how the template controls exact blank lines — the golden tests will catch any mistake.

## Git conventions

Follow the repository-wide git commit conventions in [`../CLAUDE.md`](../CLAUDE.md): commits use the Conventional Commits format, agents commit only when explicitly asked, and never push without being asked.

Project-specific note: scope the type with the package or area touched when it helps, e.g. `feat(gitlog): …`, `fix(config): …`, `feat(cli): …`.
