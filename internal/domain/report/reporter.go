// Package report defines the progress-reporting port. Services in the domain and
// adapters in the infrastructure layer announce what they are doing through this
// small interface instead of writing to the screen directly, so the core never
// depends on a concrete terminal. The terminal adapter implements it; tests pass
// a no-op.
package report

// Reporter receives short, human-readable progress messages. The terminal
// adapter colorizes them; a test reporter can discard them.
type Reporter interface {
	// Status announces work in progress (e.g. "Fetching commits...").
	Status(message string)
	// Success announces a completed step (e.g. "✓ Commits fetched").
	Success(message string)
	// Failure announces a failed step (e.g. "✗ Failed to fetch commits").
	Failure(message string)
	// Warn announces a non-fatal problem worth the user's attention.
	Warn(message string)
	// Dim prints a muted, secondary detail.
	Dim(message string)
}
