package convergence

import (
	"fmt"
	"strings"
	"testing"
)

func TestTransportCodeMapsSentinels(t *testing.T) {
	// UT-044 [contract,privacy,error]: map the stable error codes to transport
	// codes with the most specific sentinel winning.
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want string
	}{
		{"verification precedes config", fmt.Errorf("wrap: %w", ErrVerificationRequired), CodeVerificationRequired},
		{"read-only unsupported", ErrReadOnlyUnsupported, CodeReadOnlyUnsupported},
		{"target ineligible", ErrTargetIneligible, CodeTargetIneligible},
		{"already active", ErrAlreadyActive, CodeAlreadyActive},
		{"fingerprint mismatch", ErrFingerprintMismatch, CodeFingerprintMismatch},
		{"not parked", ErrNotParked, CodeNotParked},
		{"resume cursor stale", ErrResumeCursorStale, CodeResumeCursorStale},
		{"approval stale", ErrApprovalStale, CodeApprovalStale},
		{"workspace changed", ErrWorkspaceChanged, CodeWorkspaceChanged},
		{"unknown outcome", ErrUnknownOutcome, CodeUnknownOutcome},
		{"integrity failed", ErrIntegrityFailed, CodeIntegrityFailed},
		{"generic config invalid", ErrConfigInvalid, CodeConfigInvalid},
		{"profile not found maps to config", ErrProfileNotFound, CodeConfigInvalid},
		{"route invalid maps to config", ErrRouteInvalid, CodeConfigInvalid},
		{"replay conflict maps to integrity", ErrReplayConflict, CodeIntegrityFailed},
		{"transition invalid maps to integrity", ErrTransitionInvalid, CodeIntegrityFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			code, ok := TransportCode(tc.err)
			if !ok {
				t.Fatalf("TransportCode(%v) reported no code", tc.err)
			}
			if code != tc.want {
				t.Fatalf("TransportCode(%v) = %q, want %q", tc.err, code, tc.want)
			}
		})
	}

	t.Run("nil and unmapped errors report no code", func(t *testing.T) {
		t.Parallel()
		if _, ok := TransportCode(nil); ok {
			t.Fatal("TransportCode(nil) reported a code")
		}
		if _, ok := TransportCode(fmt.Errorf("unrelated")); ok {
			t.Fatal("TransportCode(unrelated) reported a code")
		}
	})

	t.Run("every declared transport code is stable and unique", func(t *testing.T) {
		t.Parallel()
		codes := TransportCodes()
		if len(codes) != 12 {
			t.Fatalf("transport code count = %d, want 12", len(codes))
		}
		seen := make(map[string]struct{}, len(codes))
		for _, code := range codes {
			if !strings.HasPrefix(code, "convergence_") {
				t.Fatalf("transport code %q is not namespaced", code)
			}
			if _, dup := seen[code]; dup {
				t.Fatalf("duplicate transport code %q", code)
			}
			seen[code] = struct{}{}
		}
	})
}

func TestRedactAndSafeDetailsProtectSensitiveDetail(t *testing.T) {
	// UT-044 privacy half: redact protected absolute paths and drop non-allowlisted
	// detail keys (findings, model output, authorization details) from transport.
	t.Parallel()

	t.Run("absolute paths are redacted, relative references retained", func(t *testing.T) {
		t.Parallel()
		if got := Redact("failed at /private/tmp/ws/main.go"); strings.Contains(got, "/private/tmp") {
			t.Fatalf("Redact leaked an absolute path: %q", got)
		}
		if got := Redact("evidence/reviews/finding.json"); got != "evidence/reviews/finding.json" {
			t.Fatalf("Redact mangled a relative reference: %q", got)
		}
		if got := Redact(`C:\Users\dev\secret.txt`); strings.Contains(got, `C:\Users`) {
			t.Fatalf("Redact leaked a Windows path: %q", got)
		}
	})

	t.Run("only allow-listed, redacted detail keys survive", func(t *testing.T) {
		t.Parallel()
		details := map[string]string{
			"field":          "convergence.verification.command",
			"config_path":    "/private/tmp/ws/compozy.toml",
			"finding_text":   "the reviewer said the auth check is missing",
			"model_output":   "chain-of-thought that must never leave the daemon",
			"actor_secret":   "principal-token-1234",
			"convergence_id": "cvg-1",
		}
		safe := SafeDetails(details)
		if _, ok := safe["finding_text"]; ok {
			t.Fatal("SafeDetails leaked protected review content")
		}
		if _, ok := safe["model_output"]; ok {
			t.Fatal("SafeDetails leaked model output")
		}
		if _, ok := safe["actor_secret"]; ok {
			t.Fatal("SafeDetails leaked an authorization detail")
		}
		if safe["field"] != "convergence.verification.command" {
			t.Fatalf("allow-listed field dropped: %q", safe["field"])
		}
		if strings.Contains(safe["config_path"], "/private/tmp") {
			t.Fatalf("config_path was not redacted: %q", safe["config_path"])
		}
		if safe["convergence_id"] != "cvg-1" {
			t.Fatalf("allow-listed identifier dropped: %q", safe["convergence_id"])
		}
	})

	t.Run("empty details map returns nil", func(t *testing.T) {
		t.Parallel()
		if SafeDetails(nil) != nil {
			t.Fatal("SafeDetails(nil) should be nil")
		}
		if SafeDetails(map[string]string{"finding_text": "x"}) != nil {
			t.Fatal("SafeDetails with only disallowed keys should be nil")
		}
	})
}
