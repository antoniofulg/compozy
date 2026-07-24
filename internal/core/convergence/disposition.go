package convergence

import (
	"fmt"
	"strings"
)

// authorizeDisposition enforces the authority and completeness rules for a
// disposition and returns the finding state it produces. Invalid and duplicate
// require a current review with evidence; waived requires an authorized user with
// a reason. Any model or unauthorized actor is denied.
func authorizeDisposition(event DispositionEvent) (FindingState, error) {
	state, ok := event.Type.finalState()
	if !ok {
		return "", fmt.Errorf("%w: unknown disposition type %q", ErrDispositionIncomplete, event.Type)
	}
	switch event.Type {
	case DispositionInvalid, DispositionDuplicate:
		return authorizeReviewDisposition(event, state)
	case DispositionWaived:
		return authorizeWaiver(event, state)
	default:
		return "", fmt.Errorf("%w: unsupported disposition %q", ErrDispositionIncomplete, event.Type)
	}
}

func authorizeReviewDisposition(event DispositionEvent, state FindingState) (FindingState, error) {
	if event.Actor.Kind != ActorReview || !event.Actor.CurrentReview {
		return "", fmt.Errorf(
			"%w: only a current review may mark %s",
			ErrDispositionUnauthorized,
			event.Type,
		)
	}
	if strings.TrimSpace(event.Evidence) == "" {
		return "", fmt.Errorf("%w: %s disposition requires evidence", ErrDispositionIncomplete, event.Type)
	}
	return state, nil
}

func authorizeWaiver(event DispositionEvent, state FindingState) (FindingState, error) {
	if event.Actor.Kind != ActorUser || !event.Actor.RunAuthority {
		return "", fmt.Errorf("%w: only an authorized user may waive a finding", ErrDispositionUnauthorized)
	}
	if strings.TrimSpace(event.Reason) == "" {
		return "", fmt.Errorf("%w: waiver requires a reason", ErrDispositionIncomplete)
	}
	return state, nil
}

// State returns the current projected state of one finding.
func (p *FindingProjection) State(fingerprint FindingFingerprint) (FindingState, bool) {
	record, ok := p.records[fingerprint]
	if !ok {
		return "", false
	}
	return record.state, true
}

// Severity returns the current observed severity of one finding.
func (p *FindingProjection) Severity(fingerprint FindingFingerprint) (Severity, bool) {
	record, ok := p.records[fingerprint]
	if !ok {
		return "", false
	}
	return record.severity, true
}

// OpenActionableCount returns the number of findings still accepted and actionable.
func (p *FindingProjection) OpenActionableCount() int {
	count := 0
	for _, record := range p.records {
		if record.state.IsOpen() {
			count++
		}
	}
	return count
}

// Fingerprints returns the finding identities in first-seen order.
func (p *FindingProjection) Fingerprints() []FindingFingerprint {
	out := make([]FindingFingerprint, len(p.order))
	copy(out, p.order)
	return out
}

// History returns the preserved ordered observations for one finding.
func (p *FindingProjection) History(fingerprint FindingFingerprint) []ObservationEvent {
	record, ok := p.records[fingerprint]
	if !ok {
		return nil
	}
	out := make([]ObservationEvent, len(record.observations))
	copy(out, record.observations)
	return out
}
