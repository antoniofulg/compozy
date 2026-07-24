package convergence

import "fmt"

// SequenceGuard admits canonical updates in strict monotonic order. It rejects a
// duplicate sequence and an out-of-order sequence so replayed or reordered phase
// completions cannot corrupt the projection.
type SequenceGuard struct {
	last    uint64
	started bool
	seen    map[uint64]struct{}
}

// NewSequenceGuard returns an empty guard.
func NewSequenceGuard() *SequenceGuard {
	return &SequenceGuard{seen: make(map[uint64]struct{})}
}

// Admit accepts one sequence. A previously seen sequence is a duplicate; a
// sequence at or below the last admitted is out of order. Both are rejected.
func (g *SequenceGuard) Admit(seq uint64) error {
	if _, ok := g.seen[seq]; ok {
		return fmt.Errorf("%w: duplicate sequence %d", ErrReplayConflict, seq)
	}
	if g.started && seq <= g.last {
		return fmt.Errorf("%w: out-of-order sequence %d after %d", ErrReplayConflict, seq, g.last)
	}
	g.seen[seq] = struct{}{}
	g.last = seq
	g.started = true
	return nil
}

// BatchGuard enforces sequential correction batches within one worktree. Only one
// batch may be active at a time, and a completed batch never restarts.
type BatchGuard struct {
	active    string
	completed map[string]struct{}
}

// NewBatchGuard returns an empty guard.
func NewBatchGuard() *BatchGuard {
	return &BatchGuard{completed: make(map[string]struct{})}
}

// Start begins a batch. It rejects a concurrent batch while another is active and
// a restart of an already completed batch.
func (g *BatchGuard) Start(batchID string) error {
	if _, done := g.completed[batchID]; done {
		return fmt.Errorf("%w: batch %q already completed", ErrReplayConflict, batchID)
	}
	if g.active != "" && g.active != batchID {
		return fmt.Errorf("%w: batch %q cannot start while %q is active", ErrReplayConflict, batchID, g.active)
	}
	g.active = batchID
	return nil
}

// Complete finishes the active batch. It rejects completing a batch that is not
// the active one.
func (g *BatchGuard) Complete(batchID string) error {
	if g.active != batchID {
		return fmt.Errorf("%w: batch %q is not the active batch", ErrReplayConflict, batchID)
	}
	g.active = ""
	g.completed[batchID] = struct{}{}
	return nil
}

// RecoveryClass classifies durable phase evidence for recovery.
type RecoveryClass string

const (
	// RecoveryStartable means no phase record exists and the phase may start.
	RecoveryStartable RecoveryClass = "startable"
	// RecoveryRetryable means an incomplete attempt may retry within limits.
	RecoveryRetryable RecoveryClass = "retryable"
	// RecoveryReplayable means a completed result may replay without a side effect.
	RecoveryReplayable RecoveryClass = "replayable"
	// RecoveryUnknown means evidence is insufficient or conflicting.
	RecoveryUnknown RecoveryClass = "unknown"
)

// AdmitRetry reports whether a phase may repeat given its recovery class. Unknown
// work is never granted a free retry.
func AdmitRetry(class RecoveryClass) error {
	switch class {
	case RecoveryStartable, RecoveryRetryable, RecoveryReplayable:
		return nil
	case RecoveryUnknown:
		return fmt.Errorf("%w: unknown work is not granted a free retry", ErrReplayConflict)
	default:
		return fmt.Errorf("%w: unrecognized recovery class %q", ErrReplayConflict, class)
	}
}
