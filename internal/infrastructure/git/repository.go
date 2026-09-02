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
	"sync"

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

	rangeStart := strings.TrimSpace(ref)

	// Three independent reads of the same repository: the range itself, what the
	// release branch says it cherry-picked, and the patch ids of the range. None
	// needs another's answer, and the last two walk history that can be long, so
	// they run together and the slowest one sets the wait.
	//
	// Nothing here touches the reporter — progress stays on this goroutine, so
	// the console is never written to from two places at once.
	var (
		wait        sync.WaitGroup
		output      string
		err         error
		released    map[string]bool
		patchGroups map[string][]string
	)

	wait.Go(func() {
		output, err = r.run(ctx,
			"-c", "color.ui=false",
			"--no-pager",
			"log",
			rangeStart+"..HEAD",
			"--pretty=format:commit %H%nAuthor: %an <%ae>%nDate: %ai%n%n%B%n---%n",
			"--date=iso",
		)
	})

	wait.Go(func() {
		released = r.releasedSources(ctx)
	})

	wait.Go(func() {
		patchGroups = r.patchIDs(ctx, rangeStart)
	})

	wait.Wait()

	if err != nil {
		r.reporter.Failure("✗ Failed to fetch commits")

		return nil, err
	}

	r.reporter.Success("✓ Commits fetched")

	commits := parse(output)
	markAlreadyShipped(commits, released)
	markDuplicates(commits, patchGroups)

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
// So the lookup runs the other way: releasedSources collects every commit the
// release ref claims to have copied, and anything in the range named there has
// already shipped.
func markAlreadyShipped(commits []commit.Commit, released map[string]bool) {
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

// markDuplicates flags the commits that land the *same* change twice inside the
// reported range.
//
// This is the other half of the workflow markAlreadyShipped serves. When a fix
// is applied to the release branch directly and the development branch is later
// merged in, both copies end up inside one range: the original that came over
// with the merge, and the copy that shipped ahead of it. The release-ref scan
// alone flags only the original — nothing names the copy — and one unflagged
// commit is enough to keep the whole issue in the release summary, so the issue
// surfaces as new work.
//
// Two kinds of evidence pair them up, and they catch different cases. A
// `cherry-pick -x` trailer naming another commit *in this range* is an explicit
// link that survives a pick which had to be adapted around a conflict. An
// identical patch id is content equality, which is the only thing left when the
// pick was made without `-x` (through a web UI, say).
//
// Every member of a matched pair is flagged, not all-but-one: the issue moves to
// "Already shipped" only when every one of its commits is flagged, so flagging a
// single side would leave the issue looking like new work. A genuinely new
// commit on the same issue is still unflagged and still holds it in the release.
func markDuplicates(commits []commit.Commit, patchGroups map[string][]string) {
	inRange := make(map[string]int, len(commits))
	for i := range commits {
		inRange[commits[i].CanonicalHash] = i
	}

	// The explicit link: a commit here says it is a copy of a commit also here.
	for i := range commits {
		for _, source := range commit.ExtractCherryPickSources(commits[i].Message) {
			if j, ok := inRange[source]; ok {
				commits[i].IsAlreadyShipped = true
				commits[j].IsAlreadyShipped = true
			}
		}
	}

	// Content equality: same patch, different hash.
	for _, hashes := range patchGroups {
		// A patch carried by a single commit is not a copy of anything.
		if len(hashes) == 1 {
			continue
		}

		for _, hash := range hashes {
			if i, ok := inRange[hash]; ok {
				commits[i].IsAlreadyShipped = true
			}
		}
	}
}

// patchIDs groups the commits in the range by the identity of the change they
// carry, as computed by `git patch-id`. The returned map is keyed by patch id;
// any entry holding more than one hash is the same change applied twice.
//
// Commits with no diff of their own — merges, empty commits — produce no patch
// id at all, so they can never be paired with anything.
//
// Like releasedSources, this fails open: any error yields nothing and the notes
// then keep every commit, because absent evidence is not evidence of a
// duplicate.
func (r *Repository) patchIDs(ctx context.Context, ref string) map[string][]string {
	output, err := r.runPiped(ctx,
		[]string{
			"-c", "color.ui=false",
			"--no-pager",
			"log",
			"--patch",
			"--format=commit %H",
			ref + "..HEAD",
		},
		[]string{"patch-id", "--stable"},
	)
	if err != nil {
		return nil
	}

	// Each line is "<patch id> <commit hash>".
	groups := map[string][]string{}

	for line := range strings.SplitSeq(output, "\n") {
		patchID, hash, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}

		groups[patchID] = append(groups[patchID], hash)
	}

	return groups
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

// runPiped executes `git <first...> | git <second...>` in the working directory
// and returns the second command's stdout.
//
// The output of the first command is streamed into the second rather than held
// in memory: the one caller pipes a full release diff, which can be large.
func (r *Repository) runPiped(ctx context.Context, first, second []string) (string, error) {
	//nolint:gosec // G204: the program is always "git"; the ref argument is format-validated before reaching here.
	producer := exec.CommandContext(ctx, "git", first...)
	producer.Dir = r.workingDir

	//nolint:gosec // G204: the program is always "git"; these arguments are literals.
	consumer := exec.CommandContext(ctx, "git", second...)
	consumer.Dir = r.workingDir

	pipe, err := producer.StdoutPipe()
	if err != nil {
		return "", err
	}

	consumer.Stdin = pipe

	var stdout, producerErr, consumerErr strings.Builder

	consumer.Stdout = &stdout
	producer.Stderr = &producerErr
	consumer.Stderr = &consumerErr

	if err := producer.Start(); err != nil {
		return "", err
	}

	if err := consumer.Run(); err != nil {
		// The producer still holds the pipe; releasing it keeps the process from
		// lingering when the consumer died early.
		_ = producer.Wait()

		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(consumerErr.String()))
	}

	if err := producer.Wait(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(producerErr.String()))
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
