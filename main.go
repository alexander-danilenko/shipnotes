// Command shipnotes generates a Markdown release notes file from git
// history, enriching each commit with the status of its linked Jira issue.
//
// Usage:
//
//	shipnotes <commit_hash> [-o output.md] [--repo-dir DIR] [--jql "key IN (ABC-1, ABC-2)"]
//
// It is a single, dependency-free binary: the only thing it needs at runtime is
// the git command and network access to the Jira REST API.
//
// The code is organized as a DDD / hexagonal architecture:
//
//   - internal/domain         — entities and ports (the core; no I/O)
//   - internal/application     — the use-case orchestration
//   - internal/infrastructure  — adapters: git, Jira, markdown, config, terminal
//   - internal/cli             — the command-line interface and composition root
package main

import (
	"os"

	"github.com/alexander-danilenko/shipnotes/internal/cli"
)

// version is the release identifier reported by `shipnotes --version`. It is
// "dev" for local builds and overwritten at release time by the linker
// (`-ldflags "-X main.version=..."`), which is how GoReleaser stamps the git
// tag into the published binaries.
var version = "dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], version))
}
