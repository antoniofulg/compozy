package convergence_test

import (
	"testing"

	"github.com/compozy/compozy/internal/core/convergence"
	"github.com/compozy/compozy/pkg/compozy/events"
)

func TestTerminalOutcomePublicMapping(t *testing.T) {
	// UT-037 [contract,state]: map convergence clean to run.completed, parked to the
	// public run.parked with its exact reason, cancellation to run.cancelled, and an
	// untrustworthy segment to run.failed.
	t.Parallel()

	cases := []struct {
		name       string
		outcome    convergence.TerminalOutcome
		wantKind   string
		wantStatus string
		wantReason string
	}{
		{
			name:       "clean maps to run.completed",
			outcome:    convergence.TerminalOutcome{Kind: convergence.TerminalClean},
			wantKind:   convergence.PublicEventCompleted,
			wantStatus: convergence.PublicStatusCompleted,
		},
		{
			name: "parked maps to run.parked with the exact reason",
			outcome: convergence.TerminalOutcome{
				Kind:   convergence.TerminalParked,
				Reason: convergence.ParkedApprovalRequired,
			},
			wantKind:   convergence.PublicEventParked,
			wantStatus: convergence.PublicStatusParked,
			wantReason: string(convergence.ParkedApprovalRequired),
		},
		{
			name:       "cancellation maps to run.cancelled",
			outcome:    convergence.TerminalOutcome{Kind: convergence.TerminalCancelled},
			wantKind:   convergence.PublicEventCancelled,
			wantStatus: convergence.PublicStatusCanceled,
		},
		{
			name:       "failed maps to run.failed",
			outcome:    convergence.TerminalOutcome{Kind: convergence.TerminalFailed},
			wantKind:   convergence.PublicEventFailed,
			wantStatus: convergence.PublicStatusFailed,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := tc.outcome.Public()
			if err != nil {
				t.Fatalf("Public() = %v", err)
			}
			if got.EventKind != tc.wantKind {
				t.Fatalf("event kind = %q, want %q", got.EventKind, tc.wantKind)
			}
			if got.Status != tc.wantStatus {
				t.Fatalf("status = %q, want %q", got.Status, tc.wantStatus)
			}
			if got.Reason != tc.wantReason {
				t.Fatalf("reason = %q, want %q", got.Reason, tc.wantReason)
			}
		})
	}

	t.Run("every valid parked reason surfaces exactly on run.parked", func(t *testing.T) {
		t.Parallel()
		reasons := []convergence.ParkedReason{
			convergence.ParkedApprovalRequired,
			convergence.ParkedWorkspaceChanged,
			convergence.ParkedVerificationFailed,
			convergence.ParkedFixAttemptsExhausted,
			convergence.ParkedOscillation,
			convergence.ParkedNoProgress,
			convergence.ParkedMaxRounds,
			convergence.ParkedTimeLimit,
			convergence.ParkedRuntimeUnavailable,
		}
		for _, reason := range reasons {
			outcome := convergence.TerminalOutcome{Kind: convergence.TerminalParked, Reason: reason}
			got, err := outcome.Public()
			if err != nil {
				t.Fatalf("Public(parked %q) = %v", reason, err)
			}
			if got.EventKind != convergence.PublicEventParked || got.Reason != string(reason) {
				t.Fatalf("parked %q mapped to %+v", reason, got)
			}
		}
	})

	t.Run("error: a parked outcome without a valid reason is rejected", func(t *testing.T) {
		t.Parallel()
		if _, err := (convergence.TerminalOutcome{Kind: convergence.TerminalParked}).Public(); err == nil {
			t.Fatal("Public() accepted a parked outcome with no reason")
		}
	})

	t.Run("error: an unrecognized terminal kind is rejected", func(t *testing.T) {
		t.Parallel()
		if _, err := (convergence.TerminalOutcome{Kind: convergence.TerminalKind("bogus")}).Public(); err == nil {
			t.Fatal("Public() accepted an unrecognized terminal kind")
		}
	})
}

func TestPublicEventKindStringsMatchEventsPackage(t *testing.T) {
	// UT-037 drift guard: the pure domain keeps public run.* event kinds as strings
	// so it imports no transport package; this cross-package test proves those
	// strings still equal the canonical events.EventKind constants.
	t.Parallel()

	pairs := []struct {
		domain string
		canon  events.EventKind
	}{
		{convergence.PublicEventCompleted, events.EventKindRunCompleted},
		{convergence.PublicEventParked, events.EventKindRunParked},
		{convergence.PublicEventFailed, events.EventKindRunFailed},
		{convergence.PublicEventCancelled, events.EventKindRunCancelled},
	}
	for _, pair := range pairs {
		if pair.domain != string(pair.canon) {
			t.Fatalf("domain kind %q drifted from events kind %q", pair.domain, pair.canon)
		}
	}
}
