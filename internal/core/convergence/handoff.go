package convergence

import (
	"fmt"
	"strings"
)

// SessionRole names the phase role a fresh model session is created for. Review
// and correction always run in separate sessions with separate authority, even
// when the same model backs both.
type SessionRole string

const (
	// SessionReview is a read-only review session.
	SessionReview SessionRole = "review"
	// SessionCorrection is a correction batch session.
	SessionCorrection SessionRole = "correction"
)

// RemainingLimits is the remaining budget a fresh session is told about so it can
// stop before exhausting a limit.
type RemainingLimits struct {
	ReviewRounds    int
	FindingAttempts int
}

// Handoff is the complete canonical context one fresh model session receives. It
// deliberately carries no conversational transcript from any prior phase: a
// session is reconstructed only from durable canonical state, relevant accepted
// decisions, the current diff and evidence references, the selected route, and
// remaining limits. SessionID is a unique child identity linked to the run,
// round, phase, and optional batch so evidence binds back to exactly one session.
type Handoff struct {
	Role              SessionRole
	SessionID         string
	RunID             string
	RoundNumber       int
	PhaseID           string
	BatchID           string
	Snapshot          string
	Route             Route
	Intent            string
	AcceptedDecisions []string
	DiffRef           string
	OpenFindings      []FindingFingerprint
	VerificationRef   string
	Remaining         RemainingLimits
	ArtifactRefs      []string
}

// ReviewHandoffInput carries the durable inputs a review session handoff needs.
type ReviewHandoffInput struct {
	RunID             string
	RoundNumber       int
	PhaseID           string
	Snapshot          string
	Route             Route
	Intent            string
	AcceptedDecisions []string
	DiffRef           string
	OpenFindings      []FindingFingerprint
	VerificationRef   string
	Remaining         RemainingLimits
	ArtifactRefs      []string
}

// CorrectionHandoffInput carries the durable inputs a correction batch session
// handoff needs. Only the batch's own findings are handed off so the session
// stays scoped to one logical batch.
type CorrectionHandoffInput struct {
	RunID             string
	RoundNumber       int
	PhaseID           string
	Batch             CorrectionBatch
	Snapshot          string
	Route             Route
	Intent            string
	AcceptedDecisions []string
	DiffRef           string
	VerificationRef   string
	Remaining         RemainingLimits
	ArtifactRefs      []string
}

// BuildReviewHandoff assembles the minimal canonical context for a fresh
// read-only review session and derives its unique child session identity. It
// rejects a handoff missing the run, phase, or snapshot binding required to make
// the session's evidence attributable.
func BuildReviewHandoff(in ReviewHandoffInput) (Handoff, error) {
	if err := requireHandoffBinding(in.RunID, in.PhaseID, in.Snapshot); err != nil {
		return Handoff{}, err
	}
	return Handoff{
		Role:              SessionReview,
		SessionID:         sessionID(in.RunID, in.RoundNumber, in.PhaseID, ""),
		RunID:             in.RunID,
		RoundNumber:       in.RoundNumber,
		PhaseID:           in.PhaseID,
		Snapshot:          in.Snapshot,
		Route:             in.Route,
		Intent:            in.Intent,
		AcceptedDecisions: cloneStrings(in.AcceptedDecisions),
		DiffRef:           in.DiffRef,
		OpenFindings:      cloneFingerprints(in.OpenFindings),
		VerificationRef:   in.VerificationRef,
		Remaining:         in.Remaining,
		ArtifactRefs:      cloneStrings(in.ArtifactRefs),
	}, nil
}

// BuildCorrectionHandoff assembles the minimal canonical context for a fresh
// correction batch session and derives its unique child session identity. The
// handoff carries only the batch's own findings and the route the batch's highest
// severity selected.
func BuildCorrectionHandoff(in CorrectionHandoffInput) (Handoff, error) {
	if err := requireHandoffBinding(in.RunID, in.PhaseID, in.Snapshot); err != nil {
		return Handoff{}, err
	}
	if strings.TrimSpace(in.Batch.File) == "" {
		return Handoff{}, fmt.Errorf("%w: correction handoff requires a batch file", ErrCorrectionInvalid)
	}
	batchID := correctionBatchID(in.RunID, in.RoundNumber, in.Batch.Order)
	return Handoff{
		Role:              SessionCorrection,
		SessionID:         sessionID(in.RunID, in.RoundNumber, in.PhaseID, batchID),
		RunID:             in.RunID,
		RoundNumber:       in.RoundNumber,
		PhaseID:           in.PhaseID,
		BatchID:           batchID,
		Snapshot:          in.Snapshot,
		Route:             in.Route,
		Intent:            in.Intent,
		AcceptedDecisions: cloneStrings(in.AcceptedDecisions),
		DiffRef:           in.DiffRef,
		OpenFindings:      cloneFingerprints(in.Batch.FindingFingerprints),
		VerificationRef:   in.VerificationRef,
		Remaining:         in.Remaining,
		ArtifactRefs:      cloneStrings(in.ArtifactRefs),
	}, nil
}

func requireHandoffBinding(runID, phaseID, snapshot string) error {
	if strings.TrimSpace(runID) == "" {
		return fmt.Errorf("%w: handoff run id is required", ErrCorrectionInvalid)
	}
	if strings.TrimSpace(phaseID) == "" {
		return fmt.Errorf("%w: handoff phase id is required", ErrCorrectionInvalid)
	}
	if strings.TrimSpace(snapshot) == "" {
		return fmt.Errorf("%w: handoff snapshot binding is required", ErrCorrectionInvalid)
	}
	return nil
}

// sessionID derives a unique child session identity linked to run, round, phase,
// and optional batch so a session's evidence binds back to exactly one identity.
func sessionID(runID string, round int, phaseID, batchID string) string {
	id := fmt.Sprintf("%s/r%d/%s", runID, round, phaseID)
	if batchID != "" {
		id += "/" + batchID
	}
	return id
}

// correctionBatchID derives the durable batch identity for a same-file batch.
func correctionBatchID(runID string, round, order int) string {
	return fmt.Sprintf("%s/r%d/batch-%d", runID, round, order)
}

func cloneFingerprints(in []FindingFingerprint) []FindingFingerprint {
	if len(in) == 0 {
		return nil
	}
	out := make([]FindingFingerprint, len(in))
	copy(out, in)
	return out
}
