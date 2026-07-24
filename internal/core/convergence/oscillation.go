package convergence

// OscillationTracker counts disappearance-and-return cycles per semantic finding
// identity. Because identity is the semantic fingerprint, a finding that only
// moves lines keeps the same identity and never counts as a new cycle.
type OscillationTracker struct {
	states map[FindingFingerprint]*oscillationState
}

type oscillationState struct {
	present     bool
	disappeared bool
	cycles      int
}

// NewOscillationTracker returns an empty tracker.
func NewOscillationTracker() *OscillationTracker {
	return &OscillationTracker{states: make(map[FindingFingerprint]*oscillationState)}
}

// Observe records whether a finding is present in the current review. A cycle
// completes when a finding that had disappeared returns, and Observe returns the
// finding's completed cycle count. The first appearance and steady presence never
// add a cycle.
func (t *OscillationTracker) Observe(fingerprint FindingFingerprint, present bool) int {
	state, ok := t.states[fingerprint]
	if !ok {
		state = &oscillationState{}
		t.states[fingerprint] = state
	}
	switch {
	case present && state.disappeared:
		state.cycles++
		state.disappeared = false
		state.present = true
	case present:
		state.present = true
	case !present && state.present:
		state.disappeared = true
		state.present = false
	}
	return state.cycles
}

// Cycles returns the completed cycle count for one finding.
func (t *OscillationTracker) Cycles(fingerprint FindingFingerprint) int {
	if state, ok := t.states[fingerprint]; ok {
		return state.cycles
	}
	return 0
}

// MaxCycles returns the largest completed cycle count across all findings.
func (t *OscillationTracker) MaxCycles() int {
	highest := 0
	for _, state := range t.states {
		if state.cycles > highest {
			highest = state.cycles
		}
	}
	return highest
}

// Reached reports whether any finding reached the configured oscillation cycles.
// A non-positive limit never trips.
func (t *OscillationTracker) Reached(limit int) bool {
	return limit > 0 && t.MaxCycles() >= limit
}
