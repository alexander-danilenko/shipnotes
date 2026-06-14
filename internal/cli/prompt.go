package cli

import (
	"bufio"
	"io"

	"github.com/alexander-danilenko/shipnotes/internal/domain/issue"
	"github.com/alexander-danilenko/shipnotes/internal/infrastructure/terminal"
)

// Prompt asks the user for the optional list of release issue IDs. It is the
// driving adapter behind the application's IssueIDProvider port: it reads from
// an input stream and validates with the issue domain.
type Prompt struct {
	console *terminal.Console
	in      io.Reader
}

// NewPrompt builds a Prompt that reads from in and reports through console.
func NewPrompt(console *terminal.Console, in io.Reader) *Prompt {
	return &Prompt{console: console, in: in}
}

// ForIssueIDs asks for the release issue IDs, re-asking until the answer is
// valid (or empty, which means "skip"). When input is not interactive (no more
// lines), it returns an empty list, so the tool still works in scripts and CI.
func (p *Prompt) ForIssueIDs() ([]string, error) {
	reader := bufio.NewReader(p.in)

	for {
		p.console.Plain("Enter Jira Issue IDs in current release to check (comma-separated, optional):")

		line, err := reader.ReadString('\n')

		keys, parseErr := issue.ParseIDs(line)
		if parseErr == nil {
			return keys, nil
		}

		// At end of input there is no chance to re-ask, so a final unparseable
		// (or absent) line is treated as "skip" rather than warning forever.
		if err != nil {
			return []string{}, nil //nolint:nilerr // EOF means "skip", which is a valid empty result, not an error.
		}

		p.console.Warn("Invalid Jira issue key format. Expected format: PROJECT-123")
	}
}
