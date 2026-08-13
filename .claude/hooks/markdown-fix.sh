#!/bin/sh
# PostToolUse hook: auto-format Markdown after Claude edits it.
#
# Claude Code pipes the tool call as JSON on stdin; we pull the edited file out
# of it, and if it is Markdown, rewrite it in place with markdownlint-cli2 --fix.
#
# Exit code 2 is the one Claude Code feeds back into the model, so every failure
# path below writes a plain-English instruction to stderr and exits 2 — that is
# how a missing tool or an unfixable rule reaches the user instead of vanishing.
set -eu

# jq is needed before anything else: without it we cannot even learn which file
# was edited.
command -v jq >/dev/null || {
	echo 'markdown-lint hook is broken: jq is not installed, so the hook cannot read the edited file path. Stop and tell the user to run: brew install jq' >&2
	exit 2
}

file=$(jq -r '.tool_input.file_path // empty')

case "$file" in
*.md | *.mdx) ;;
*) exit 0 ;; # Not Markdown — nothing to do.
esac

command -v markdownlint-cli2 >/dev/null || {
	echo 'markdown-lint hook is broken: markdownlint-cli2 is not installed, so Markdown was left unformatted. Stop and tell the user to run: brew install markdownlint-cli2' >&2
	exit 2
}

# --no-globs restricts the run to this one file: without it markdownlint-cli2
# merges the "**/*.md" glob from .markdownlint-cli2.yaml with our argument and
# rewrites every Markdown file in the repo on every single edit. The config's
# "ignores" list still applies, so .remember/ and testdata/ stay untouched.
# Whatever --fix could not repair goes back to Claude to fix by hand.
output=$(markdownlint-cli2 --fix --no-globs "$file" 2>&1) || {
	echo "$output" >&2
	exit 2
}
