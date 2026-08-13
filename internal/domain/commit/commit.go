// Package commit is the commit domain: the Commit entity and the rules that
// interpret a commit's message (is it a revert, a reapply, which Jira issues
// does it reference). It is pure domain logic — it knows nothing about git, the
// command line, or how commits are read from disk; the infrastructure layer
// produces Commit values and the rest of the program reasons about them here.
package commit

import (
	"regexp"
	"strings"
)

// Commit is one git commit, reduced to the pieces the release notes need.
//
// The classification fields (IsRevert, IsReapply, JiraIssueIDs) are derived
// from the message by the rules in this package; an adapter that builds a
// Commit fills them with ExtractIssueIDs, IsRevertTopic and IsReapplyTopic so
// the rules live in one place.
type Commit struct {
	// CanonicalHash is the full 40-character commit hash.
	CanonicalHash string
	// Hash is the short 7-character hash shown in tables.
	Hash string
	// Authors are the unique author names (primary plus any co-authors),
	// kept in the order they were first seen.
	Authors []string
	// Topic is the first line of the commit message (the subject).
	Topic string
	// Message is the full commit message body.
	Message string
	// JiraIssueIDs are the Jira keys found in the topic (e.g. "PROJ-123"),
	// de-duplicated in first-seen order.
	JiraIssueIDs []string
	// IsRevert is true when the topic begins with "revert".
	IsRevert bool
	// IsReapply is true when the topic is a git "reapply" — the message git
	// writes when you revert a revert, shaped exactly as `Reapply "<subject>"`.
	IsReapply bool
	// IsAlreadyShipped is true when this commit re-lands a change that was
	// released earlier: its message carries a `git cherry-pick -x` trailer
	// naming a commit that already sits behind the start of the reported range.
	//
	// Unlike the other classification fields this one cannot be decided from the
	// message alone — answering "is that commit already behind the range start?"
	// needs git — so the adapter fills it after reading the log, using
	// ExtractCherryPickSources to find the candidates.
	IsAlreadyShipped bool
}

// PrimaryIssueID returns the commit's main Jira issue key — the first one found
// in the topic — or an empty string when the commit references no issue.
func (c Commit) PrimaryIssueID() string {
	if len(c.JiraIssueIDs) == 0 {
		return ""
	}

	return c.JiraIssueIDs[0]
}

// jiraKeyPattern matches a Jira issue key such as "PROJ-123": two or more letters,
// a dash, then digits.
var jiraKeyPattern = regexp.MustCompile(`[A-Za-z]{2,}-[0-9]+`)

// ExtractIssueIDs finds the Jira issue keys in text, de-duplicated in first-seen
// order. It always returns a non-nil slice so callers can range over it freely.
func ExtractIssueIDs(text string) []string {
	keys := []string{}
	if text == "" {
		return keys
	}

	seen := map[string]bool{}
	for _, key := range jiraKeyPattern.FindAllString(text, -1) {
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}

	return keys
}

// cherryPickSourcePattern matches the trailer `git cherry-pick -x` appends to a
// message: `(cherry picked from commit <hash>)`.
//
// The hash capture is deliberately strict — 7 to 40 lowercase hex characters,
// exactly the shape git itself writes. The value comes from commit-message
// text, which anyone who can land a commit controls, and it is later handed to
// the git command line; refusing to recognise anything else keeps a malformed
// or hostile "hash" from ever reaching that boundary.
var cherryPickSourcePattern = regexp.MustCompile(`\(cherry picked from commit ([0-9a-f]{7,40})\)`)

// ExtractCherryPickSources returns the commits a message says it was
// cherry-picked from, de-duplicated in first-seen order. Only git's own `-x`
// trailer is recognised, so a message written without `-x` yields nothing. It
// always returns a non-nil slice so callers can range over it freely.
//
// A message can carry more than one trailer: cherry-picking an already
// cherry-picked commit appends a second line, one per hop.
func ExtractCherryPickSources(message string) []string {
	hashes := []string{}
	if message == "" {
		return hashes
	}

	seen := map[string]bool{}

	for _, match := range cherryPickSourcePattern.FindAllStringSubmatch(message, -1) {
		hash := match[1]
		if !seen[hash] {
			seen[hash] = true
			hashes = append(hashes, hash)
		}
	}

	return hashes
}

// IsRevertTopic reports whether a commit topic is a revert. Git's generated
// message starts with `Revert "…"`, but people also hand-write "revert: …", so
// the rule is a deliberately loose case-insensitive "revert" prefix.
func IsRevertTopic(topic string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(topic)), "revert")
}

// IsReapplyTopic reports whether a topic is git's "reapply" message — what git
// writes when you revert a revert. The shape is always `Reapply "<subject>"`,
// so we require both the opening `Reapply "` and a closing quote. Unlike the
// loose revert match, this is deliberately exact: only git's generated form
// should be flagged.
func IsReapplyTopic(topic string) bool {
	trimmed := strings.TrimSpace(topic)

	return strings.HasPrefix(trimmed, `Reapply "`) && strings.HasSuffix(trimmed, `"`)
}
