package notes

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alexander-danilenko/shipnotes/internal/domain/commit"
	"github.com/alexander-danilenko/shipnotes/internal/domain/issue"
	"github.com/alexander-danilenko/shipnotes/internal/domain/report"
)

// unknownValue is the placeholder used when an issue (or its title/status) could
// not be loaded.
const unknownValue = "Unknown"

// Builder assembles the shipnotes model from commits and issues. Build one
// with NewBuilder.
type Builder struct {
	issues   issue.Provider
	reporter report.Reporter
	// checked decides which issue statuses render as completed ("[x]") checklist
	// items. The zero value matches nothing, so the default output is unchecked.
	checked StatusMatcher
	// excluded decides which commits are dropped from the notes (into the
	// "Excluded commits" section). The zero value matches nothing, so by default
	// every commit is kept.
	excluded CommitMatcher
}

// NewBuilder wires the builder with its issue provider, progress reporter, the
// status matcher that marks completed issues in the summary, and the commit
// matcher that filters commits out of the notes.
func NewBuilder(
	issues issue.Provider, reporter report.Reporter, checked StatusMatcher, excluded CommitMatcher,
) *Builder {
	return &Builder{issues: issues, reporter: reporter, checked: checked, excluded: excluded}
}

// Build loads the relevant issues and produces the full model used to render the
// Markdown.
func (b *Builder) Build(
	ctx context.Context,
	coords Coordinates,
	commits []commit.Commit,
	releaseIssueIDs []string,
) (ReleaseNotes, error) {
	// Exclusion is the first gate: a commit matched by --exclude-commits leaves
	// the notes entirely (it is listed only under "Excluded commits"), so every
	// step below works on the kept commits and never sees the excluded ones.
	keptCommits, excludedCommits := splitExcluded(commits, b.excluded)

	normalCommits, revertedCommits := splitReverted(keptCommits)
	reappliedCommits := selectReapplied(normalCommits)

	commitKeys := uniqueSortedJiraKeys(keptCommits)

	// When the caller named no release issues, summarize every issue referenced
	// by the non-revert commits in the range, so all tickets in the diff still
	// appear in the summary. Reverts are excluded here; they keep their own
	// "Reverted commits" section.
	//
	// A nil list means the caller made no selection at all (no --jql). A non-nil
	// but empty list means a selection was made but matched nothing (a --jql query
	// that found no issues); in that case SearchByJQL has already warned about the
	// fallback, so we stay quiet here to avoid a second, misleading message.
	noSelectionMade := releaseIssueIDs == nil
	if len(releaseIssueIDs) == 0 {
		releaseIssueIDs = uniqueSortedJiraKeys(normalCommits)
		if len(releaseIssueIDs) > 0 && noSelectionMade {
			b.reporter.Warn(fmt.Sprintf(
				"⚠️  No release issue IDs given; summarizing all %d issue(s) found in the commit range.",
				len(releaseIssueIDs),
			))
		}
	}

	keysToLoad := union(commitKeys, releaseIssueIDs)

	issues, err := b.issues.LoadByKeys(ctx, keysToLoad)
	if err != nil {
		return ReleaseNotes{}, err
	}

	if len(commitKeys) > 0 && len(issues) == 0 {
		b.warnNoIssuesFound(commitKeys)
	}

	issueMap := indexByKey(issues)

	return ReleaseNotes{
		Header: HeaderData{Date: nowISO(), Repository: coords.GithubBaseURL},
		Summary: b.buildSummary(
			coords, normalCommits, releaseIssueIDs, issueMap, revertedCommits, reappliedCommits, excludedCommits,
		),
		Commits: b.buildFlatCommits(coords, keptCommits, issueMap),
		// Participants are the authors of the kept commits only: an excluded commit
		// leaves the notes, so its author is not credited here either.
		Authors: collectUniqueAuthors(keptCommits),
	}, nil
}

// warnNoIssuesFound explains a common credential mismatch: commits referenced
// issue keys, but the provider returned nothing.
func (b *Builder) warnNoIssuesFound(commitKeys []string) {
	b.reporter.Warn("⚠️  Warning: No JIRA issues were retrieved despite finding JIRA keys in commits.")
	b.reporter.Warn("   This might indicate an email + API key mismatch in your JIRA credentials.")
	b.reporter.Warn(fmt.Sprintf("   Expected to find %d issues: %s",
		len(commitKeys), strings.Join(commitKeys, ", ")))
}

// buildFlatCommits prepares every commit (in git order) for the history table,
// attaching its primary issue's status.
func (b *Builder) buildFlatCommits(
	coords Coordinates, commits []commit.Commit, issueMap map[string]issue.Issue,
) []CommitView {
	views := make([]CommitView, 0, len(commits))
	for _, c := range commits {
		view := baseCommitView(coords, c)

		if key := c.PrimaryIssueID(); key != "" {
			view.JiraIssueKey = key
			view.JiraIssueURL = jiraBrowseURL(coords.JiraBaseURL, key)
		}

		view.Status = commitStatus(c, issueMap)

		views = append(views, view)
	}

	return views
}

// commitStatus returns the status text for a commit's primary issue, or a
// placeholder when there is no issue ("No Issue") or it could not be loaded
// ("Unknown").
func commitStatus(c commit.Commit, issueMap map[string]issue.Issue) string {
	key := c.PrimaryIssueID()
	if key == "" {
		return "No Issue"
	}

	found, ok := issueMap[key]
	if !ok || found.Status == "" {
		return unknownValue
	}

	return found.Status
}

// baseCommitView fills the display fields common to every commit.
func baseCommitView(coords Coordinates, c commit.Commit) CommitView {
	// Convert Jira keys before PR references. The PR conversion inserts a GitHub
	// URL whose path can contain a "name-123" segment (e.g. a repo named
	// "widget-2"); running the Jira conversion afterwards would match that
	// segment and rewrite it a second time, corrupting the link.
	topic := convertJiraReferences(c.Topic, coords)
	topic = convertPRReferences(topic, coords)

	return CommitView{
		Hash:           c.Hash,
		URL:            commitURL(coords.GithubBaseURL, c.CanonicalHash),
		FormattedTopic: escapeTablePipes(topic),
		Authors:        formatAuthors(c.Authors),
	}
}

// commitURL builds the GitHub link to a commit, or "" when no GitHub base URL is
// configured — the renderer then shows the hash as plain text rather than a
// broken link.
func commitURL(githubBaseURL, canonicalHash string) string {
	if githubBaseURL == "" {
		return ""
	}

	return fmt.Sprintf("%s/commit/%s", githubBaseURL, canonicalHash)
}

// escapeTablePipes escapes any "|" so a commit subject cannot break out of its
// Markdown table cell. The link syntax inserted by the reference conversions
// never contains a pipe, so only literal pipes from the original subject are
// affected.
func escapeTablePipes(topic string) string {
	return strings.ReplaceAll(topic, "|", `\|`)
}

// buildSummary produces the optional release-summary section. It returns nil
// only when there is nothing to show at all — no release issues and no reverted,
// reapplied, or excluded commits — so the template omits the whole section. In
// particular, excluded commits must still be reported (the "Excluded commits"
// callout keeps the notes auditable) even when the range carries no issue keys.
func (b *Builder) buildSummary(
	coords Coordinates,
	normalCommits []commit.Commit,
	releaseIssueIDs []string,
	issueMap map[string]issue.Issue,
	revertedCommits []commit.Commit,
	reappliedCommits []commit.Commit,
	excludedCommits []commit.Commit,
) *SummaryData {
	// When releaseIssueIDs is empty here, normalCommits carry no keys either (Build
	// falls back to their keys), so the issue loops below produce nothing and the
	// summary reflects only the reverted/reapplied/excluded callouts.
	commitKeys := toSet(uniqueSortedJiraKeys(normalCommits))
	releaseSet := toSet(releaseIssueIDs)

	var inCommits, missing []IssueView

	for _, key := range releaseIssueIDs {
		found, hasIssue := issueMap[key]
		view := issueView(coords, key, found, hasIssue, b.checked)

		if commitKeys[key] {
			inCommits = append(inCommits, view)
		} else {
			missing = append(missing, view)
		}
	}

	var extra []IssueView

	for _, key := range sortedKeys(commitKeys) {
		if !releaseSet[key] {
			found, hasIssue := issueMap[key]
			extra = append(extra, issueView(coords, key, found, hasIssue, b.checked))
		}
	}

	summary := &SummaryData{
		ByStatus:  groupByStatus(inCommits),
		Missing:   sortByIssueNumber(missing),
		Extra:     groupByStatus(extra),
		Reverted:  flatCommitViews(coords, revertedCommits),
		Reapplied: flatCommitViews(coords, reappliedCommits),
		Excluded:  flatCommitViews(coords, excludedCommits),
	}

	if summary.isEmpty() {
		return nil
	}

	return summary
}

// flatCommitViews prepares a plain list of commits (reverted or reapplied) for
// the summary.
func flatCommitViews(coords Coordinates, commits []commit.Commit) []CommitView {
	views := make([]CommitView, 0, len(commits))
	for _, c := range commits {
		views = append(views, baseCommitView(coords, c))
	}

	return views
}

// --- pure helper functions (no dependencies, easy to test) ---

// splitExcluded separates the commits the matcher excludes from the rest,
// preserving order. It runs before splitReverted, so a commit the caller
// excludes is reported only under "Excluded commits" even if it is also a revert
// or reapply.
func splitExcluded(commits []commit.Commit, matcher CommitMatcher) (kept, excluded []commit.Commit) {
	for _, c := range commits {
		if matcher.Matches(c) {
			excluded = append(excluded, c)
		} else {
			kept = append(kept, c)
		}
	}

	return kept, excluded
}

// splitReverted separates revert commits from the rest, preserving order.
func splitReverted(commits []commit.Commit) (normal, reverted []commit.Commit) {
	for _, c := range commits {
		if c.IsRevert {
			reverted = append(reverted, c)
		} else {
			normal = append(normal, c)
		}
	}

	return normal, reverted
}

// selectReapplied returns the commits that reapply a previously reverted change,
// preserving order. Unlike splitReverted, it does not remove them from the
// input: a reapply re-ships its change, so it still belongs in the commit
// history and the issue summary — it is only additionally flagged in its own
// section.
func selectReapplied(commits []commit.Commit) []commit.Commit {
	var reapplied []commit.Commit

	for _, c := range commits {
		if c.IsReapply {
			reapplied = append(reapplied, c)
		}
	}

	return reapplied
}

// issueView builds the display data for one issue. The checked matcher marks the
// issue's status as a completed checklist item.
func issueView(coords Coordinates, key string, found issue.Issue, hasIssue bool, checked StatusMatcher) IssueView {
	title := unknownValue
	if hasIssue && found.Title != "" {
		title = found.Title
	}

	status := unknownValue
	if hasIssue && found.Status != "" {
		status = found.Status
	}

	return IssueView{
		Key:     key,
		Title:   title,
		URL:     jiraBrowseURL(coords.JiraBaseURL, key),
		Status:  status,
		Checked: checked.Matches(status),
	}
}

// groupByStatus groups issues by status, sorts the groups by status name, and
// sorts the issues inside each group by issue number.
func groupByStatus(issues []IssueView) []StatusGroup {
	grouped := map[string][]IssueView{}
	for _, view := range issues {
		grouped[view.Status] = append(grouped[view.Status], view)
	}

	statuses := make([]string, 0, len(grouped))
	for status := range grouped {
		statuses = append(statuses, status)
	}

	sort.Strings(statuses)

	groups := make([]StatusGroup, 0, len(statuses))
	for _, status := range statuses {
		groups = append(groups, StatusGroup{
			Status: status,
			Issues: sortByIssueNumber(grouped[status]),
		})
	}

	return groups
}

// issueNumberPattern captures the numeric part of a key such as "CX-123".
var issueNumberPattern = regexp.MustCompile(`-(\d+)$`)

// sortByIssueNumber returns the issues ordered by their trailing number. The
// sort is stable, so issues sharing a number keep their original order.
func sortByIssueNumber(issues []IssueView) []IssueView {
	sorted := append([]IssueView(nil), issues...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return issueNumber(sorted[i].Key) < issueNumber(sorted[j].Key)
	})

	return sorted
}

// issueNumber extracts the number from an issue key, or 0 if there is none.
func issueNumber(key string) int {
	match := issueNumberPattern.FindStringSubmatch(key)
	if match == nil {
		return 0
	}

	number, _ := strconv.Atoi(match[1])

	return number
}

// collectUniqueAuthors returns every author across all commits, de-duplicated
// and sorted case-insensitively.
func collectUniqueAuthors(commits []commit.Commit) []string {
	seen := map[string]bool{}

	var names []string

	for _, c := range commits {
		for _, author := range c.Authors {
			if !seen[author] {
				seen[author] = true
				names = append(names, author)
			}
		}
	}

	sortCaseInsensitive(names)

	return names
}

// formatAuthors renders author names as comma-separated inline code, sorted
// case-insensitively. With no authors it returns `Unknown`.
func formatAuthors(authors []string) string {
	if len(authors) == 0 {
		return "`Unknown`"
	}

	sorted := append([]string(nil), authors...)
	sortCaseInsensitive(sorted)

	quoted := make([]string, len(sorted))
	for i, name := range sorted {
		quoted[i] = "`" + name + "`"
	}

	return strings.Join(quoted, ", ")
}

// prReferencePattern matches a pull-request reference like "(#123)".
var prReferencePattern = regexp.MustCompile(`\(#(\d+)\)`)

// convertPRReferences turns "(#123)" into a Markdown link to the GitHub PR.
// Without a GitHub base URL there is nowhere to link, so the reference is left
// as written rather than turned into a broken relative link.
func convertPRReferences(topic string, coords Coordinates) string {
	if coords.GithubBaseURL == "" {
		return topic
	}

	return prReferencePattern.ReplaceAllStringFunc(topic, func(match string) string {
		number := prReferencePattern.FindStringSubmatch(match)[1]

		return fmt.Sprintf("([#%s](%s/pull/%s))", number, coords.GithubBaseURL, number)
	})
}

// jiraReferencePattern matches an issue key like "CX-123" on word boundaries.
var jiraReferencePattern = regexp.MustCompile(`\b([A-Za-z]{2,}-[0-9]+)\b`)

// convertJiraReferences turns "CX-123" into a Markdown link to the issue.
func convertJiraReferences(topic string, coords Coordinates) string {
	return jiraReferencePattern.ReplaceAllStringFunc(topic, func(key string) string {
		return fmt.Sprintf("[%s](%s)", key, jiraBrowseURL(coords.JiraBaseURL, key))
	})
}

// jiraBrowseURL builds the "browse" URL for an issue key. The absolute
// "/browse/" path replaces whatever path the base URL had, keeping only the
// scheme and host.
func jiraBrowseURL(baseURL, key string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return baseURL + "/browse/" + key
	}

	return parsed.Scheme + "://" + parsed.Host + "/browse/" + key
}

// uniqueSortedJiraKeys returns every issue key across the commits, de-duplicated
// and sorted lexicographically.
func uniqueSortedJiraKeys(commits []commit.Commit) []string {
	seen := map[string]bool{}

	var keys []string

	for _, c := range commits {
		for _, key := range c.JiraIssueIDs {
			if !seen[key] {
				seen[key] = true
				keys = append(keys, key)
			}
		}
	}

	sort.Strings(keys)

	return keys
}

// indexByKey builds a lookup map from issue key to issue.
func indexByKey(issues []issue.Issue) map[string]issue.Issue {
	byKey := make(map[string]issue.Issue, len(issues))
	for _, found := range issues {
		byKey[found.Key] = found
	}

	return byKey
}

// union concatenates two key lists, removing duplicates while keeping order.
func union(first, second []string) []string {
	seen := map[string]bool{}

	var result []string

	for _, key := range append(append([]string(nil), first...), second...) {
		if !seen[key] {
			seen[key] = true
			result = append(result, key)
		}
	}

	return result
}

// toSet turns a slice into a set for fast membership checks.
func toSet(keys []string) map[string]bool {
	set := make(map[string]bool, len(keys))
	for _, key := range keys {
		set[key] = true
	}

	return set
}

// sortedKeys returns a set's members in lexicographic order.
func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}

// sortCaseInsensitive sorts names ignoring case, in place.
func sortCaseInsensitive(names []string) {
	sort.SliceStable(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
}

// nowISO formats the current local time as an ISO-8601 timestamp to the
// minute: "2006-01-02T15:04". Sub-minute precision is noise in release notes.
func nowISO() string {
	return time.Now().Format("2006-01-02T15:04")
}
