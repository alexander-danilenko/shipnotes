---
title: Cherry-picked work still listed as new in the release notes
date: 2026-09-02
category: logic-errors
module: already-shipped-detection
problem_type: logic_error
component: infrastructure
symptoms:
  - Tickets that shipped weeks ago appear under "Extra (not in release list)"
  - No "## Already shipped" heading is rendered, and no warning says why
  - Two commits with identical content and different hashes both sit in the commit history table
root_cause: config_error
resolution_type: code_fix
severity: high
related_components:
  - development_workflow
tags: [cherry-pick, already-shipped, patch-id, git-adapter, silent-failure, shipped-ref]
---

# Cherry-picked work still listed as new in the release notes

## Problem

Work that was cherry-picked and released ahead of the current range still read as new work in the generated notes, landing under "Extra (not in release list)" instead of "Already shipped". Two independent causes produce the same symptom, and neither announces itself — the already-shipped gate fails open, so "found nothing" and "misconfigured" look identical in the output.

## Symptoms

- A ticket the team released weeks earlier is listed under `## Extra (not in release list)`.
- The `## Already shipped` section is absent entirely, with no warning explaining the absence.
- The commit history table shows the same change twice, under two different hashes.

## What Didn't Work

- **Trusting the inferred `--shipped-ref`.** `resolveShippedRef` falls back to `git.InferDefaultBranchRef` (`internal/cli/cli.go:206`), which reads `refs/remotes/origin/HEAD` (`internal/infrastructure/git/remote.go:102-103`). On the downstream repository where this surfaced, `origin/HEAD` pointed at `origin/develop` while releases went out from `origin/master`. A ref *did* resolve, so the "no release branch found" warning never fired; the scan simply read the wrong branch and found none of the ~150 cherry-pick trailers sitting on `origin/master` (counted there at the time; not reproducible from this repository). This was the entire cause of the reported symptom — the detection code was correct and pointed somewhere useless.
- **Reading cherry-pick trailers only from the release branch.** That direction is right when the fix is cherry-picked *out* to a release branch: git writes `(cherry picked from commit …)` on the copy, so the original arriving in the range carries nothing to inspect. It is wrong when the fix was applied to the release branch *directly* and the development branch merged in afterwards — then both copies sit in one range, and only the original gets flagged.
- **Relying on the `-x` trailer alone.** Picks made through the GitHub UI carry no trailer, so `ExtractCherryPickSources` (`internal/domain/commit/commit.go:102`) returns nothing for them and no link exists to follow.
- **A test fixture that cherry-picks onto an unmoved branch.** Without `-x`, the copy shares its parent, tree, message, author, and timestamp with the original, so git hands back the *same commit object* — there is no duplicate to detect and the test fails for a reason that has nothing to do with the code under test. The branch has to move first (`internal/infrastructure/git/repository_internal_test.go:238`).
- **`git cherry` for the remaining gap.** It only compares the symmetric difference of two refs. When the range-start ref is an ancestor of `HEAD` — which it is, for `<ref>..HEAD` — the upstream-only side is empty and every commit comes back marked `+`.

## Solution

Two parts, both needed.

**1. Name the branch releases actually go out from, and make the resolved value visible.**

```bash
shipnotes <ref> --shipped-ref origin/master
```

`resolveShippedRef` now prints what it settled on, and says when the value was inferred rather than given (`internal/cli/cli.go:201`, `internal/cli/cli.go:219`):

```text
Reading already-released work from origin/develop (the repository's default branch; override with --shipped-ref).
```

`console.Header()` moved up into `Run` so this line and the pre-existing GitHub-inference warnings print *under* the banner instead of above it.

**2. Pair duplicate commits up inside the range.**

`markDuplicates` (`internal/infrastructure/git/repository.go:235`) flags commits that land the same change twice within `<ref>..HEAD`, on two kinds of evidence that fail in different places:

- an `-x` trailer naming another commit *in the same range* — an explicit link that survives a pick adapted around a merge conflict, where the content diverged;
- an identical `git patch-id --stable` (`internal/infrastructure/git/repository.go:286`) — content equality, the only signal left when the pick carried no trailer.

Both members of a pair are flagged, not one. Merge commits carry no patch of their own, so `git patch-id` emits no line for them and they can never pair with anything.

## Why This Works

The gate is all-or-nothing per issue. `alreadyShippedKeys` (`internal/domain/notes/builder.go:307`) retires a key only when *every* commit carrying it is flagged; any unflagged commit puts the key into `stillShipping` and deletes it from the shipped set. That rule exists so a ticket spanning a hotfix and a follow-up is not erased — but it also means one unflagged copy is enough to send the whole ticket back into `Extra` (`internal/domain/notes/builder.go:237`). Flagging only the original was therefore indistinguishable from flagging nothing.

Both new signals are intra-range equivalence, so nothing outside the reported range has to be readable for them to work. Cause 1 needed no code change at all — only the right ref and a way to notice when it is wrong.

## Prevention

- **An inference that gates a feature must announce what it resolved.** The existing code warned only when *nothing* resolved. A wrong-but-valid guess was silent, and a fail-open feature has no other way to tell "no evidence" from "looking in the wrong place". Both `--shipped-ref` and `--github-repo` infer from git remotes; the same trap applies to any future inference.
- **When already-shipped detection reports nothing, check the ref first, not the code.** `git symbolic-ref --short refs/remotes/origin/HEAD` against the branch releases really ship from is a two-second check that would have skipped this entire investigation.
- **In git fixtures, move the branch before cherry-picking.** A pick without `-x` onto an unmoved branch reproduces the original commit object byte-for-byte. `TestLogMarksDuplicatesInsideTheRange` (`internal/infrastructure/git/repository_internal_test.go:259`) covers both the `-x` and the trailer-less path against real `git`, and asserts the merge commit stays unflagged.
- **Detection stays fail-open.** `releasedSources` (`internal/infrastructure/git/repository.go:189`) and `patchIDs` both return nothing on any error, so a missing or unreadable ref never fails the run. That is deliberate — absent evidence is not evidence of a release — and it is exactly why the reporting line is load-bearing rather than cosmetic.
- **Known cost:** the patch-id pass reads the full diff of the range. Measured on a large downstream repository (not reproducible here) at 0.2s for a 32-commit release range and **17.5s** over full history. `Log` runs its three reads concurrently (`internal/infrastructure/git/repository.go:121-138`), which does not help here — patch-id is the long pole, not a parallelizable tail.
- **Known gap:** a copy sitting *behind* the range start with no `-x` trailer is undetectable. Finding it would mean computing patch ids over the release ref's whole history.

## Related Issues

- `ARCHITECTURE.md` §9 carries both decision rows — reading the trailer from the release branch, and pairing duplicates inside the range — with the scope boundaries stated.
- The gate's original design (2026-08-13) left out two cases, and this work reopened both: patch-id equivalence for picks made without `-x`, and duplicates whose two copies both sit in the range. The second was left out because "it never occurs in the target workflow", and that assumption did not hold.
