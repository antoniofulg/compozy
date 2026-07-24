package convergence

// EvaluationInput carries the deterministic conditions the evaluation phase needs
// to select a terminal outcome or admit another round. Every field is computed by
// the daemon from durable evidence; the evaluation itself performs no I/O.
type EvaluationInput struct {
	// CancellationAccepted is true once a user cancellation is durably accepted.
	CancellationAccepted bool
	// DurableStateUntrusted is true when durable or contract state cannot be trusted.
	DurableStateUntrusted bool
	// CurrentReviewClean is true when the current-snapshot review reported no
	// actionable findings with a nonblank explanation.
	CurrentReviewClean bool
	// VerificationPassedCurrentSnapshot is true when verification passed on the same
	// snapshot the clean review observed.
	VerificationPassedCurrentSnapshot bool
	// AcceptedActionableFindings counts findings still accepted and actionable.
	AcceptedActionableFindings int
	// ApprovalPending is true when a protected-action decision is outstanding.
	ApprovalPending bool
	// WorkspaceDiverged is true when an unexplained Git difference was detected.
	WorkspaceDiverged bool
	// VerificationAttemptsExhausted is true when verification attempts ran out.
	VerificationAttemptsExhausted bool
	// FindingAttemptsExhausted is true when any finding's correction attempts ran out.
	FindingAttemptsExhausted bool
	// OscillationReached is true when a finding oscillated the configured cycles.
	OscillationReached bool
	// NoProgressReached is true when consecutive no-progress rounds hit the limit.
	NoProgressReached bool
	// MaxRoundsReached is true when no further round may be admitted by count.
	MaxRoundsReached bool
	// TimeLimitReached is true when the review admission boundary elapsed.
	TimeLimitReached bool
	// RuntimeUnavailable is true when neither primary nor fallback route is usable.
	RuntimeUnavailable bool
}

// EvaluationResult is the deterministic outcome of one evaluation. When Terminal
// is false the segment admits review(N+1); otherwise Outcome is authoritative.
type EvaluationResult struct {
	Terminal  bool
	Outcome   TerminalOutcome
	NextRound bool
}

// cleanSatisfied reports whether the strict clean contract holds: an independent
// current review reports no actionable findings, verification passes on that same
// snapshot, and no accepted actionable finding remains.
func (in EvaluationInput) cleanSatisfied() bool {
	return in.CurrentReviewClean &&
		in.VerificationPassedCurrentSnapshot &&
		in.AcceptedActionableFindings == 0
}

// EvaluateTerminal applies the terminal-decision priority exactly. The clean
// check precedes every limit park because a round admitted within the boundary
// may finish clean after a limit is reached, and no other stop reason may be
// rendered as clean. When no condition matches, the segment advances to the next
// round.
func EvaluateTerminal(in EvaluationInput) EvaluationResult {
	if in.CancellationAccepted {
		return terminal(TerminalOutcome{Kind: TerminalCancelled})
	}
	if in.DurableStateUntrusted {
		return terminal(TerminalOutcome{Kind: TerminalFailed})
	}
	if in.cleanSatisfied() {
		return terminal(TerminalOutcome{Kind: TerminalClean})
	}
	for _, rule := range parkPriority(in) {
		if rule.active {
			return terminal(parked(rule.reason))
		}
	}
	return EvaluationResult{Terminal: false, NextRound: true}
}

// parkRule pairs a park reason with whether its condition is active. The slice
// order encodes steps four through twelve of the terminal priority.
type parkRule struct {
	active bool
	reason ParkedReason
}

func parkPriority(in EvaluationInput) []parkRule {
	return []parkRule{
		{in.ApprovalPending, ParkedApprovalRequired},
		{in.WorkspaceDiverged, ParkedWorkspaceChanged},
		{in.VerificationAttemptsExhausted, ParkedVerificationFailed},
		{in.FindingAttemptsExhausted, ParkedFixAttemptsExhausted},
		{in.OscillationReached, ParkedOscillation},
		{in.NoProgressReached, ParkedNoProgress},
		{in.MaxRoundsReached, ParkedMaxRounds},
		{in.TimeLimitReached, ParkedTimeLimit},
		{in.RuntimeUnavailable, ParkedRuntimeUnavailable},
	}
}

func terminal(outcome TerminalOutcome) EvaluationResult {
	return EvaluationResult{Terminal: true, Outcome: outcome}
}
