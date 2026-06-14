package git

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
)

// schemePrefix matches the "scheme://" at the start of a URL (https://, ssh://,
// git://, …). We use it to tell a real URL from the scp-like SSH shorthand
// ("git@host:org/repo.git"), which has no scheme.
var schemePrefix = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://`)

// validHost matches a plausible hostname or SSH-config alias: it must start with
// an alphanumeric character, which crucially rejects anything beginning with
// "-". That guard prevents a crafted remote URL from smuggling a value like
// "-oProxyCommand=…" into the `ssh -G` argument list (argv flag injection).
var validHost = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// InferRemoteBaseURL inspects the repository's git remote and derives the web
// base URL from it (e.g. https://github.com/acme/widgets).
//
// It is best-effort: when there is no remote, or its URL cannot be parsed, it
// returns an empty string (and never an error). This lets the tool work with
// zero configuration in the common case while still allowing the user to
// override it with --github-repo or SHIPNOTES_GITHUB_REPO.
func InferRemoteBaseURL(ctx context.Context, dir string) string {
	raw := remoteURL(ctx, dir)
	if raw == "" {
		return ""
	}

	host, org, repo, isSSH, ok := parseRemoteURL(raw)
	if !ok {
		return ""
	}

	// A custom SSH host (e.g. "github-work" defined in ~/.ssh/config) is an
	// alias, not a real hostname, so it would produce a broken web URL. Resolve
	// it the way git itself does — by asking ssh — but only for SSH remotes,
	// since HTTPS remotes already carry the real host.
	if isSSH {
		host = resolveSSHHost(ctx, host)
	}

	return fmt.Sprintf("https://%s/%s/%s", host, org, repo)
}

// ParseGithubSpec turns a user-supplied repository specification into a web base
// URL. It accepts the same shapes a git remote uses plus a bare "org/repo"
// shorthand:
//
//	https://github.com/org/repo[.git]   (and http://, git://)
//	git@github.com:org/repo.git         (the scp-like SSH shorthand)
//	ssh://git@github.com/org/repo.git
//	org/repo                            (shorthand; assumed to live on github.com)
//
// It returns ok=false when the value matches none of these. Unlike
// InferRemoteBaseURL it does not resolve SSH host aliases (there is no ambient
// repository to ask about); a value that needs that should be given as a full
// URL instead.
func ParseGithubSpec(spec string) (baseURL string, ok bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", false
	}

	// Bare "org/repo": no scheme and no host (a ":" would mean scp-like syntax).
	// We assume github.com, since the tool builds GitHub-style link paths. Unlike
	// a full URL — where extra path segments (e.g. "/tree/main") are expected and
	// trimmed — the shorthand must be *exactly* two segments: an explicit value is
	// the user naming a repo, so "org/repo/extra" is rejected rather than silently
	// reduced to "org/extra".
	if !schemePrefix.MatchString(spec) && !strings.Contains(spec, ":") {
		segments := strings.Split(strings.TrimSuffix(strings.Trim(spec, "/"), ".git"), "/")
		if len(segments) != 2 || segments[0] == "" || segments[1] == "" {
			return "", false
		}

		return fmt.Sprintf("https://github.com/%s/%s", segments[0], segments[1]), true
	}

	host, org, repo, _, found := parseRemoteURL(spec)
	if !found {
		return "", false
	}

	return fmt.Sprintf("https://%s/%s/%s", host, org, repo), true
}

// remoteURL returns the fetch URL of the repository's "origin" remote, falling
// back to "upstream". It returns an empty string when neither exists.
func remoteURL(ctx context.Context, dir string) string {
	for _, remote := range []string{"origin", "upstream"} {
		//nolint:gosec // G204: fixed git subcommand; the remote name is a hardcoded literal.
		cmd := exec.CommandContext(ctx, "git", "remote", "get-url", remote)
		cmd.Dir = dir

		out, err := cmd.Output()
		if err != nil {
			continue
		}

		if trimmed := strings.TrimSpace(string(out)); trimmed != "" {
			return trimmed
		}
	}

	return ""
}

// parseRemoteURL pulls the host, organization, and repository name out of a git
// remote URL. It understands the three shapes git uses:
//
//	https://host/org/repo.git          (and http://, git://)
//	ssh://git@host/org/repo.git
//	git@host:org/repo.git              (the scp-like SSH shorthand)
//
// isSSH reports whether the remote uses SSH, so the caller knows whether the
// host might be a ~/.ssh/config alias that needs resolving. ok is false when the
// URL does not contain at least "org/repo".
func parseRemoteURL(raw string) (host, org, repo string, isSSH, ok bool) {
	raw = strings.TrimSpace(raw)

	var hostPart, pathPart string

	switch {
	case schemePrefix.MatchString(raw):
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Hostname() == "" {
			return "", "", "", false, false
		}

		hostPart = parsed.Hostname()
		pathPart = parsed.Path
		isSSH = parsed.Scheme == "ssh"

	case strings.Contains(raw, ":"):
		// scp-like shorthand: [user@]host:path. Drop an optional "user@" prefix,
		// then split on the first colon into host and path.
		rest := raw
		if at := strings.LastIndex(rest, "@"); at >= 0 {
			rest = rest[at+1:]
		}

		var found bool

		hostPart, pathPart, found = strings.Cut(rest, ":")
		if !found {
			return "", "", "", false, false
		}

		isSSH = true

	default:
		return "", "", "", false, false
	}

	// Reject a host that is empty or not a plausible hostname/alias. This keeps a
	// crafted remote (e.g. one whose "host" is "-oProxyCommand=…") from reaching
	// `ssh -G` as a smuggled flag, and out of the constructed web URL.
	if !validHost.MatchString(hostPart) {
		return "", "", "", false, false
	}

	org, repo, ok = splitOrgRepo(pathPart)
	if !ok {
		return "", "", "", false, false
	}

	return hostPart, org, repo, isSSH, true
}

// splitOrgRepo extracts the organization (first path segment) and repository
// name (last path segment, minus a trailing ".git") from a remote URL path.
// It returns ok=false when the path has fewer than two segments.
func splitOrgRepo(path string) (org, repo string, ok bool) {
	path = strings.Trim(path, "/")
	path = strings.TrimSuffix(path, ".git")

	if path == "" {
		return "", "", false
	}

	const minPathSegments = 2 // We need at least "org/repo".

	segments := strings.Split(path, "/")
	if len(segments) < minPathSegments {
		return "", "", false
	}

	org = segments[0]
	repo = segments[len(segments)-1]

	if org == "" || repo == "" {
		return "", "", false
	}

	return org, repo, true
}

// resolveSSHHost turns a (possibly aliased) SSH host into its real hostname by
// running `ssh -G`, which prints the effective configuration — including any
// HostName from ~/.ssh/config — without making a network connection. If ssh is
// unavailable or fails, it returns the host unchanged so behavior degrades
// gracefully.
func resolveSSHHost(ctx context.Context, host string) string {
	//nolint:gosec // G204: host comes from the repo's own remote; `ssh -G` only prints config, it does not connect.
	cmd := exec.CommandContext(ctx, "ssh", "-G", host)

	out, err := cmd.Output()
	if err != nil {
		return host
	}

	// `ssh -G` prints one "key value" pair per line; we want "hostname".
	for line := range strings.SplitSeq(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "hostname" {
			return fields[1]
		}
	}

	return host
}
