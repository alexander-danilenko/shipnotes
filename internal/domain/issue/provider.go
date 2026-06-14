package issue

import "context"

// Provider is the port for loading issues by key. The domain defines it; the
// Jira adapter in the infrastructure layer implements it, and tests provide a
// fake. Depending on this small interface (rather than a concrete Jira client)
// keeps the shipnotes builder easy to test in isolation.
type Provider interface {
	LoadByKeys(ctx context.Context, keys []string) ([]Issue, error)
}
