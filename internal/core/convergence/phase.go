package convergence

// PhaseKind enumerates the model and verification phases of one convergence
// segment. These are the only legal phase kinds; a segment is otherwise in the
// non-phase lifecycle states tracked by SegmentState.
type PhaseKind string

const (
	// PhaseInitialVerification is the authoritative verification that must pass
	// before the first review.
	PhaseInitialVerification PhaseKind = "initial_verification"
	// PhasePreReviewCorrection corrects an initial verification failure before any
	// review begins.
	PhasePreReviewCorrection PhaseKind = "pre_review_correction"
	// PhaseReview is a fresh read-only review of the current snapshot.
	PhaseReview PhaseKind = "review"
	// PhaseCorrection is one sequential correction batch within a round.
	PhaseCorrection PhaseKind = "correction"
	// PhasePostCorrectionVerification runs once after all correction batches in a
	// round against the combined snapshot.
	PhasePostCorrectionVerification PhaseKind = "post_correction_verification"
	// PhaseEvaluation applies terminal-decision priority and admits the next round.
	PhaseEvaluation PhaseKind = "evaluation"
)

// IsValid reports whether k is a recognized phase kind.
func (k PhaseKind) IsValid() bool {
	switch k {
	case PhaseInitialVerification,
		PhasePreReviewCorrection,
		PhaseReview,
		PhaseCorrection,
		PhasePostCorrectionVerification,
		PhaseEvaluation:
		return true
	default:
		return false
	}
}

// SegmentState is the lifecycle state of a convergence segment around its phases.
type SegmentState string

const (
	// SegmentPrepared is the frozen pre-execution state before the first phase.
	SegmentPrepared SegmentState = "prepared"
	// SegmentActive means a phase is in flight.
	SegmentActive SegmentState = "active"
	// SegmentTerminal means the segment reached a terminal outcome and is immutable.
	SegmentTerminal SegmentState = "terminal"
)

// FirstPhase returns the phase a prepared segment always starts with.
func FirstPhase() PhaseKind { return PhaseInitialVerification }

// phaseTransitions is the deterministic phase graph. It excludes the terminal
// decisions taken during evaluation, which EvaluateTerminal owns.
var phaseTransitions = map[PhaseKind]map[PhaseKind]struct{}{
	PhaseInitialVerification: {
		PhasePreReviewCorrection: {}, // verification failed
		PhaseReview:              {}, // verification passed
	},
	PhasePreReviewCorrection: {
		PhaseInitialVerification: {}, // re-verify after correcting the baseline
	},
	PhaseReview: {
		PhaseCorrection:                 {}, // findings to fix
		PhasePostCorrectionVerification: {}, // clean review still verifies the snapshot
	},
	PhaseCorrection: {
		PhaseCorrection:                 {}, // next sequential same-round batch
		PhasePostCorrectionVerification: {}, // all batches complete
	},
	PhasePostCorrectionVerification: {
		PhaseEvaluation: {}, // verification result decided
		PhaseCorrection: {}, // verification failed, retry within limits
	},
	PhaseEvaluation: {
		PhaseReview: {}, // admit the next round
	},
}

// CanTransition reports whether a segment may move directly from one phase to
// another. The empty PhaseKind represents the prepared start, whose only legal
// first phase is initial verification.
func CanTransition(from, to PhaseKind) bool {
	if from == "" {
		return to == FirstPhase()
	}
	next, ok := phaseTransitions[from]
	if !ok {
		return false
	}
	_, ok = next[to]
	return ok
}
