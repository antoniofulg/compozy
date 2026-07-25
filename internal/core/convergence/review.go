package convergence

import (
	"fmt"
	"strings"
)

// ReviewOutcome is the structured outcome a read-only reviewer returns to the
// daemon. A reviewer may only report clean or findings; the daemon classifies an
// interrupted, denied, or replayed review with its own phase outcomes and never
// accepts those values from the reviewer contract.
type ReviewOutcome string

const (
	// ReviewOutcomeClean means the reviewer reports no actionable findings on the
	// current snapshot. It requires an empty actionable finding list and a nonblank
	// explanation; it never authorizes clean on its own without verification.
	ReviewOutcomeClean ReviewOutcome = "clean"
	// ReviewOutcomeFindings means the reviewer reports at least one actionable
	// finding, each with stable identity and observation evidence.
	ReviewOutcomeFindings ReviewOutcome = "findings"
)

// IsValid reports whether o is a recognized reviewer outcome.
func (o ReviewOutcome) IsValid() bool {
	return o == ReviewOutcomeClean || o == ReviewOutcomeFindings
}

// ReviewedFinding is one finding a reviewer reports: its stable identity inputs
// plus snapshot-bound observation data. Line, column, and evidence prose are
// observation data and never feed the semantic identity. A reviewer may also
// close a finding it deems invalid or a duplicate through Disposition, always
// with evidence.
type ReviewedFinding struct {
	// Identity is the reviewer-supplied semantic-v1 identity input.
	Identity FindingIdentity
	// Severity is the observed severity for this snapshot.
	Severity Severity
	// Outcome is what the reviewer reports for this finding on the current
	// snapshot: still actionable or no longer observed.
	Outcome ObservationOutcome
	// Evidence is the required nonblank observation evidence.
	Evidence string
	// Line and Column are observation-only location data.
	Line   int
	Column int
	// Disposition optionally closes the finding as invalid or duplicate. It is nil
	// when the reviewer only observes the finding.
	Disposition *ReviewedDisposition
}

// ReviewedDisposition is a reviewer's evidence-backed closure of a finding as
// invalid or a duplicate. Only these two disposition types may originate from a
// review; a waiver is a user-only decision handled elsewhere.
type ReviewedDisposition struct {
	// Type is invalid or duplicate.
	Type DispositionType
	// Evidence is the required justification for the closure.
	Evidence string
	// RelatedFingerprint optionally names the duplicate's counterpart.
	RelatedFingerprint FindingFingerprint
}

// ReviewResult is the structured output one read-only review returns through the
// session result channel. The daemon validates it before any state advances and
// is the only writer of the canonical review artifact; the reviewer session
// itself never writes an artifact. Every field the daemon persists derives from
// this validated value bound to Snapshot.
type ReviewResult struct {
	// ReviewID identifies the review and makes its observations idempotent under
	// replay.
	ReviewID string
	// Snapshot is the Git snapshot digest the review observed. A result bound to a
	// superseded snapshot is stale and cannot resolve a newer snapshot.
	Snapshot string
	// SnapshotSeq is the snapshot recency ordinal used to reject stale updates.
	SnapshotSeq uint64
	// Outcome is the reviewer's structured outcome.
	Outcome ReviewOutcome
	// Explanation is the required nonblank reviewer explanation.
	Explanation string
	// Findings is the reported finding set. It must be empty of actionable findings
	// for a clean outcome and nonempty for a findings outcome.
	Findings []ReviewedFinding
}

// Validate enforces the structured review contract before any state advances. It
// rejects a missing review identity, a missing snapshot binding, an empty or
// unexplained response, an unknown outcome enum, a clean outcome that still
// carries actionable findings, a findings outcome with no actionable finding, and
// any finding missing stable identity, severity, or evidence. It never guesses a
// missing field or a finding identity.
func (r ReviewResult) Validate() error {
	if strings.TrimSpace(r.ReviewID) == "" {
		return fmt.Errorf("%w: review id is required", ErrReviewInvalid)
	}
	if strings.TrimSpace(r.Snapshot) == "" {
		return fmt.Errorf("%w: review snapshot is required", ErrReviewInvalid)
	}
	if !r.Outcome.IsValid() {
		return fmt.Errorf("%w: unknown review outcome %q", ErrReviewInvalid, r.Outcome)
	}
	if strings.TrimSpace(r.Explanation) == "" {
		return fmt.Errorf("%w: review explanation is required", ErrReviewInvalid)
	}
	return r.validateFindings()
}

// validateFindings enforces the outcome-specific finding rules and validates every
// reported finding's identity and observation evidence.
func (r ReviewResult) validateFindings() error {
	actionable := 0
	for i := range r.Findings {
		if err := r.Findings[i].validate(); err != nil {
			return fmt.Errorf("finding %d: %w", i, err)
		}
		if r.Findings[i].Outcome == ObservationActionable && r.Findings[i].Disposition == nil {
			actionable++
		}
	}
	switch r.Outcome {
	case ReviewOutcomeClean:
		if actionable > 0 {
			return fmt.Errorf("%w: clean review cannot carry %d actionable findings", ErrReviewInvalid, actionable)
		}
	case ReviewOutcomeFindings:
		if actionable == 0 {
			return fmt.Errorf("%w: findings review reported no actionable finding", ErrReviewInvalid)
		}
	}
	return nil
}

// validate enforces one finding's identity, severity, outcome, and evidence.
func (f ReviewedFinding) validate() error {
	if _, err := f.Identity.Fingerprint(); err != nil {
		return err
	}
	if !f.Severity.IsValid() {
		return fmt.Errorf("%w: finding severity %q is invalid", ErrReviewInvalid, f.Severity)
	}
	if f.Outcome != ObservationActionable && f.Outcome != ObservationResolved {
		return fmt.Errorf("%w: finding outcome %q is invalid", ErrReviewInvalid, f.Outcome)
	}
	if strings.TrimSpace(f.Evidence) == "" {
		return fmt.Errorf("%w: finding evidence is required", ErrReviewInvalid)
	}
	if f.Disposition != nil {
		return f.Disposition.validate()
	}
	return nil
}

// validate enforces a reviewer disposition's type and evidence.
func (d ReviewedDisposition) validate() error {
	if d.Type != DispositionInvalid && d.Type != DispositionDuplicate {
		return fmt.Errorf("%w: review disposition %q must be invalid or duplicate", ErrReviewInvalid, d.Type)
	}
	if strings.TrimSpace(d.Evidence) == "" {
		return fmt.Errorf("%w: %s disposition requires evidence", ErrReviewInvalid, d.Type)
	}
	return nil
}

// Observations projects the validated review into ordered observation events
// bound to the review's snapshot. baseSeq is the projection sequence the first
// observation takes; each subsequent observation increments it so the caller can
// interleave observations from several reviews deterministically. Each
// ObservationID is deterministic in the review and finding identity so an
// at-least-once replay deduplicates. Validate must succeed before mapping.
func (r ReviewResult) Observations(baseSeq uint64) ([]ObservationEvent, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	out := make([]ObservationEvent, 0, len(r.Findings))
	for i := range r.Findings {
		fingerprint, err := r.Findings[i].Identity.Fingerprint()
		if err != nil {
			return nil, err
		}
		out = append(out, ObservationEvent{
			ObservationID: observationID(r.ReviewID, fingerprint),
			Fingerprint:   fingerprint,
			Sequence:      baseSeq + uint64(i),
			SnapshotSeq:   r.SnapshotSeq,
			Severity:      r.Findings[i].Severity,
			Outcome:       r.Findings[i].Outcome,
			ReviewID:      r.ReviewID,
		})
	}
	return out, nil
}

// Dispositions projects the review's evidence-backed invalid and duplicate
// closures into disposition events carried by a current-review actor. It returns
// no events when the review closed no finding. Validate must succeed first.
func (r ReviewResult) Dispositions() ([]DispositionEvent, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	actor := Actor{Kind: ActorReview, ID: r.ReviewID, CurrentReview: true}
	out := make([]DispositionEvent, 0)
	for i := range r.Findings {
		disposition := r.Findings[i].Disposition
		if disposition == nil {
			continue
		}
		fingerprint, err := r.Findings[i].Identity.Fingerprint()
		if err != nil {
			return nil, err
		}
		out = append(out, DispositionEvent{
			DecisionID:         dispositionID(r.ReviewID, fingerprint),
			Fingerprint:        fingerprint,
			Type:               disposition.Type,
			Actor:              actor,
			Evidence:           disposition.Evidence,
			SnapshotSeq:        r.SnapshotSeq,
			RelatedFingerprint: disposition.RelatedFingerprint,
		})
	}
	return out, nil
}

// observationID derives a review observation's idempotent identity.
func observationID(reviewID string, fingerprint FindingFingerprint) string {
	return reviewID + "/obs/" + string(fingerprint)
}

// dispositionID derives a review disposition's idempotent identity.
func dispositionID(reviewID string, fingerprint FindingFingerprint) string {
	return reviewID + "/disp/" + string(fingerprint)
}
