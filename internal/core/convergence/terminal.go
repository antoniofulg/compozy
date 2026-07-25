package convergence

import "github.com/compozy/compozy/pkg/compozy/events/kinds"

// TerminalKind is the coarse terminal classification of a convergence segment.
type TerminalKind string

const (
	// TerminalClean is verified convergence.
	TerminalClean TerminalKind = "clean"
	// TerminalParked is a non-clean, safe, resumable stop with a precise reason.
	TerminalParked TerminalKind = "parked"
	// TerminalFailed means durable state or the convergence contract cannot be trusted.
	TerminalFailed TerminalKind = "failed"
	// TerminalCancelled is an explicit user cancellation. The value matches the
	// run store's bare status spelling; the public event uses run.cancelled.
	TerminalCancelled TerminalKind = "canceled"
)

// ParkedReason is the precise reason a segment parked. Every parked outcome
// carries exactly one reason.
type ParkedReason string

const (
	// ParkedApprovalRequired means a user decision is pending on a protected action.
	ParkedApprovalRequired ParkedReason = kinds.ConvergenceParkedReasonApprovalRequired
	// ParkedWorkspaceChanged means an unexplained Git difference was detected.
	ParkedWorkspaceChanged ParkedReason = "workspace_changed"
	// ParkedVerificationFailed means verification attempts were exhausted.
	ParkedVerificationFailed ParkedReason = "verification_failed"
	// ParkedFixAttemptsExhausted means finding correction attempts were exhausted.
	ParkedFixAttemptsExhausted ParkedReason = "fix_attempts_exhausted"
	// ParkedOscillation means a finding disappeared and returned the configured
	// number of cycles.
	ParkedOscillation ParkedReason = "oscillation"
	// ParkedNoProgress means the configured consecutive no-progress rounds elapsed.
	ParkedNoProgress ParkedReason = "no_progress"
	// ParkedMaxRounds means no round admission remains.
	ParkedMaxRounds ParkedReason = "max_rounds"
	// ParkedTimeLimit means the review admission boundary elapsed.
	ParkedTimeLimit ParkedReason = "time_limit"
	// ParkedRuntimeUnavailable means neither the primary nor fallback route is usable.
	ParkedRuntimeUnavailable ParkedReason = "runtime_unavailable"
)

// IsValid reports whether r is a recognized parked reason.
func (r ParkedReason) IsValid() bool {
	switch r {
	case ParkedApprovalRequired,
		ParkedWorkspaceChanged,
		ParkedVerificationFailed,
		ParkedFixAttemptsExhausted,
		ParkedOscillation,
		ParkedNoProgress,
		ParkedMaxRounds,
		ParkedTimeLimit,
		ParkedRuntimeUnavailable:
		return true
	default:
		return false
	}
}

// TerminalOutcome is a fully determined terminal result. Reason is set only when
// Kind is TerminalParked.
type TerminalOutcome struct {
	Kind   TerminalKind
	Reason ParkedReason
}

// IsClean reports whether the outcome certifies verified convergence.
func (o TerminalOutcome) IsClean() bool { return o.Kind == TerminalClean }

// parked builds a parked terminal outcome for reason.
func parked(reason ParkedReason) TerminalOutcome {
	return TerminalOutcome{Kind: TerminalParked, Reason: reason}
}
