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

// Repository runs git commands inside a specific repository directory. It
// implements commit.Repository.
type Repository struct {
	workingDir string
	reporter   report.Reporter
}

// New returns a Repository bound to the given repository directory.
func New(workingDir string, reporter report.Reporter) *Repository {
	return &Repository{workingDir: workingDir, reporter: reporter}
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
	r.markAlreadyShipped(ctx, commits, strings.TrimSpace(ref))

	return commits, nil
}

// markAlreadyShipped flags the commits in the range that re-land a change
// released earlier. A commit qualifies when it carries a `git cherry-pick -x`
// trailer naming a commit that already sits behind rangeStart.
//
// One trailer is enough. Cherry-picking an already cherry-picked commit appends
// a second trailer, one per hop, so a fix backported along a chain of
// maintenance branches names both its original and the intermediate pick. The
// intermediate usually lives on a branch this range knows nothing about, so
// requiring every trailer to resolve would miss exactly the chained backports
// this feature exists to catch. Finding one released ancestor is proof; failing
// to resolve the others is only absence of proof.
func (r *Repository) markAlreadyShipped(ctx context.Context, commits []commit.Commit, rangeStart string) {
	for i := range commits {
		for _, source := range commit.ExtractCherryPickSources(commits[i].Message) {
			if r.isAncestor(ctx, source, rangeStart) {
				commits[i].IsAlreadyShipped = true

				break
			}
		}
	}
}

// isAncestor reports whether hash is already reachable from rangeStart.
//
// The hash is format-checked here, not just where it was parsed: it reaches the
// git command line from this function, and every argument this adapter passes
// to git is validated in the same place it is used.
//
// Any failure means "not an ancestor". git exits 1 when the commit is simply
// not behind rangeStart and 128 when it does not exist locally at all — a
// shallow clone or a pruned branch — and both answers are the same one here.
// Missing data is not evidence of a release, so the commit stays in the notes.
func (r *Repository) isAncestor(ctx context.Context, hash, rangeStart string) bool {
	if !isValidHashFormat(hash) {
		return false
	}

	_, err := r.run(ctx, "merge-base", "--is-ancestor", hash, rangeStart)

	return err == nil
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
