package git

import (
	"regexp"
	"strings"

	"github.com/alexander-danilenko/shipnotes/internal/domain/commit"
)

// Regular expressions used while parsing the raw git output. Compiling them once
// at startup keeps the parser fast and the intent of each pattern in one place.
// The rules for interpreting a commit (reverts, reapplies, issue keys) live in
// the commit domain, not here — this file only deals with git's text format.
var (
	// A line that begins a new commit block, e.g. "commit a1b2c3...".
	commitHeaderLine = regexp.MustCompile(`^commit [0-9a-f]{40}$`)
	// Captures the full 40-character hash from a commit block.
	commitHashPattern = regexp.MustCompile(`(?m)^commit ([0-9a-f]{40})`)
	// Captures everything after the blank line that follows the Date line:
	// that is the commit message body.
	commitMessagePattern = regexp.MustCompile(`(?m)^Date: .*\n\n([\s\S]*)`)
	// The primary author line.
	authorLinePattern = regexp.MustCompile(`(?m)^Author:.*$`)
	// Co-author trailer lines (case-insensitive, optionally indented).
	coAuthorLinePattern = regexp.MustCompile(`(?im)^[\t ]*Co-authored-by: .*$`)
	// Extracts the display name from an Author/Co-authored-by line, dropping the
	// trailing "<email>" part.
	authorNamePattern = regexp.MustCompile(`(?i)^(?:Author|Co-authored-by):\s*([^<]+?)(?:\s*<[^>]+>)?\s*$`)
)

// parse turns raw `git log` output into a slice of domain commits, in the order
// git produced them (newest first).
func parse(gitLog string) []commit.Commit {
	blocks := splitIntoCommitBlocks(gitLog)

	commits := make([]commit.Commit, 0, len(blocks))
	for _, block := range blocks {
		commits = append(commits, parseCommit(block))
	}

	return commits
}

// splitIntoCommitBlocks breaks the log into one chunk per commit. A new chunk
// starts at every "commit <hash>" header line. Blank chunks are dropped and
// each chunk is trimmed.
func splitIntoCommitBlocks(gitLog string) []string {
	var (
		blocks  []string
		current []string
	)

	flush := func() {
		if len(current) == 0 {
			return
		}

		block := strings.TrimSpace(strings.Join(current, "\n"))
		if block != "" {
			blocks = append(blocks, block)
		}

		current = nil
	}

	for line := range strings.SplitSeq(gitLog, "\n") {
		if commitHeaderLine.MatchString(line) {
			flush()
		}

		current = append(current, line)
	}

	flush()

	return blocks
}

// parseCommit extracts every field of interest from a single commit block,
// delegating the interpretation rules to the commit domain.
func parseCommit(block string) commit.Commit {
	canonical, short := parseHashes(block)
	message, topic := parseMessageAndTopic(block)

	return commit.Commit{
		CanonicalHash: canonical,
		Hash:          short,
		Authors:       parseAuthors(block),
		Topic:         topic,
		Message:       message,
		JiraIssueIDs:  commit.ExtractIssueIDs(topic),
		IsRevert:      commit.IsRevertTopic(topic),
		IsReapply:     commit.IsReapplyTopic(topic),
	}
}

// parseHashes returns the canonical (full) hash and the short 7-character hash.
func parseHashes(block string) (canonical, short string) {
	match := commitHashPattern.FindStringSubmatch(block)
	if match == nil {
		return "", ""
	}

	canonical = match[1]

	return canonical, canonical[:7]
}

// parseMessageAndTopic returns the full message body and its topic (first
// non-empty line).
func parseMessageAndTopic(block string) (message, topic string) {
	match := commitMessagePattern.FindStringSubmatch(block)
	if match == nil {
		return "", ""
	}

	message = strings.TrimSpace(match[1])

	// The git format string appends a literal "---" separator line after every
	// commit body. Strip it so a commit with an empty message does not surface
	// "---" as its topic (and so the separator never leaks into the body).
	message = strings.TrimSpace(strings.TrimSuffix(message, "---"))
	if message == "" {
		return "", ""
	}

	for line := range strings.SplitSeq(message, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return message, trimmed
		}
	}

	return message, ""
}

// parseAuthors collects unique author names from the primary Author line and
// any Co-authored-by trailers, preserving first-seen order.
func parseAuthors(block string) []string {
	var names []string

	seen := map[string]bool{}

	add := func(line string) {
		if name := parseAuthorName(line); name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}

	if primary := authorLinePattern.FindString(block); primary != "" {
		add(primary)
	}

	for _, coAuthor := range coAuthorLinePattern.FindAllString(block, -1) {
		add(coAuthor)
	}

	return names
}

// parseAuthorName pulls the display name out of an Author/Co-authored-by line.
func parseAuthorName(line string) string {
	match := authorNamePattern.FindStringSubmatch(strings.TrimSpace(line))
	if match == nil {
		return ""
	}

	return strings.TrimSpace(match[1])
}
