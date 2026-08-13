# feat: Issue → commit sub-bullets and self-explaining section headers

Date: 2026-06-14
Type: feat
Origin: docs/brainstorms/2026-06-14-shipnotes-template-release-manager-requirements.md
Depth: Standard

---

## Summary

Two reader-facing additions to the generated release notes, to speed up a release manager's verification pass:

1. **Issue → commit sub-bullets** — under each issue in the release summary, render that issue's commits as indented linked sub-bullets (short hash + topic), newest-first. Applies to the grouped-by-status issues and to `Extra`; never to `Missing`.
2. **Self-explaining section headers** — a one-line description under each of `Missing`, `Extra`, `Reverted`, and `Reapplied` stating what the section contains and the implied action.

The work is fully contained in the `notes` domain (`model.go`, `builder.go`) and the `markdown` adapter (template + golden tests), plus the mandated `ARCHITECTURE.md` sync. No new Jira API calls, no new flags, no new dependencies. The workflow-agnostic "no notion of done" rule is preserved — both changes are presentation only.

---

## Problem frame

The release notes are used as a live verification checklist (in a PR or locally): the release manager ticks issues off while reviewing each one and its commits. Today the release summary lists issues and a separate table lists commits, with nothing linking the two — verifying an issue means manually scanning the commit table for its commits. Several sections (`Missing`, `Extra`, `Reverted`, `Reapplied`) also assume the reader already knows what they mean. See origin for the full workflow and rationale.

---

## Requirements

Traceability back to the origin requirements doc:

- **R1.** Each issue in the release summary's grouped-by-status section shows its commits as indented sub-bullets: linked short hash + commit topic (see origin: "Issue → commit sub-bullets").
- **R2.** Sub-bullets also appear under `Extra` issues; `Missing` issues stay single-line (they have no commits by definition).
- **R3.** Commits under an issue are ordered newest-first (older = lower), matching the git-log order already used by the commit-history table — not sorted by hash or key.
- **R4.** A commit that references multiple Jira keys appears as a sub-bullet under *every* issue it references. This **overrides** the origin doc's tentative preference ("it should avoid listing the same commit under multiple issues"): the user confirmed all-keys during planning, and it matches the builder's existing `uniqueSortedJiraKeys` behavior (an issue already qualifies as "in commits" via any referenced key), so the alternative would be inconsistent with how issues are grouped.
- **R5.** Sub-bullets are plain links, not checkboxes; the issue line keeps its checkbox.
- **R6.** Sub-bullet topic formatting reuses the existing commit-topic formatting (Jira/PR references linked, pipes escaped) — no separate formatting path.
- **R7.** A one-line neutral description appears under each of `Missing`, `Extra`, `Reverted`, `Reapplied`, stating the section's meaning and implied action, introducing no status semantics.
- **R8.** The output contract change is captured: golden files regenerated and `ARCHITECTURE.md` updated in the same change.

---

## Key technical decisions

- **Reuse `CommitView` for sub-bullets, don't introduce a new type.** `CommitView` already carries `Hash`, `URL`, and `FormattedTopic` — exactly what a sub-bullet renders, and the same fields the Reverted/Reapplied lists already use. Add `Commits []CommitView` to `IssueView`. (Aligns with the project's "use existing shapes / stdlib hard" principle.)
- **`json:"commits,omitempty"` tag.** The model's JSON tags double as the golden-test fixture shape. `omitempty` keeps `Missing` issues (and any commit-less issue) from forcing an empty `"commits": []` into fixtures/output, so untouched fixtures need no edit.
- **Match commits to issues on all `JiraIssueIDs`, not just `PrimaryIssueID()`.** This is consistent with how an issue qualifies as "in commits" (`uniqueSortedJiraKeys` already uses all keys). A multi-key commit therefore appears under each issue it references (R4).
- **No explicit sort for sub-bullet order.** Commits arrive from git in newest-first order and the builder preserves that order by ranging in place; a per-issue list built by iterating the commits inherits newest-first order with no sort (R3). Document this reliance in the helper's doc comment.
- **Header descriptions live inside each section's existing `{{ if }}` block**, after the heading, so a description only renders when its section renders.
- **Golden regeneration is the record of the change.** Run `go test ./internal/infrastructure/markdown -update` and review the diff; never hand-edit `.tmpl`/`.golden` whitespace.

---

## Implementation units

### U1. Add `Commits` field to `IssueView`

**Goal:** Let an issue carry its commits for the template to render.
**Requirements:** R1, R2, R5, R6.
**Dependencies:** none.
**Files:** `internal/domain/notes/model.go`
**Approach:** Add `Commits []CommitView` with tag `json:"commits,omitempty"` to `IssueView`, plus a doc comment (the `revive` exported rule requires it) noting the slice is newest-first and empty for issues with no commits (e.g. all `Missing` issues). Reuse `CommitView`; do not create a new type.
**Patterns to follow:** existing `IssueView`/`StatusGroup` field style and JSON tag conventions in `model.go`; keep the "JSON tags double as fixture shape" comment intact.
**Test scenarios:** Test expectation: none — pure field addition, behavior is covered by U2 (population) and U3 (rendering).
**Verification:** `go build ./...` succeeds; the field is visible to the builder and template.

### U2. Populate per-issue commits in the builder

**Goal:** Attach each issue's commits (newest-first, all-keys match) to its `IssueView`.
**Requirements:** R1, R2, R3, R4, R6.
**Dependencies:** U1.
**Files:** `internal/domain/notes/builder.go`, `internal/domain/notes/builder_test.go`, `internal/domain/notes/builder_internal_test.go`
**Approach:** Add a pure package-private helper `commitViewsByIssue(coords, commits) map[string][]CommitView` that builds a `CommitView` per commit via the existing `baseCommitView` and appends it under every key in `commit.JiraIssueIDs`, preserving input (git/newest-first) order. In `buildSummary`, build the map once from `normalCommits`, then set `view.Commits = byIssue[key]` immediately after each `issueView(...)` call in both the `inCommits` loop and the `extra` loop. Do **not** set it in the `Missing` branch. `groupByStatus` only re-buckets/re-sorts `IssueView` values, so attached commits ride along untouched — no change there. Document the all-keys match and the git-order reliance in the helper doc comment.
**Patterns to follow:** `baseCommitView` (single source of truth for commit display fields), `flatCommitViews`, and the existing pure-helper section in `builder.go`. For tests, split by visibility: the black-box `Commits`-population scenarios go in `builder_test.go` (`package notes_test`) alongside `TestBuildSummaryCategories`, reusing `newBuilder`/`fakeProvider`; only the direct `commitViewsByIssue` unit test goes in `builder_internal_test.go` (`package notes`). Note that the existing `assertSingleIssue` helper checks only group status + issue key — it does **not** inspect `Commits` — so the sub-bullet scenarios need a new inline assertion (or small helper) that reaches `group.Issues[i].Commits` and checks `[].Hash` order, plus new commit fixtures (a two-commit-one-issue case and a multi-key commit; the existing fixtures are single-commit, single-key).
**Test scenarios:**

- Happy path: an issue with two commits → `Commits` has both, newest-first (assert order by hash).
- Multi-key commit (R4): one commit referencing two issues → appears under both issues' `Commits`.
- `Missing` issue → `Commits` is empty/nil (not rendered later).
- `Extra` issue with a commit → `Commits` populated.
- Issue grouped by status keeps its commits after `groupByStatus` re-sorts.
- White-box `commitViewsByIssue`: empty input → empty map; single multi-key commit → indexed under each key; order preserved from input.
**Verification:** `go test ./internal/domain/notes/...` passes; golden tests remain green (builder changes don't touch the fixture-driven renderer test).

### U3. Render sub-bullets in the template + regenerate goldens

**Goal:** Output the indented commit sub-bullets under summary and `Extra` issues.
**Requirements:** R1, R2, R3, R5, R6, R8.
**Dependencies:** U1 (field must exist for the template to range it); U2 (so rendered fixtures are realistic, though the golden fixture drives the test directly).
**Files:** `internal/infrastructure/markdown/templates/shipnotes.tmpl`, `testdata/cases/full.json`, `testdata/golden/full.golden`
**Approach:** Inside the `ByStatus` inner issue loop and the `Extra` inner issue loop, nest a `{{ range .Commits -}}` block emitting a two-space-indented `- [`{{ .Hash }}`]({{ .URL }}) {{ .FormattedTopic }}` line, mirroring the existing Reverted/Reapplied bullet idiom and whitespace trims (`{{- -}}`). Do **not** add the range to `Missing`. Add `commits` arrays (CommitView JSON shape: `hash`, `url`, `formattedTopic`) at the **per-issue** level — `summary.byStatus[].issues[].commits` and `summary.extra[].issues[].commits` (on each `IssueView`, not at `StatusGroup` level) — in `testdata/cases/full.json`; include at least one multi-key case and a `Missing` issue with no commits to prove it stays single-line. Regenerate via `go test ./internal/infrastructure/markdown -update` and review the diff.
**Patterns to follow:** the Reverted/Reapplied list lines in `shipnotes.tmpl`; the whitespace-control convention; CLAUDE.md "never reflow `.tmpl`/`.golden` by hand".
**Test scenarios:**

- Golden render (via `full.json`): summary issues show indented sub-bullets with linked hash + topic, newest-first.
- `Extra` issues show sub-bullets; `Missing` issues render as single lines with no sub-bullet (omitempty path).
- A multi-key commit renders under each referencing issue in the fixture.
- Topic formatting (Jira/PR links, escaped pipes) renders identically to the history table.
**Verification:** `go test ./internal/infrastructure/markdown` passes against regenerated goldens; diff reviewed and matches intent.

### U4. Add self-explaining section descriptions + regenerate goldens

**Goal:** A one-line neutral description under each of `Missing`, `Extra`, `Reverted`, `Reapplied`.
**Requirements:** R7, R8.
**Dependencies:** none on U1–U3 (pure template), but land after U3 to regenerate goldens once more cleanly.
**Files:** `internal/infrastructure/markdown/templates/shipnotes.tmpl`, `testdata/golden/full.golden`
**Approach:** After each of the four `##` headings, inside that section's existing `{{ if }}` block, add a single prose line plus a blank line before the list, minding the trim markers so the blank line isn't collapsed. Wording must be neutral and status-agnostic, e.g.: Missing — "Issues expected in this release that never appeared in a commit." Extra — "Issues found in commits but not on the release list." Reverted — "Changes backed out in this range — they did not ship." Reapplied — "Previously reverted changes that were re-shipped." Finalize exact wording during implementation. Regenerate goldens via `-update` and review.
**Patterns to follow:** existing heading blocks in `shipnotes.tmpl`; one-line-per-paragraph rule does not apply to the byte-locked template/golden.
**Test scenarios:**

- Golden render: each of the four populated sections in `full.json` shows its description line directly under the heading, above the list.
- `summary_partial.golden` (no populated Missing/Extra/Reverted/Reapplied) stays byte-identical.
**Verification:** `go test ./internal/infrastructure/markdown` passes; diff shows only the four added description lines.

### U5. Sync ARCHITECTURE.md

**Goal:** Keep the architecture contract in step with the model and output-contract changes (mandatory per CLAUDE.md).
**Requirements:** R8.
**Dependencies:** U1–U4 (describe the final state).
**Files:** `ARCHITECTURE.md`
**Approach:** Per the §12 change-map: update §5 (Building block view) where the `notes` model fields are enumerated, noting `IssueView` now carries its commits; update §1 (goal 3), §10, and §11 (the output-contract row) to reflect that issue sub-bullets and section descriptions are part of the rendered contract. Optionally add a §9 design-decision row for the all-keys commit-matching choice. Keep edits surgical. Run `npx markdownlint-cli2 --fix` then `npx markdownlint-cli2`.
**Patterns to follow:** existing ARCHITECTURE.md section style; one-line-per-paragraph prose rule.
**Test scenarios:** Test expectation: none — documentation. Verify by reading: §5 reflects the new field; §1/§10/§11 reflect the output change; markdownlint reports zero errors.
**Verification:** markdownlint clean; sections accurately describe the shipped behavior.

---

## Scope boundaries

**In scope:** the two features above and their model/builder/template/golden/architecture changes.

### Deferred for later (from origin)

- **Checkbox-wipe-on-regenerate** — re-running shipnotes mid-verification overwrites ticked checkboxes. Recorded as a known limitation; not addressed here. A future "preserve checkbox state" or stable-ordering effort could take it on.

### Outside this work

- One-line issue roster (dropped in brainstorm).
- Surfacing Jira "release steps" from issue comments (separate feature; needs a new Jira comments API call).
- Per-status counts, human-readable/diff-friendly timestamp, making commits individually checkable.

---

## Risks & dependencies

- **Output-contract regression risk.** Any template mistake changes rendered bytes. Mitigation: the golden tests are the guardrail — regenerate with `-update` and review the diff as the record; never hand-edit goldens.
- **Whitespace/indentation in the template.** Sub-bullet indentation and the description blank lines depend on correct `{{- -}}` trims. Mitigation: mirror the existing Reverted/Reapplied idiom exactly; the golden diff makes any blank-line error visible.
- **Fixture/model drift.** Because JSON tags are the fixture shape, the `full.json` edits must match the new field. Mitigation: `omitempty` limits edits to issues that should show sub-bullets; only `full.json` needs changes.
- No external dependencies; no new packages; Go 1.26, zero-dep build unchanged.

---

## Definition of done

- Sub-bullets render under summary + `Extra` issues, newest-first, multi-key commits under each issue; `Missing` stays single-line.
- The four section descriptions render under their headings.
- `IssueView` carries `Commits`; builder populates via `commitViewsByIssue` (all-keys, git-order) with unit tests.
- Golden files regenerated and diff reviewed; `ARCHITECTURE.md` updated.
- All gates clean: `go build`, `go vet ./...`, `go test ./...`, `golangci-lint run ./...`, `gofmt -l .` empty, and `npx markdownlint-cli2` zero errors.

---

## Sources & research

- Origin requirements: `docs/brainstorms/2026-06-14-shipnotes-template-release-manager-requirements.md`.
- Repo research confirmed: feature is contained in the `notes` domain + `markdown` adapter; `CommitView` is reusable for sub-bullets; commits arrive newest-first in git-log order (no sort needed); `full.json`/`full.golden` is the pair that exercises both affected sections; Go 1.26, `exhaustruct`/`tagliatelle` disabled, `revive` requires the new field's doc comment.
- Learnings: `docs/solutions/` and `CONCEPTS.md` do not exist yet — no institutional learnings to carry. Candidate for `/ce-compound` capture after this lands ("extend `IssueView` without breaking goldens"; regenerate-and-review workflow).
- No external research warranted (strong local patterns, zero-dependency stdlib project).
