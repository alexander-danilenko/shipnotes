# Architecture — shipnotes

Format: **arc42 (light)** — a trimmed subset of the [arc42](https://arc42.org) template that keeps the sections worth maintaining for a small, single-binary tool and drops the ceremony.

> [!IMPORTANT]
> **This document is a living contract and must stay in sync with the code.** Any change that alters the structure described here — adding/removing/renaming a package, changing a port (interface) or adapter, changing the end-to-end flow, adding a dependency, or changing the runtime/deployment story — **must update this file in the same change**. See [§12 Keeping this document in sync](#12-keeping-this-document-in-sync) for the exact map of code → section. Cosmetic edits (renaming a local variable, fixing a typo, internal refactors that don't cross a package or port boundary) do not require an update.

---

## 1. Introduction and goals

`shipnotes` generates a Markdown release-notes file from git history, annotating each commit with the status of its linked Jira issue.

**Top quality goals (in priority order):**

| # | Goal | What it means in practice |
|---|------|---------------------------|
| 1 | **Readability for non-Go readers** | A few obvious lines beat one clever line. Every exported symbol has a plain-English doc comment. |
| 2 | **Zero runtime dependencies** | One static binary; needs only the `git` command and network access to the Jira REST API. No external Go modules (`go.mod` has no `require` block). |
| 3 | **Stable output contract** | The exact Markdown produced is locked by golden-file tests; rendering changes are deliberate, reviewed diffs. |
| 4 | **Workflow-agnostic** | Grouping has no built-in notion of "done": issues are grouped by whatever status names they have. The opinions are opt-in: `--checked-statuses` (a configurable regexp) marks matching statuses as completed checkboxes (defaults to a "done"-like pattern, emptyable to disable), and `--exclude-commits` (a regexp, empty by default) drops matching commits from the notes into an "Excluded commits" section. Works on any repo and any Jira setup with zero config. |

**Primary use case:** a developer runs `shipnotes <commit_hash>` and gets a `SHIPNOTES.md` summarizing everything between that commit and `HEAD`.

## 2. Constraints

- **Language/toolchain:** Go (see `go.mod` for the version). `gofmt`/`gofumpt`, `go vet`, and `golangci-lint` (all linters on, a documented few off) are the arbiters of style; they must report zero issues.
- **Standard library only at runtime.** Each capability maps to a Go built-in: templating → `text/template`, HTTP → `net/http`, URL/email validation → `net/url`/`net/mail`, CLI flags → `flag`. `.env` parsing and ANSI color are tiny hand-written helpers. Adding a dependency requires real justification.
- **No stateful globals.** Package-level `var`s only for compiled regexps and the parsed template (immutable, shared).
- **Errors, not panics** (the lone exception is `template.Must` at startup).

## 3. Context and scope

```text
            ┌──────────────────────────────────────────────┐
   user  ─▶ │                  shipnotes                    │ ─▶ SHIPNOTES.md
 (CLI args) │  (single binary; reads git, calls Jira REST)  │    (Markdown file)
            └───────────────┬───────────────────┬──────────┘
                            │                   │
                      `git` command        Jira REST API
                     (local subprocess)     (network, read-only)
```

**External interfaces:**

| Partner | Direction | Via |
|---------|-----------|-----|
| The user | in: commit hash + flags; out: a Markdown file + console progress | `flag`, stdout/stdin |
| A git repository | in: refs, commit log | the `git` CLI as a subprocess |
| Jira | in: issue fields (summary, status) | Jira REST API, read-only, over HTTPS |
| Configuration | in: 3 Jira values + an optional GitHub repo (the repo inferable from the git remote) | flags + real env + nearest `.env` |

## 4. Solution strategy

- **DDD / hexagonal (ports-and-adapters).** A pure domain core surrounded by adapters. Dependencies point only inward: `cli → application → domain ← infrastructure`.
- **Ports are Go interfaces owned by inner layers; adapters are structs in `infrastructure`/`cli` that satisfy them.** This keeps the domain testable with fakes and free of I/O.
- **One composition root** (`internal/cli/cli.go`, `buildService`) is the single place that wires concrete adapters into the application service.
- **Golden-file tests** lock the rendered Markdown so output changes are deliberate.

## 5. Building block view

### Level 1 — layers

```text
cli  ──▶  application  ──▶  domain  ◀──  infrastructure
(driving adapter +         (entities + ports;     (adapters that
 composition root)          no I/O)                 implement the ports)
```

| Layer | Package(s) | Responsibility | May import |
|-------|-----------|----------------|------------|
| **domain** | `internal/domain/{commit,issue,notes,report}` | Entities, domain rules, and the **ports** (interfaces) the core needs. | stdlib + other domain packages only |
| **application** | `internal/application` | Orchestrates the use case through ports. Owns the `Writer` and `IssueSearcher` ports. | domain |
| **infrastructure** | `internal/infrastructure/{git,jira,markdown,config,terminal,fileoutput}` | Adapters implementing the ports (git, Jira, Markdown, config, terminal, file output). | domain |
| **cli** | `internal/cli` | Interface layer + composition root: parse args, build adapters, run, present. The only package that imports every layer. | all |

### Level 2 — ports and their adapters

| Port (interface) | Declared in | Adapter (implementation) |
|------------------|-------------|--------------------------|
| `commit.Repository` (`Validate`, `Log`) | `domain/commit/repository.go` | `infrastructure/git` (`repository.go`, `parser.go`) |
| `issue.Provider` (`LoadByKeys`) | `domain/issue/provider.go` | `infrastructure/jira` (`client.go`, `types.go`, `errors.go`) |
| `application.IssueSearcher` (`SearchByJQL`) | `application/app.go` | `infrastructure/jira` (`client.go`) |
| `notes.Renderer` (`Render`) | `domain/notes/renderer.go` | `infrastructure/markdown` (`renderer.go` + `templates/shipnotes.tmpl`) |
| `report.Reporter` | `domain/report/reporter.go` | `infrastructure/terminal` (`terminal.go`) |
| `application.Writer` (`Write`) | `application/app.go` | `infrastructure/fileoutput` (`writer.go`) |

### Level 2 — domain internals

- **`notes.Builder`** (`domain/notes/builder.go`) — the domain service that turns commits + issues into the `notes.ReleaseNotes` model. Depends on `issue.Provider` and `report.Reporter` ports, and holds a `notes.StatusMatcher` (marks completed issues) and a `notes.CommitMatcher` (excludes commits). Exclusion is the first gate: a matched commit leaves the history and summary and is listed only under "Excluded commits", even if it is also a revert/reapply.
- **`notes.StatusMatcher`** (`domain/notes/status.go`) — a domain value wrapping a compiled, case-insensitive, fully-anchored regexp; decides which issue statuses render as completed (`[x]`) checklist items. Its zero value (and an empty pattern) matches nothing, keeping checking opt-in.
- **`notes.CommitMatcher`** (`domain/notes/exclude.go`) — a domain value wrapping a compiled, case-insensitive, **unanchored** regexp (it matches a prefix/substring, not a whole value); decides which commits `--exclude-commits` drops, testing the commit subject (which carries the Jira key, so one pattern excludes by type or by ticket). Its zero value (and an empty pattern) matches nothing, keeping exclusion opt-in.
- **`notes` model** (`domain/notes/model.go`) — `ReleaseNotes`, `HeaderData`, `CommitView`, `IssueView` (with a `Checked` flag for the checkbox state), `StatusGroup`, `SummaryData` (including the `Excluded` commit list), plus `Coordinates` (repo/Jira base URLs as a domain value object). The JSON tags double as the on-disk shape of the golden-test fixtures.
- **`commit` / `issue` entities** — `Commit` (with revert/reapply/key rules) and `Issue` (the loaded issue's key, title, and status).

### Supporting infrastructure (non-port helpers)

- `infrastructure/git/remote.go` — `InferRemoteBaseURL` derives the GitHub base URL from the git remote (`origin`, then `upstream`), resolving SSH host aliases via `ssh -G`; `ParseGithubSpec` turns an explicit `--github-repo`/`SHIPNOTES_GITHUB_REPO` value (a URL, an SSH remote, or the bare `org/repo` shorthand → `github.com`) into the same base URL.
- `infrastructure/config/{config.go,dotenv.go}` — `LoadDotEnv` reads the `.env`; `Load` validates the three required Jira values (a flag overrides the environment) and records the already-resolved, optional GitHub base URL. Hand-written `.env` reader.

## 6. Runtime view

The end-to-end flow (start reading at `internal/application/app.go`, `Run`):

```text
flags ─▶ validate commit ─▶ git log ─▶ parse commits ─▶ load Jira issues
      ─▶ build data model ─▶ render template ─▶ write Markdown file
```

Step by step:

1. **`cli.Run`** parses args (including the optional `--jql` query), compiles the `--checked-statuses` and `--exclude-commits` regexps into a `notes.StatusMatcher` and a `notes.CommitMatcher` (a bad pattern fails fast before any git/Jira work), resolves the repo dir, and sets up a signal-cancellable `context.Context`.
2. **`loadSettings`** loads the `.env`, then resolves the optional GitHub base URL (`--github-repo`/`SHIPNOTES_GITHUB_REPO`, else the inferred git remote — a flag/env value that cannot be parsed is fatal; it warns when none resolves or the host is not `github.com`), then `config.Load` validates the Jira values (flag over env, real env over `.env`).
3. **`buildService`** (composition root) wires the concrete adapters into `application.Service`, passing the `notes.StatusMatcher` and `notes.CommitMatcher` into `notes.NewBuilder`.
4. **`Service.Run`**: `repo.Validate` → `repo.Log` → (if `--jql` given) `searcher.SearchByJQL` → `builder.Build` (calls `issue.Provider.LoadByKeys`) → `renderer.Render` → `writer.Write`.
5. **`cli.generate`** presents the result (commit count + output path) and returns the process exit code.

**Cancellation:** `SIGINT`/`SIGTERM` cancels the context, which propagates into git and Jira calls.

## 7. Deployment view

- **Build:** `go build -o shipnotes .` → one static binary, no install step.
- **Run anywhere** Go-built binaries run, provided `git` is on `PATH` and the Jira host is reachable.
- **Configuration at runtime:** three required Jira values plus an optional GitHub repository (see `.env.example`). Each is settable as a flag (`--jira-base-url`, `--jira-email`, `--jira-token`, `--github-repo`) or an environment variable; precedence is flag > real env > nearest `.env` (walking up from the working dir, or an explicit `--env-file`). The GitHub repository is inferred from the git remote when unset and is optional — the notes still render (with commit/PR links omitted) when none can be determined.
- **Release & distribution:** pushing a `v*` tag triggers `.github/workflows/release.yml`, which runs **GoReleaser** (`.goreleaser.yaml`) to cross-compile the binary for linux/darwin/windows × amd64/arm64, package the archives plus a `checksums.txt`, and publish a GitHub release. The release tag is stamped into the binary via `-ldflags -X main.version=…` and reported by `shipnotes --version`. GoReleaser is dev/CI-only tooling — it never ships in the binary, so goal 2 (zero runtime dependencies) still holds.

## 8. Crosscutting concepts

- **Configuration & inference** — precedence is flag > real env > `.env`; the optional GitHub repository additionally falls back to the inferred git remote. See §7 and `infrastructure/config`, `infrastructure/git/remote.go`.
- **Error handling** — return errors with descriptive, user-facing prose; handle with the early-return guard pattern; no panics (except `template.Must`).
- **Progress reporting** — the `report.Reporter` port keeps the domain free of the terminal; `infrastructure/terminal` provides colored output via a small ANSI helper.
- **Output stability** — the Markdown template (`markdown/templates/shipnotes.tmpl`) plus golden tests are the output contract (see §11 / the testing strategy).
- **Workflow-agnosticism** — no hard-coded "done" statuses in grouping; issues are grouped by their status text, sorted alphabetically. The two opt-in opinions are `--checked-statuses` (a `notes.StatusMatcher`, sets the checkbox state) and `--exclude-commits` (a `notes.CommitMatcher`, filters commits out); both are disabled by an empty pattern, restoring fully neutral output.

## 9. Architecture decisions

| Decision | Rationale |
|----------|-----------|
| **Hexagonal / DDD layering** | Keeps a pure, testable domain and isolates git/Jira/FS/terminal behind ports. |
| **Zero external dependencies** | Simplicity, security, and a trivial build/run story; the stdlib covers every need. |
| **Shell out to `git` rather than a Go git library** | Avoids a heavy dependency; matches the "use what's installed" philosophy. |
| **Golden-file tests for output** | Treats rendered Markdown as a contract; any change is a reviewed diff. |
| **Infer the GitHub repository from the git remote** | Minimizes required configuration to the three Jira variables in the common case. |
| **One optional `SHIPNOTES_GITHUB_REPO` / `--github-repo`, replacing the old org/repo/URL trio** | The renderer only ever needed the GitHub *base URL*, so the three coordinate variables collapsed into one. It accepts a URL, an SSH remote, or the bare `org/repo` (assumed `github.com`), parsed by `git.ParseGithubSpec`. It is **optional**: when neither flag, env, nor git remote yields one, the cli warns and renders the notes with commit/PR links omitted — the template degrades to plain hashes and leaves `(#123)` references literal; a non-`github.com` host warns too (the links use GitHub's `/commit/` and `/pull/` scheme). An explicit but unparseable value is fatal, since the user named a specific repo. |
| **Every config value also settable as a flag (`--jira-*`, `--github-repo`)** | Lets the tool run with no `.env` file (e.g. CI) — a flag takes precedence over the environment. Resolution lives in the cli composition root, which keeps `config` and `git` as independent sibling adapters: `config` validates the Jira values, the cli orchestrates the git-backed GitHub resolution and its warnings. |
| **Workflow-agnostic status grouping** | Works on any Jira setup without per-project config. |
| **`--jql` as the sole release-issue source (no `--ids`, no interactive prompt)** | One non-interactive, script-friendly input; JQL expresses both explicit key lists (`key IN (…)`) and richer selections (`fixVersion`, `project`). The Jira search reuses the existing `/search/jql` adapter. Omitting `--jql` falls back to summarizing every issue in the commit range. |
| **Warn (don't fail) when `--jql` matches no issues** | A zero-result query is valid but rarely intended, so the Jira adapter emits a warning and the run continues with the commit-range fallback. The builder distinguishes "no selection" (nil list → fallback note) from "selection matched nothing" (non-nil empty list → adapter already warned), so the user sees one clear message, not two. |
| **`--checked-statuses` as an opt-in regexp marking completed issues** | Status grouping stays workflow-agnostic, but the checklist boxes carry one configurable opinion: issues whose full status text matches a case-insensitive regexp render as `[x]`. The match is a domain value (`notes.StatusMatcher`), anchored (`^(?:…)$`) so a pattern matches a whole status not a substring, defaulting to `done\|ready to release\|ready for release`. An empty pattern disables checking and restores fully status-neutral output. Reverted/reapplied lists have no status, so they stay unchecked. |
| **`--exclude-commits` as an opt-in regexp filtering commits** | Lets a release manager drop noise (docs/chore/etc.) without hiding it: matched commits are *relocated*, not deleted, to an "Excluded commits" section so the notes stay auditable (it renders even when the range has no Jira issues). The match is a domain value (`notes.CommitMatcher`), **unanchored** (it matches a prefix/substring like `^(chore\|docs):`, unlike the whole-value `StatusMatcher`), tested against the commit subject — which carries the Jira key, so one pattern excludes by type or by ticket. It is the first gate (an excluded commit never also appears under reverted/reapplied or in the summary, and its author drops from Participants), and is empty by default so the standard output is unchanged. |
| **Status-specific Jira API error messages with Jira's own detail** | `infrastructure/jira` parses the error body (`errorMessages`) and tailors the "Possible causes / Troubleshooting" guidance per HTTP status (400 → JQL, 401 → credentials, 403 → permissions, 404 → endpoint, 429 → rate limit), so a malformed JQL points at the query instead of generic credential advice. |
| **Release via GoReleaser + GitHub Actions (tag-triggered)** | Reproducible cross-platform binaries with checksums on every `v*` tag, with no hand-built release steps; the tooling is CI-only and keeps the binary dependency-free. |
| **Single `#` (h1) per document, no `---` rules** | `# Release Notes` is the lone document title; "Participants" and "Commit history" render as `##` siblings of "Release summary" rather than competing h1s. One h1 is the canonical Markdown title convention — it keeps the heading outline well-formed for tables of contents, anchor generation, and Markdown linters, and reflects that the whole file *is* one release-notes document with several sections. The horizontal rules that previously separated those h1 blocks were dropped: an `##` heading already reads as a section break in every Markdown viewer, so the rules added visual noise without semantic value. |

## 10. Quality requirements

- **Correctness gate:** `go build`, `go vet`, `go test ./...`, and `golangci-lint run ./...` must all pass with zero issues before any task is considered complete.
- **Readability:** code understandable by people who have never written Go (§1).
- **Output stability:** golden tests must pass; intentional changes regenerate goldens (`go test ./internal/infrastructure/markdown -update`) and review the diff.

## 11. Risks and technical debt

- **Dependence on the external `git` CLI** — behavior/format differences across git versions are a latent risk; mitigated by parsing a stable `git log` format.
- **Jira REST API coupling** — API/auth changes can break issue loading; isolated to `infrastructure/jira`.
- **Golden tests are strict** — any unintended whitespace/template change fails the build (this is by design, but means template edits need care).

## 12. Keeping this document in sync

This file is **not** auto-generated; it is maintained by whoever (human or AI) changes the code. The contract: **when a change crosses one of the boundaries below, update the named section in the same commit/PR.**

| If you change… | Update section(s) |
|----------------|-------------------|
| A package: add / remove / rename, or move responsibility between packages | §5 Building block view (and the directory map in `CLAUDE.md`) |
| A **port** (interface in `domain/*` or `application/app.go`) or its adapter | §5 (Level 2 port/adapter tables) |
| The end-to-end flow / order of steps in `application/app.go` or `cli/cli.go` | §6 Runtime view |
| Runtime requirements, build command, or configuration (env vars, `.env`, inference) | §2 Constraints, §7 Deployment, §8 Crosscutting |
| Add or remove a dependency (`go.mod`) | §1 (goal 2), §2 Constraints, §9 Decisions |
| A significant design decision or trade-off | §9 Architecture decisions |
| The output contract (template / golden files) | §1 (goal 3), §10, §11 |

If a change makes a statement here false, fix the statement — a drifted architecture doc is worse than none. Keep edits surgical: change the affected rows/sections, not the whole document.
