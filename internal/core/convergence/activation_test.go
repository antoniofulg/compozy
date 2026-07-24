package convergence

import (
	"errors"
	"testing"
)

func baseActivation() ActivationRequest {
	return ActivationRequest{
		WorkspaceID:      "ws-1",
		ExecutionScopeID: "TG-004",
		TargetSnapshot:   "8d92abcd",
		NormalizedIntent: "profile=quality;setup=balanced",
		SourceRunID:      "run-7",
	}
}

func mustActivationFingerprint(t *testing.T, r ActivationRequest) string {
	t.Helper()
	fingerprint, err := r.Fingerprint()
	if err != nil {
		t.Fatalf("activation fingerprint: %v", err)
	}
	return fingerprint
}

func TestActivationFingerprint(t *testing.T) {
	t.Parallel()
	t.Run("Should be identical for duplicate requests", func(t *testing.T) {
		t.Parallel()
		first := mustActivationFingerprint(t, baseActivation())
		second := mustActivationFingerprint(t, baseActivation())
		if first != second {
			t.Fatal("expected identical fingerprints for duplicate activation")
		}
	})
	t.Run("Should differ when an explicit nonce is supplied", func(t *testing.T) {
		t.Parallel()
		base := mustActivationFingerprint(t, baseActivation())
		withNonce := baseActivation()
		withNonce.Nonce = "new-run-1"
		if mustActivationFingerprint(t, withNonce) == base {
			t.Fatal("expected a distinct fingerprint for a new run nonce")
		}
	})
	t.Run("Should reject a missing required field", func(t *testing.T) {
		t.Parallel()
		invalid := baseActivation()
		invalid.TargetSnapshot = "  "
		if _, err := invalid.Fingerprint(); !errors.Is(err, ErrActivationInvalid) {
			t.Fatalf("expected ErrActivationInvalid, got %v", err)
		}
	})
	t.Run("Should require a nonce for a legal new run after terminal", func(t *testing.T) {
		t.Parallel()
		if err := RequireNonceForNewRun(true, ""); !errors.Is(err, ErrActivationInvalid) {
			t.Fatalf("expected new-run nonce requirement, got %v", err)
		}
		if err := RequireNonceForNewRun(true, "nonce"); err != nil {
			t.Fatalf("expected a nonced new run to be allowed, got %v", err)
		}
		if err := RequireNonceForNewRun(false, ""); err != nil {
			t.Fatalf("expected a first run to need no nonce, got %v", err)
		}
	})
}
