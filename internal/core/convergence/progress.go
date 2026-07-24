package convergence

// ProgressSignals captures the three measurable improvements that count as
// progress in one round. Code changes alone are never progress.
type ProgressSignals struct {
	// FindingResolved is true when an accepted finding became resolved.
	FindingResolved bool
	// SeverityDecreased is true when an accepted finding's severity dropped.
	SeverityDecreased bool
	// VerificationGatePassed is true when a previously failing gate now passes.
	VerificationGatePassed bool
}

// MadeProgress reports whether any measurable improvement occurred.
func (s ProgressSignals) MadeProgress() bool {
	return s.FindingResolved || s.SeverityDecreased || s.VerificationGatePassed
}

// ProgressLedger accumulates consecutive no-progress rounds. Evaluation is
// idempotent per round ordinal so a duplicated evaluation event does not
// double-count.
type ProgressLedger struct {
	consecutiveNoProgress int
	processed             map[int]struct{}
}

// NewProgressLedger returns an empty ledger.
func NewProgressLedger() *ProgressLedger {
	return &ProgressLedger{processed: make(map[int]struct{})}
}

// Record applies the round's progress signals. Measurable progress resets the
// no-progress counter; its absence increments it. A round already recorded is a
// no-op and reports applied=false, keeping the counter idempotent under replay.
func (l *ProgressLedger) Record(round int, signals ProgressSignals) (consecutive int, applied bool) {
	if _, seen := l.processed[round]; seen {
		return l.consecutiveNoProgress, false
	}
	l.processed[round] = struct{}{}
	if signals.MadeProgress() {
		l.consecutiveNoProgress = 0
	} else {
		l.consecutiveNoProgress++
	}
	return l.consecutiveNoProgress, true
}

// ConsecutiveNoProgress returns the current consecutive no-progress count.
func (l *ProgressLedger) ConsecutiveNoProgress() int { return l.consecutiveNoProgress }

// NoProgressReached reports whether the consecutive no-progress count reached the
// configured limit. A non-positive limit never trips.
func (l *ProgressLedger) NoProgressReached(limit int) bool {
	return limit > 0 && l.consecutiveNoProgress >= limit
}
