package convergence

// AttemptLedger counts bounded correction attempts per stable key. It backs both
// finding correction attempts (keyed by semantic fingerprint) and verification
// failure attempts (keyed by failure fingerprint). Recording is idempotent per
// attempt identity so replayed events never inflate the count.
type AttemptLedger struct {
	counts    map[string]int
	recorded  map[string]struct{}
	limit     int
	unlimited bool
}

// NewAttemptLedger returns a ledger that treats a key as exhausted once its
// recorded attempts reach limit. A non-positive limit is treated as no bound.
func NewAttemptLedger(limit int) *AttemptLedger {
	return &AttemptLedger{
		counts:    make(map[string]int),
		recorded:  make(map[string]struct{}),
		limit:     limit,
		unlimited: limit <= 0,
	}
}

// Record counts one attempt for key. attemptID makes the record idempotent: the
// same attempt identity is counted at most once. It returns the resulting count
// and whether the record was newly applied.
func (l *AttemptLedger) Record(key, attemptID string) (count int, applied bool) {
	dedupe := key + "\x00" + attemptID
	if _, seen := l.recorded[dedupe]; seen {
		return l.counts[key], false
	}
	l.recorded[dedupe] = struct{}{}
	l.counts[key]++
	return l.counts[key], true
}

// Count returns the recorded attempt count for key.
func (l *AttemptLedger) Count(key string) int { return l.counts[key] }

// Exhausted reports whether key reached the configured attempt limit.
func (l *AttemptLedger) Exhausted(key string) bool {
	if l.unlimited {
		return false
	}
	return l.counts[key] >= l.limit
}

// AnyExhausted reports whether any recorded key reached the attempt limit.
func (l *AttemptLedger) AnyExhausted() bool {
	if l.unlimited {
		return false
	}
	for _, count := range l.counts {
		if count >= l.limit {
			return true
		}
	}
	return false
}
