package convergence

import (
	"errors"
	"testing"

	"github.com/compozy/compozy/internal/core/model"
)

// TestEnsureReadOnlyReviewers verifies the preflight rejects any reviewer route
// whose primary or fallback adapter cannot enforce read-only, before model work.
func TestEnsureReadOnlyReviewers(t *testing.T) {
	t.Parallel()

	t.Run("Should accept a declared read-only primary and fallback", func(t *testing.T) {
		t.Parallel()
		frozen := FrozenConfiguration{Review: ResolvedRoute{
			Role:        RoleReview,
			Primary:     Route{IDE: model.IDECodex},
			Fallback:    &Route{IDE: model.IDEClaude},
			HasFallback: true,
		}}
		if err := frozen.EnsureReadOnlyReviewers(); err != nil {
			t.Fatalf("EnsureReadOnlyReviewers() = %v, want nil", err)
		}
	})

	t.Run("Should reject an unsupported primary reviewer", func(t *testing.T) {
		t.Parallel()
		frozen := FrozenConfiguration{Review: ResolvedRoute{
			Role:    RoleReview,
			Primary: Route{IDE: "not-a-real-runtime"},
		}}
		if err := frozen.EnsureReadOnlyReviewers(); !errors.Is(err, ErrReadOnlyUnsupported) {
			t.Fatalf("EnsureReadOnlyReviewers() = %v, want ErrReadOnlyUnsupported", err)
		}
	})

	t.Run("Should reject an unsupported fallback reviewer", func(t *testing.T) {
		t.Parallel()
		frozen := FrozenConfiguration{Review: ResolvedRoute{
			Role:        RoleReview,
			Primary:     Route{IDE: model.IDECodex},
			Fallback:    &Route{IDE: "not-a-real-runtime"},
			HasFallback: true,
		}}
		if err := frozen.EnsureReadOnlyReviewers(); !errors.Is(err, ErrReadOnlyUnsupported) {
			t.Fatalf("EnsureReadOnlyReviewers() = %v, want ErrReadOnlyUnsupported", err)
		}
	})
}
