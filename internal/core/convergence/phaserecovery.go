package convergence

import "fmt"

// RecoveryState is the action a restart selects for one incomplete phase after
// classifying its durable journal, process, and Git evidence. Every state either
// resumes durable evidence without a repeated side effect or settles the segment
// safely; no state blindly repeats work whose side effect may already exist.
type RecoveryState string

const (
	// RecoveryStart means no phase record exists and the phase may start cleanly.
	RecoveryStart RecoveryState = "start"
	// RecoveryRetry means an incomplete attempt with no proven side effect may
	// retry within the remaining limits.
	RecoveryRetry RecoveryState = "retry_incomplete"
	// RecoveryReplay means a completed durable result may replay without re-running
	// the phase.
	RecoveryReplay RecoveryState = "replay"
	// RecoveryReconstruct means a verification result row is missing but trusted
	// exit evidence lets the daemon reconstruct pass/fail and checkpoint it.
	RecoveryReconstruct RecoveryState = "reconstruct_verification"
	// RecoverySafeRetry means a lost fixer response left the owned snapshot proven
	// unchanged, so the correction may retry under a new phase identity while
	// preserving the consumed-attempt record.
	RecoverySafeRetry RecoveryState = "safe_retry"
	// RecoveryVerifyReview means a lost fixer response left a durable owned change,
	// so the correction is never repeated; authoritative verification runs and a
	// fresh read-only review reconstructs current state.
	RecoveryVerifyReview RecoveryState = "verify_then_review"
	// RecoveryPark means evidence is insufficient or conflicting but canonical run
	// history is trusted, so the segment parks resumably with UNKNOWN_OUTCOME.
	RecoveryPark RecoveryState = "park_unknown"
	// RecoveryFail means evidence is insufficient or conflicting and canonical run
	// history is untrusted, so the segment fails on an integrity violation.
	RecoveryFail RecoveryState = "fail_integrity"
)

// RecoveryClass maps a recovery state to the coarse retry-admission class the
// SequenceGuard and AdmitRetry consume. Only Park and Fail are unknown work that
// is never granted a free retry; every other state consumes durable evidence.
func (s RecoveryState) RecoveryClass() RecoveryClass {
	switch s {
	case RecoveryStart:
		return RecoveryStartable
	case RecoveryRetry, RecoverySafeRetry:
		return RecoveryRetryable
	case RecoveryReplay, RecoveryReconstruct, RecoveryVerifyReview:
		return RecoveryReplayable
	default:
		return RecoveryUnknown
	}
}

// RecoveryDecision is the outcome of classifying one incomplete phase. Terminal
// is set only when recovery settles the segment to a failed terminal on an
// integrity violation; it is nil for every recoverable state and for a trusted
// unknown park, which the caller surfaces as a resumable park.
type RecoveryDecision struct {
	State    RecoveryState
	Terminal *TerminalOutcome
}

// RepeatsSideEffect reports whether the decision reruns the original phase whose
// side effect may already exist. Only a clean start or a proven-unchanged safe
// retry repeats the phase; both are provably free of a duplicate side effect.
func (d RecoveryDecision) RepeatsSideEffect() bool {
	return d.State == RecoveryStart || d.State == RecoverySafeRetry
}

// PhaseEvidence is the durable evidence a restart reads to classify a
// verification, review, or coordinator phase. Every field is derived from the run
// journal, process supervision state, and Git snapshots; the classifier performs
// no I/O.
type PhaseEvidence struct {
	// HasRecord is false when no durable phase record exists.
	HasRecord bool
	// ResultComplete reports whether a durable terminal result exists.
	ResultComplete bool
	// ResultReadable reports whether the durable result and evidence are readable
	// rather than corrupt.
	ResultReadable bool
	// SnapshotMatches reports whether a completed result's snapshot and checksum
	// match durable canonical state.
	SnapshotMatches bool
	// FingerprintMatches reports whether a completed result's identity fingerprint
	// matches the phase request.
	FingerprintMatches bool
	// ExitEvidenceTrusted reports whether a missing verification result still has
	// trusted process-exit evidence to reconstruct pass/fail from.
	ExitEvidenceTrusted bool
	// ExpectedSnapshotUnchanged reports whether the expected snapshot still holds
	// for a phase that has no durable record yet.
	ExpectedSnapshotUnchanged bool
	// CanonicalHistoryTrusted reports whether canonical run history remains
	// trustworthy, selecting park versus fail for an unknown outcome.
	CanonicalHistoryTrusted bool
}

// ClassifyPhaseRecovery selects the recovery action for one incomplete
// verification, review, or coordinator phase from its durable evidence. It never
// grants a repeat whose side effect may already exist: a completed result
// replays, a reconstructable verification is rebuilt from trusted exit evidence,
// an incomplete attempt retries, and conflicting, mismatched, or corrupt evidence
// returns UNKNOWN_OUTCOME parked when canonical history is trusted or failed when
// it is not.
func ClassifyPhaseRecovery(ev PhaseEvidence) (RecoveryDecision, error) {
	if !ev.HasRecord {
		if ev.ExpectedSnapshotUnchanged {
			return RecoveryDecision{State: RecoveryStart}, nil
		}
		return unknownRecovery(ev.CanonicalHistoryTrusted)
	}
	if !ev.ResultReadable {
		return unknownRecovery(ev.CanonicalHistoryTrusted)
	}
	if !ev.ResultComplete {
		if ev.ExitEvidenceTrusted {
			return RecoveryDecision{State: RecoveryReconstruct}, nil
		}
		return RecoveryDecision{State: RecoveryRetry}, nil
	}
	if ev.SnapshotMatches && ev.FingerprintMatches {
		return RecoveryDecision{State: RecoveryReplay}, nil
	}
	return unknownRecovery(ev.CanonicalHistoryTrusted)
}

// CorrectionEvidence is the durable evidence a restart reads to classify an
// incomplete correction phase whose fixer response was lost.
type CorrectionEvidence struct {
	// HasRecord is false when no durable correction phase record exists.
	HasRecord bool
	// ResultComplete reports whether a durable terminal correction result exists.
	ResultComplete bool
	// ResultReadable reports whether the durable correction evidence is readable
	// rather than corrupt.
	ResultReadable bool
	// EvidenceConsistent reports whether the journal, process, and Git evidence
	// agree; conflicting evidence is an unknown outcome.
	EvidenceConsistent bool
	// SnapshotChanged reports whether the owned Git snapshot changed since the
	// correction phase began.
	SnapshotChanged bool
	// ChangeOwned reports whether a detected change is attributable to this
	// correction phase rather than an external boundary edit.
	ChangeOwned bool
	// CanonicalHistoryTrusted reports whether canonical run history remains
	// trustworthy, selecting park versus fail for an unknown outcome.
	CanonicalHistoryTrusted bool
}

// ClassifyCorrectionRecovery selects the recovery action for an incomplete
// correction phase. A missing result with a proven unchanged snapshot retries
// safely under a new identity while preserving the consumed attempt; a missing
// result with a durable owned change is never repeated and instead runs
// verification then a fresh read-only review; a completed result replays; and
// conflicting, corrupt, or unattributable evidence returns UNKNOWN_OUTCOME parked
// or failed by canonical trust.
func ClassifyCorrectionRecovery(ev CorrectionEvidence) (RecoveryDecision, error) {
	if !ev.HasRecord {
		return RecoveryDecision{State: RecoveryStart}, nil
	}
	if !ev.ResultReadable {
		return unknownRecovery(ev.CanonicalHistoryTrusted)
	}
	if ev.ResultComplete {
		return RecoveryDecision{State: RecoveryReplay}, nil
	}
	if !ev.EvidenceConsistent {
		return unknownRecovery(ev.CanonicalHistoryTrusted)
	}
	if !ev.SnapshotChanged {
		return RecoveryDecision{State: RecoverySafeRetry}, nil
	}
	if ev.ChangeOwned {
		return RecoveryDecision{State: RecoveryVerifyReview}, nil
	}
	return unknownRecovery(ev.CanonicalHistoryTrusted)
}

// unknownRecovery settles an insufficient or conflicting evidence outcome. A
// trusted canonical history parks resumably with UNKNOWN_OUTCOME; an untrusted
// one fails on an integrity violation. Neither repeats a side effect.
func unknownRecovery(canonicalTrusted bool) (RecoveryDecision, error) {
	if canonicalTrusted {
		return RecoveryDecision{State: RecoveryPark},
			fmt.Errorf("%w: durable evidence is insufficient or conflicting", ErrUnknownOutcome)
	}
	failed := TerminalOutcome{Kind: TerminalFailed}
	return RecoveryDecision{State: RecoveryFail, Terminal: &failed},
		fmt.Errorf("%w: canonical history is untrusted for an unknown outcome", ErrIntegrityFailed)
}
