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
| 4 | **Workflow-agnostic** | No built-in notion of "done"; issues are grouped by whatever status names they have. Works on any repo and any Jira setup with zero config. |

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
| Configuration | in: 6 env vars (3 inferable from the git remote) | real env + nearest `.env` |

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

- **`notes.Builder`** (`domain/notes/builder.go`) — the domain service that turns commits + issues into the `notes.ReleaseNotes` model. Depends on `issue.Provider` and `report.Reporter` ports.
- **`notes` model** (`domain/notes/model.go`) — `ReleaseNotes`, `HeaderData`, `CommitView`, `IssueView`, `StatusGroup`, `SummaryData`, plus `Coordinates` (repo/Jira base URLs as a domain value object). The JSON tags double as the on-disk shape of the golden-test fixtures.
- **`commit` / `issue` entities** — `Commit` (with revert/reapply/key rules) and `Issue` (the loaded issue's key, title, and status).

### Supporting infrastructure (non-port helpers)

- `infrastructure/git/remote.go` — infers org / repo / GitHub base URL from the git remote (`origin`, then `upstream`), resolving SSH host aliases via `ssh -G`.
- `infrastructure/config/{config.go,dotenv.go}` — loads and validates the 6 env vars (3 fall back to the inferred git-remote defaults); hand-written `.env` reader.

## 6. Runtime view

The end-to-end flow (start reading at `internal/application/app.go`, `Run`):

```text
flags ─▶ validate commit ─▶ git log ─▶ parse commits ─▶ load Jira issues
      ─▶ build data model ─▶ render template ─▶ write Markdown file
```

Step by step:

1. **`cli.Run`** parses args (including the optional `--jql` query), resolves the repo dir, sets up a signal-cancellable `context.Context`.
2. **`loadSettings`** infers GitHub org/repo/base-URL from the git remote, then `config.Load` reads + validates env (real env always wins over `.env`).
3. **`buildService`** (composition root) wires the concrete adapters into `application.Service`.
4. **`Service.Run`**: `repo.Validate` → `repo.Log` → (if `--jql` given) `searcher.SearchByJQL` → `builder.Build` (calls `issue.Provider.LoadByKeys`) → `renderer.Render` → `writer.Write`.
5. **`cli.generate`** presents the result (commit count + output path) and returns the process exit code.

**Cancellation:** `SIGINT`/`SIGTERM` cancels the context, which propagates into git and Jira calls.

## 7. Deployment view

- **Build:** `go build -o shipnotes .` → one static binary, no install step.
- **Run anywhere** Go-built binaries run, provided `git` is on `PATH` and the Jira host is reachable.
- **Configuration at runtime:** 6 env vars (see `.env.example`). Loaded from the real environment and the nearest `.env` (walking up from the working dir, or an explicit `--env-file`). Three Jira vars are usually all the user must set; the three GitHub vars are inferred from the git remote when unset.
- **Release & distribution:** pushing a `v*` tag triggers `.github/workflows/release.yml`, which runs **GoReleaser** (`.goreleaser.yaml`) to cross-compile the binary for linux/darwin/windows × amd64/arm64, package the archives plus a `checksums.txt`, and publish a GitHub release. The release tag is stamped into the binary via `-ldflags -X main.version=…` and reported by `shipnotes --version`. GoReleaser is dev/CI-only tooling — it never ships in the binary, so goal 2 (zero runtime dependencies) still holds.

## 8. Crosscutting concepts

- **Configuration & inference** — env-var precedence (real env > `.env` > inferred git-remote defaults); see §7 and `infrastructure/config`, `infrastructure/git/remote.go`.
- **Error handling** — return errors with descriptive, user-facing prose; handle with the early-return guard pattern; no panics (except `template.Must`).
- **Progress reporting** — the `report.Reporter` port keeps the domain free of the terminal; `infrastructure/terminal` provides colored output via a small ANSI helper.
- **Output stability** — the Markdown template (`markdown/templates/shipnotes.tmpl`) plus golden tests are the output contract (see §11 / the testing strategy).
- **Workflow-agnosticism** — no hard-coded "done" statuses; issues are grouped by their status text, sorted alphabetically.

## 9. Architecture decisions

| Decision | Rationale |
|----------|-----------|
| **Hexagonal / DDD layering** | Keeps a pure, testable domain and isolates git/Jira/FS/terminal behind ports. |
| **Zero external dependencies** | Simplicity, security, and a trivial build/run story; the stdlib covers every need. |
| **Shell out to `git` rather than a Go git library** | Avoids a heavy dependency; matches the "use what's installed" philosophy. |
| **Golden-file tests for output** | Treats rendered Markdown as a contract; any change is a reviewed diff. |
| **Infer repo coordinates from the git remote** | Minimizes required configuration to the three Jira variables. |
| **Workflow-agnostic status grouping** | Works on any Jira setup without per-project config. |
| **`--jql` as the sole release-issue source (no `--ids`, no interactive prompt)** | One non-interactive, script-friendly input; JQL expresses both explicit key lists (`key IN (…)`) and richer selections (`fixVersion`, `project`). The Jira search reuses the existing `/search/jql` adapter. Omitting `--jql` falls back to summarizing every issue in the commit range. |
| **Warn (don't fail) when `--jql` matches no issues** | A zero-result query is valid but rarely intended, so the Jira adapter emits a warning and the run continues with the commit-range fallback. The builder distinguishes "no selection" (nil list → fallback note) from "selection matched nothing" (non-nil empty list → adapter already warned), so the user sees one clear message, not two. |
| **Status-specific Jira API error messages with Jira's own detail** | `infrastructure/jira` parses the error body (`errorMessages`) and tailors the "Possible causes / Troubleshooting" guidance per HTTP status (400 → JQL, 401 → credentials, 403 → permissions, 404 → endpoint, 429 → rate limit), so a malformed JQL points at the query instead of generic credential advice. |
| **Release via GoReleaser + GitHub Actions (tag-triggered)** | Reproducible cross-platform binaries with checksums on every `v*` tag, with no hand-built release steps; the tooling is CI-only and keeps the binary dependency-free. |

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
