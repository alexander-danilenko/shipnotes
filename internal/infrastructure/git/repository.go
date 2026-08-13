// Package git runs git commands and turns their output into domain commits. It
// is the adapter that implements the commit.Repository port: validating refs,
// reading the log, and parsing the raw text into commit.Commit values. It also
// infers repository coordinates from the git remote (see remote.go).
package git

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/alexander-danilenko/shipnotes/internal/domain/commit"
	"github.com/alexander-danilenko/shipnotes/internal/domain/report"
)

// validHashFormat accepts the references the tool understands: HEAD, HEAD~N, a
// 7-40 character hex hash, or a hex hash with a ~N suffix. Validating the format
// before passing it to git prevents command injection.
var validHashFormat = regexp.MustCompile(`^(HEAD|HEAD~\d+|[a-fA-F0-9]{7,40}(?:~\d+)?)$`)

// validRefName accepts the shapes a branch or tag name can take — "master",
// "origin/main", "release/2.1", "v1.0.0". It must begin with an alphanumeric so
// the value can never be read by git as an option rather than a ref.
var validRefName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

// IsValidRefName reports whether ref is safe to pass to git as a branch or tag
// name. The cli uses it to reject an explicit --shipped-ref typo up front.
func IsValidRefName(ref string) bool {
	return validRefName.MatchString(ref)
}

// Repository runs git commands inside a specific repository directory. It
// implements commit.Repository.
type Repository struct {
	workingDir string
	reporter   report.Reporter
	// shippedRef names the branch or tag that holds already-released work, e.g.
	// "origin/main". Commits it says it cherry-picked are marked as already
	// shipped. Empty turns that detection off.
	shippedRef string
}

// New returns a Repository bound to the given repository directory. shippedRef
// is the already-released branch or tag (see the field); pass "" to disable
// already-shipped detection.
func New(workingDir string, reporter report.Reporter, shippedRef string) *Repository {
	return &Repository{workingDir: workingDir, reporter: reporter, shippedRef: shippedRef}
}

// isValidHashFormat reports whether ref has an acceptable shape. It does not
// check whether the commit actually exists — Validate does that.
func isValidHashFormat(ref string) bool {
	return validHashFormat.MatchString(ref)
}

// Validate checks that ref exists in the repository.
//
// It returns an error only when the format is invalid (a programming/usage
// mistake). When the format is fine but the commit is simply not found, it
// returns (false, nil) so the caller can show a friendly message.
func (r *Repository) Validate(ctx context.Context, ref string) (bool, error) {
	if !isValidHashFormat(ref) {
		return false, fmt.Errorf(
			"invalid commit hash format: %s. Must be HEAD, HEAD~N, or 7-40 hex characters", ref,
		)
	}

	r.reporter.Status("Validating commit...")

	_, err := r.run(ctx, "rev-parse", "--verify", "--quiet", ref)
	if err != nil {
		r.reporter.Failure("✗ Invalid commit")

		if isNotARepoError(err) {
			r.reporter.Warn(fmt.Sprintf(
				"⚠️  Make sure you're running this command from a git repository (current dir: %s)",
				r.workingDir,
			))
		}

		return false, nil
	}

	r.reporter.Success("✓ Valid commit")

	return true, nil
}

// Log returns the commits from ref (exclusive) up to HEAD, newest first. It runs
// git in a stable, easy-to-parse format and turns that output into domain
// commits.
func (r *Repository) Log(ctx context.Context, ref string) ([]commit.Commit, error) {
	if !isValidHashFormat(strings.TrimSpace(ref)) {
		return nil, fmt.Errorf(
			"invalid commit hash format: %s. Must be HEAD, HEAD~N, or 7-40 hex characters", ref,
		)
	}

	r.reporter.Status("Fetching commits...")

	output, err := r.run(ctx,
		"-c", "color.ui=false",
		"--no-pager",
		"log",
		strings.TrimSpace(ref)+"..HEAD",
		"--pretty=format:commit %H%nAuthor: %an <%ae>%nDate: %ai%n%n%B%n---%n",
		"--date=iso",
	)
	if err != nil {
		r.reporter.Failure("✗ Failed to fetch commits")

		return nil, err
	}

	r.reporter.Success("✓ Commits fetched")

	commits := parse(output)
	r.markAlreadyShipped(ctx, commits)

	return commits, nil
}

// markAlreadyShipped flags the commits in the range whose change has already
// been released.
//
// The evidence is on the release branch, not in the range. When a fix is
// cherry-picked out to a release branch, git writes the `cherry picked from
// commit …` trailer on the *copy*, naming the original — so the commit that
// later arrives in this range is the original and carries no trailer at all.
// Reading the range alone can never recognise it; the only record of the
// release lives in what the release branch says it took.
//
// So the lookup runs the other way: one pass over the release ref collects
// every commit it claims to have copied, and anything in the range named there
// has already shipped.
func (r *Repository) markAlreadyShipped(ctx context.Context, commits []commit.Commit) {
	released := r.releasedSources(ctx)
	if len(released) == 0 {
		return
	}

	for i := range commits {
		if released[commits[i].CanonicalHash] {
			commits[i].IsAlreadyShipped = true
		}
	}
}

// releasedSources returns the commits the release ref says it already carries a
// copy of, read from the cherry-pick trailers in its history.
//
// The ref is format-checked here, where it reaches the git command line, so the
// adapter never passes git an argument it has not validated itself.
//
// No ref, an unreadable ref, or a history with no trailers all yield nothing,
// and the notes then keep every commit: absent evidence is not evidence of a
// release. Scanning the whole ref rather than a bounded slice of it is
// deliberate — a copy can be older than the range it was taken from, so any
// cutoff risks missing one.
func (r *Repository) releasedSources(ctx context.Context) map[string]bool {
	if !IsValidRefName(r.shippedRef) {
		return nil
	}

	output, err := r.run(ctx,
		"-c", "color.ui=false",
		"--no-pager",
		"log",
		"--grep=cherry picked from commit",
		"--format=%B",
		r.shippedRef,
	)
	if err != nil {
		return nil
	}

	sources := map[string]bool{}
	for _, hash := range commit.ExtractCherryPickSources(output) {
		sources[hash] = true
	}

	return sources
}

// run executes `git <args...>` in the working directory and returns stdout.
// On failure it includes git's stderr in the error so problems are visible.
func (r *Repository) run(ctx context.Context, args ...string) (string, error) {
	//nolint:gosec // G204: the program is always "git"; the ref argument is format-validated before reaching here.
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.workingDir

	var stdout, stderr strings.Builder

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrText := strings.TrimSpace(stderr.String())
		if stderrText != "" {
			return "", fmt.Errorf("%w: %s", err, stderrText)
		}

		return "", err
	}

	return stdout.String(), nil
}

// isNotARepoError detects the "not a git repository" family of errors so we can
// offer a more helpful hint.
func isNotARepoError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())

	return strings.Contains(message, "not a git repository") || strings.Contains(message, "not found")
}
