package commit

import "context"

// Repository is the port for reading commits from a version-control system. The
// domain defines what it needs; an adapter in the infrastructure layer (the git
// adapter) implements it. Tests can supply a fake.
type Repository interface {
	// Validate reports whether ref exists in the repository. It returns an error
	// only when ref is malformed (a usage mistake); a well-formed ref that simply
	// does not exist returns (false, nil) so the caller can show a friendly hint.
	Validate(ctx context.Context, ref string) (bool, error)
	// Log returns the commits from ref (exclusive) up to HEAD, newest first.
	Log(ctx context.Context, ref string) ([]Commit, error)
}
