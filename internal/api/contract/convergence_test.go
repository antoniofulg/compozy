package contract_test

import (
	"encoding/json"
	"testing"

	"github.com/compozy/compozy/internal/api/contract"
	"github.com/compozy/compozy/internal/core/convergence"
)

// TestConvergenceErrorCodesMatchDomainTransportCodes guards against drift between
// the transport-side convergence code catalog and the domain's canonical table.
// The domain owns the sentinel->code mapping; the contract package restates the
// codes for older/newer clients, and this test keeps the two in lockstep.
func TestConvergenceErrorCodesMatchDomainTransportCodes(t *testing.T) {
	t.Parallel()
	t.Run("Should list identical codes in identical order", func(t *testing.T) {
		t.Parallel()
		domain := convergence.TransportCodes()
		if len(contract.ConvergenceErrorCodes) != len(domain) {
			t.Fatalf(
				"convergence code count drift: contract=%d domain=%d",
				len(contract.ConvergenceErrorCodes),
				len(domain),
			)
		}
		for i, code := range contract.ConvergenceErrorCodes {
			if string(code) != domain[i] {
				t.Fatalf("code drift at %d: contract=%q domain=%q", i, string(code), domain[i])
			}
		}
	})
	t.Run("Should register every convergence code as a canonical code", func(t *testing.T) {
		t.Parallel()
		canonical := make(map[contract.ErrorCode]struct{}, len(contract.CanonicalErrorCodes))
		for _, code := range contract.CanonicalErrorCodes {
			canonical[code] = struct{}{}
		}
		for _, code := range contract.ConvergenceErrorCodes {
			if _, ok := canonical[code]; !ok {
				t.Fatalf("convergence code %q missing from CanonicalErrorCodes", string(code))
			}
		}
	})
}

// TestConvergenceSnapshotResponseRoundTrips proves the read model is a stable,
// JSON-serializable transport equivalent of the internal snapshot.
func TestConvergenceSnapshotResponseRoundTrips(t *testing.T) {
	t.Parallel()
	t.Run("Should preserve fields across marshal and unmarshal", func(t *testing.T) {
		t.Parallel()
		exit := 0
		original := contract.ConvergenceSnapshotResponse{
			Convergence: contract.ConvergenceSnapshot{
				ConvergenceID: "cvg-1",
				RequestID:     "req-1",
				Segment: contract.ConvergenceSegment{
					RunID:   "run-1",
					Ordinal: 2,
					State:   "active",
				},
				Target: contract.ConvergenceTarget{WorkspaceID: "ws-1", TaskGroupID: "TG-004"},
				Config: contract.ConvergenceConfigSummary{
					Profile:    "quality",
					ModelSetup: "balanced",
					AutoCommit: true,
					Limits:     contract.ConvergenceLimits{MaxReviewRounds: 6, ReviewAdmissionTimeout: "90m0s"},
				},
				Phase: contract.ConvergencePhase{Kind: "review", Round: 3, Attempt: 1},
				Findings: []contract.ConvergenceFinding{
					{Fingerprint: "abc", State: "actionable", Severity: "high"},
				},
				Verification: []contract.ConvergenceVerification{{VerificationID: "v1", Passed: true, ExitCode: &exit}},
				Terminal: &contract.ConvergenceTerminal{
					Kind:      "parked",
					Reason:    "approval_required",
					Status:    "parked",
					EventKind: "run.parked",
				},
				UnresolvedCount: 1,
				LastSeq:         42,
			},
		}
		encoded, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var decoded contract.ConvergenceSnapshotResponse
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if decoded.Convergence.ConvergenceID != original.Convergence.ConvergenceID {
			t.Fatalf("convergence id drift: %q", decoded.Convergence.ConvergenceID)
		}
		if decoded.Convergence.Terminal == nil || decoded.Convergence.Terminal.Reason != "approval_required" {
			t.Fatalf("terminal reason drift: %+v", decoded.Convergence.Terminal)
		}
		if decoded.Convergence.Verification[0].ExitCode == nil ||
			*decoded.Convergence.Verification[0].ExitCode != 0 {
			t.Fatalf("exit code drift: %+v", decoded.Convergence.Verification[0].ExitCode)
		}
		if decoded.Convergence.UnresolvedCount != 1 || decoded.Convergence.LastSeq != 42 {
			t.Fatalf("counter drift: unresolved=%d seq=%d",
				decoded.Convergence.UnresolvedCount, decoded.Convergence.LastSeq)
		}
	})
}
