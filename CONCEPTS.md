# Concepts

Shared domain vocabulary for this project — entities, named processes, and status concepts with project-specific meaning. Seeded with core domain vocabulary, then accretes as ce-compound and ce-compound-refresh process learnings; direct edits are fine. Glossary only, not a spec or catch-all.

## The reported range

### Range

The commits between the ref the user names and the current checkout — the unit of work that one set of release notes describes. Everything reported is derived from the range: nothing outside it appears, and no commit inside it is ever dropped from the commit history.

### Release list

The issues the user declares this release is meant to contain, selected by a Jira query. When no release list is given, every issue the range references is summarized instead.

The release list is a claim about intent; the range is evidence about fact. The summary sections exist to show where the two disagree, which is why an issue can appear in more than one of them rather than being reconciled away.

### Missing

An issue on the release list that no commit in the range carries — expected to ship, but nothing in the range delivers it.

### Extra

An issue carried by a commit in the range that is not on the release list — delivered, but not expected. The counterpart of Missing.

## Gates

### Gate

A rule that moves commits or issues out of the release summary and into a section of their own. The gates run in a fixed order — exclusion, then reverts, then already-shipped — and the earliest one to catch an item wins, so an excluded commit is never also reported as a revert.

A gate relocates rather than deletes. The item is still shown, in a section that says why it does not count as new work, so the notes remain auditable and nothing is silently hidden.

### Already shipped

An issue with nothing left to ship in the range, because every commit carrying it re-lands a change that has already gone out. One such commit is not enough: an issue that also carries ordinary new work stays in the release, so a ticket spanning a hotfix and a follow-up is never erased.

A change can have gone out in two shapes. It was cherry-picked away to a release branch and shipped from there, leaving the record on the copy rather than on the commit in the range; or it was applied to the release branch directly and the development branch merged in afterwards, putting both copies of one change inside a single range under different hashes. In the second shape every copy counts as already shipped — flagging one side alone would leave the issue reading as new work.

### Shipped ref

The branch or tag holding work that has already been released, named by the user or inferred from the repository's default branch.

It says *where* to look for evidence of an earlier release, never *what* counts as shipped — git's own records answer that. Detection fails open: a ref that does not resolve, cannot be read, or holds no evidence produces no findings rather than an error. A wrong ref and no ref therefore yield identical output, which is why the ref that was resolved is reported at the start of every run.

## Flagged ambiguities

- "Release branch" is used loosely in conversation for both the branch a release is cut *from* and the Shipped ref scanned for already-released work. These are frequently different branches; prefer Shipped ref when the scanned branch is meant.
