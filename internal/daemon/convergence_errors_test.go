package daemon

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/compozy/compozy/internal/core/convergence"
)

// TestConvergenceProblemMapsStableCodes proves every convergence domain sentinel
// maps to its stable transport code and HTTP status, including when wrapped with
// %w context, and that non-convergence errors fall through to nil.
func TestConvergenceProblemMapsStableCodes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		err        error
		wantCode   string
		wantStatus int
	}{
		{"config invalid", convergence.ErrConfigInvalid, convergence.CodeConfigInvalid, http.StatusUnprocessableEntity},
		{
			"verification required",
			convergence.ErrVerificationRequired,
			convergence.CodeVerificationRequired,
			http.StatusUnprocessableEntity,
		},
		{
			"read only unsupported",
			convergence.ErrReadOnlyUnsupported,
			convergence.CodeReadOnlyUnsupported,
			http.StatusUnprocessableEntity,
		},
		{
			"target ineligible",
			convergence.ErrTargetIneligible,
			convergence.CodeTargetIneligible,
			http.StatusUnprocessableEntity,
		},
		{"already active", convergence.ErrAlreadyActive, convergence.CodeAlreadyActive, http.StatusConflict},
		{
			"fingerprint mismatch",
			convergence.ErrFingerprintMismatch,
			convergence.CodeFingerprintMismatch,
			http.StatusConflict,
		},
		{"not parked", convergence.ErrNotParked, convergence.CodeNotParked, http.StatusConflict},
		{
			"resume cursor stale",
			convergence.ErrResumeCursorStale,
			convergence.CodeResumeCursorStale,
			http.StatusConflict,
		},
		{"approval stale", convergence.ErrApprovalStale, convergence.CodeApprovalStale, http.StatusConflict},
		{"workspace changed", convergence.ErrWorkspaceChanged, convergence.CodeWorkspaceChanged, http.StatusConflict},
		{"unknown outcome", convergence.ErrUnknownOutcome, convergence.CodeUnknownOutcome, http.StatusConflict},
		{"integrity failed", convergence.ErrIntegrityFailed, convergence.CodeIntegrityFailed, http.StatusConflict},
	}
	for _, tc := range cases {
		t.Run("Should map "+tc.name, func(t *testing.T) {
			t.Parallel()
			wrapped := fmt.Errorf("convergence start rejected: %w", tc.err)
			problem := convergenceProblem(wrapped, nil)
			if problem == nil {
				t.Fatalf("expected a problem for %v", tc.err)
			}
			if problem.Code != tc.wantCode {
				t.Fatalf("code = %q, want %q", problem.Code, tc.wantCode)
			}
			if problem.Status != tc.wantStatus {
				t.Fatalf("status = %d, want %d", problem.Status, tc.wantStatus)
			}
			if !errors.Is(problem, tc.err) {
				t.Fatalf("problem should unwrap to the domain sentinel")
			}
		})
	}
	t.Run("Should return nil for a non-convergence error", func(t *testing.T) {
		t.Parallel()
		if problem := convergenceProblem(errors.New("some other failure"), nil); problem != nil {
			t.Fatalf("expected nil, got %+v", problem)
		}
		if problem := convergenceProblem(nil, nil); problem != nil {
			t.Fatalf("expected nil for nil error, got %+v", problem)
		}
	})
}

// TestConvergenceProblemRedactsDetails proves detail maps are reduced to the
// allow-listed, redacted subset: absolute host paths are replaced with a bounded
// marker and non-authorized keys (which could carry review prose or model output)
// are dropped before they reach the transport envelope.
func TestConvergenceProblemRedactsDetails(t *testing.T) {
	t.Parallel()
	t.Run("Should redact paths and drop unauthorized detail keys", func(t *testing.T) {
		t.Parallel()
		details := map[string]string{
			"config_path":  "/Users/dev/secret/workspace.toml",
			"field":        "convergence.verification.command",
			"finding_text": "the reviewer said the auth check is missing",
			"model_output": "here is my full chain of thought",
		}
		problem := convergenceProblem(
			fmt.Errorf("bad config: %w", convergence.ErrConfigInvalid),
			details,
		)
		if problem == nil {
			t.Fatalf("expected a problem")
		}
		if _, leaked := problem.Details["finding_text"]; leaked {
			t.Fatalf("finding prose leaked into transport details")
		}
		if _, leaked := problem.Details["model_output"]; leaked {
			t.Fatalf("model output leaked into transport details")
		}
		field, ok := problem.Details["field"].(string)
		if !ok || field != "convergence.verification.command" {
			t.Fatalf("expected the authorized field to survive redaction, got %v", problem.Details["field"])
		}
		configPath, ok := problem.Details["config_path"].(string)
		if !ok {
			t.Fatalf("expected config_path detail present")
		}
		if configPath != "[redacted-path]" {
			t.Fatalf("absolute path not redacted: %q", configPath)
		}
	})
	t.Run("Should omit details entirely when nothing survives redaction", func(t *testing.T) {
		t.Parallel()
		problem := convergenceProblem(
			convergence.ErrConfigInvalid,
			map[string]string{"finding_text": "prose", "model_output": "reasoning"},
		)
		if problem == nil {
			t.Fatalf("expected a problem")
		}
		if problem.Details != nil {
			t.Fatalf("expected nil details, got %+v", problem.Details)
		}
	})
}
