package convergence

import (
	"fmt"

	"github.com/compozy/compozy/internal/core/agent"
)

// EnsureReadOnlyReviewers validates that the frozen review route's primary and
// fallback adapters can declare and enforce the read-only review capability. It
// is a preflight gate: an unsupported reviewer route fails here, before any
// model or verification phase starts, mapped to ErrReadOnlyUnsupported so the
// transport layer can surface a stable code.
//
// The fallback is validated with the same rule as the primary because a fallback
// may run before a phase starts; a route that cannot guarantee read-only must
// never become a reviewer, even as a substitute.
func (f FrozenConfiguration) EnsureReadOnlyReviewers() error {
	if err := ensureReadOnlyReviewer(f.Review.Primary.IDE); err != nil {
		return err
	}
	if f.Review.HasFallback && f.Review.Fallback != nil {
		if err := ensureReadOnlyReviewer(f.Review.Fallback.IDE); err != nil {
			return err
		}
	}
	return nil
}

func ensureReadOnlyReviewer(ide string) error {
	if err := agent.EnsureReadOnlyReviewer(ide); err != nil {
		return fmt.Errorf("%w: reviewer runtime %q cannot enforce read-only: %v", ErrReadOnlyUnsupported, ide, err)
	}
	return nil
}
