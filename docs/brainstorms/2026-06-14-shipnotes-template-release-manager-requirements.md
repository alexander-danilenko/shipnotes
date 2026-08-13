# Requirements: Template improvements for release managers

Date: 2026-06-14
Status: Ready for planning

## Problem

A release manager uses the generated notes (in a PR or locally) as a live verification checklist: they tick checkboxes while reviewing each release issue and its commits one by one, typically opening each Jira ticket to read its release steps. Today the notes make that pass harder than it needs to be: the release summary lists issues and a separate table lists commits, with nothing connecting the two halves — so verifying one issue means manually scanning the commit table for its commits. Several sections (`Missing`, `Extra`, `Reverted`, `Reapplied`) also assume the reader already knows what they mean and what to do about them.

## Goal

Make the existing release notes faster to verify against, without changing the tool's workflow-agnostic nature. Two focused, template-driven changes — no new Jira API calls, no new flags.

## In scope

### 1. Issue → commit sub-bullets

Under each issue in the release summary, list that issue's commits as indented sub-bullets: a linked short hash followed by the commit topic. This lets the release manager verify an issue and click straight into its commits in one motion instead of scanning the commit-history table.

- Applies to the grouped-by-status issues (the `ByStatus` section) and to `Extra (not in release list)`, since those issues also have commits.
- `Missing` issues have no commits by definition, so they remain single lines (unchanged).
- Sub-bullets are plain links, not checkboxes. The issue line keeps its checkbox; the commits beneath it are reference links the RM clicks while verifying.
- Each sub-bullet reuses the same commit topic formatting already used in the commit table (Jira/PR references linked, pipes escaped).
- The commits under an issue are ordered chronologically — newest at the top, oldest at the bottom (older = lower) — matching the git-log order already used by the commit-history table. They are not sorted by hash or issue key.

Shape (confirmed against a side-by-side preview):

```text
### Done

- [ ] [CX-101](…) Add login page
    - [`abc1234`](…) CX-101: Add login page (#42)
    - [`bcd2345`](…) CX-101: handle edge case
- [ ] [CX-105](…) Fix logout
    - [`efg3456`](…) CX-105: Fix logout
```

### 2. Self-explaining section headers

Add a one-line description directly under each of these section headings, stating what the section contains and the action it implies, so any reader (including a stakeholder reviewing the PR) understands it without prior context:

- `Missing` — issues expected in this release that never appeared in a commit (decide whether the release can still be cut).
- `Extra (not in release list)` — issues found in commits but not on the release list (confirm they belong).
- `Reverted commits` — changes backed out in this range (they did not ship).
- `Reapplied commits` — previously reverted changes that were re-shipped (verify they are intended).

The exact wording is for planning to finalize; it must stay neutral and must not introduce any notion of which status means "done."

## Out of scope

- One-line issue roster (considered, then dropped).
- Surfacing Jira "release steps" from issue comments — the user is pursuing this as a separate feature; it requires a new Jira comments API call.
- Per-status counts, a diff-friendly/human-readable timestamp, and making commits individually checkable.

## Assumptions and decisions

- The workflow-agnostic principle is preserved: neither change adds status semantics or judges which statuses are "done." Sub-bullets and descriptions are presentation only.
- These changes cross the **output contract**: the golden files in `testdata/golden/` must be regenerated (`go test ./internal/infrastructure/markdown -update`) and the resulting diff reviewed as the record of the change.
- The issue → commit mapping requires the **notes model** to carry commits per issue (today `IssueView` has none) and the builder to populate them. Because this touches a domain model and the rendered output, `ARCHITECTURE.md` is updated in the same change per the project's sync rule.
- How an issue's commits are determined (e.g. primary issue key vs. all referenced keys) is an implementation decision for planning; it should avoid listing the same commit under multiple issues.

## Known limitation (recorded, not addressed here)

Because the notes file is fully regenerated on every run, re-running shipnotes mid-verification overwrites any checkboxes the release manager has already ticked. This is unchanged by this work and out of scope, but worth noting so it is not a surprise; a future "preserve checkbox state" or stable-ordering effort could address it.

## Definition of done

- `shipnotes.tmpl` renders issue → commit sub-bullets (summary + `Extra`) and the four section descriptions.
- The notes model/builder provide each issue's commits.
- `ARCHITECTURE.md` reflects the model and output-contract changes.
- Golden files regenerated and reviewed; `go build`, `go vet`, `go test ./...`, and `golangci-lint run ./...` all pass clean.
