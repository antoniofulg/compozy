package convergence

import "fmt"

// findingRecord is the projected state and preserved history of one finding.
type findingRecord struct {
	fingerprint  FindingFingerprint
	state        FindingState
	severity     Severity
	snapshotSeq  uint64
	firstSeq     uint64
	observations []ObservationEvent
	dispositions []DispositionEvent
}

// FindingProjection is the ordered projection of observations and dispositions
// into current finding state. It is deterministic, idempotent under replay, and
// rejects stale updates. It performs no I/O.
type FindingProjection struct {
	records map[FindingFingerprint]*findingRecord
	order   []FindingFingerprint
	seenObs map[string]struct{}
	seenDec map[string]struct{}
}

// NewFindingProjection returns an empty projection.
func NewFindingProjection() *FindingProjection {
	return &FindingProjection{
		records: make(map[FindingFingerprint]*findingRecord),
		seenObs: make(map[string]struct{}),
		seenDec: make(map[string]struct{}),
	}
}

// ApplyObservation projects one observation. A repeated ObservationID is a
// deduplicated no-op. An observation bound to a superseded snapshot is rejected
// as stale and never changes current state. Applied history is preserved.
func (p *FindingProjection) ApplyObservation(event ObservationEvent) (applied bool, err error) {
	if event.Fingerprint == "" {
		return false, fmt.Errorf("%w: observation fingerprint is required", ErrReplayConflict)
	}
	if _, seen := p.seenObs[event.ObservationID]; seen && event.ObservationID != "" {
		return false, nil
	}
	record := p.record(event.Fingerprint, event.Sequence)
	if record.hasSnapshot() && event.SnapshotSeq < record.snapshotSeq {
		return false, fmt.Errorf("%w: observation snapshot %d precedes %d", ErrObservationStale,
			event.SnapshotSeq, record.snapshotSeq)
	}
	p.markObservation(event.ObservationID)
	record.observations = append(record.observations, event)
	record.snapshotSeq = event.SnapshotSeq
	if !record.isClosed() {
		record.state = observationState(event.Outcome)
		record.severity = event.Severity
	}
	return true, nil
}

// ApplyDisposition projects one disposition after enforcing authority, evidence,
// and snapshot freshness. A repeated DecisionID is a deduplicated no-op.
func (p *FindingProjection) ApplyDisposition(event DispositionEvent) (applied bool, err error) {
	record, ok := p.records[event.Fingerprint]
	if !ok {
		return false, fmt.Errorf("%w: disposition targets unknown finding", ErrReplayConflict)
	}
	if _, seen := p.seenDec[event.DecisionID]; seen && event.DecisionID != "" {
		return false, nil
	}
	if event.SnapshotSeq < record.snapshotSeq {
		return false, fmt.Errorf("%w: disposition snapshot %d precedes %d", ErrObservationStale,
			event.SnapshotSeq, record.snapshotSeq)
	}
	state, err := authorizeDisposition(event)
	if err != nil {
		return false, err
	}
	p.seenDec[event.DecisionID] = struct{}{}
	record.dispositions = append(record.dispositions, event)
	record.state = state
	return true, nil
}

func (p *FindingProjection) record(fingerprint FindingFingerprint, seq uint64) *findingRecord {
	if existing, ok := p.records[fingerprint]; ok {
		return existing
	}
	created := &findingRecord{fingerprint: fingerprint, state: FindingActionable, firstSeq: seq}
	p.records[fingerprint] = created
	p.order = append(p.order, fingerprint)
	return created
}

func (p *FindingProjection) markObservation(id string) {
	if id != "" {
		p.seenObs[id] = struct{}{}
	}
}

func (r *findingRecord) hasSnapshot() bool { return len(r.observations) > 0 }

func (r *findingRecord) isClosed() bool {
	switch r.state {
	case FindingInvalid, FindingDuplicate, FindingWaived:
		return true
	default:
		return false
	}
}

func observationState(outcome ObservationOutcome) FindingState {
	if outcome == ObservationResolved {
		return FindingResolved
	}
	return FindingActionable
}
