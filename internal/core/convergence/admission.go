package convergence

import "time"

// RoundClock tracks the review admission boundary. The clock starts with the
// first review; task execution, initial verification, and any pre-review
// correction do not consume it. A round admitted before the boundary may finish
// after it, but no later round may start.
type RoundClock struct {
	// CompletedRounds counts review rounds that have already started.
	CompletedRounds int
	// MaxRounds is the profile's maximum review rounds.
	MaxRounds int
	// Elapsed is the review-loop time measured from the first review.
	Elapsed time.Duration
	// AdmissionTimeout is the profile's review admission boundary.
	AdmissionTimeout time.Duration
}

// CanAdmitRound reports whether another review round may start. When admission is
// denied it returns the precise parked reason, preferring max_rounds over
// time_limit so a run that exhausts both reports the first applicable boundary
// deterministically. A boundary reached exactly (elapsed == timeout) denies the
// next round.
func (c RoundClock) CanAdmitRound() (bool, ParkedReason) {
	if c.CompletedRounds >= c.MaxRounds {
		return false, ParkedMaxRounds
	}
	if c.AdmissionTimeout > 0 && c.Elapsed >= c.AdmissionTimeout {
		return false, ParkedTimeLimit
	}
	return true, ""
}

// MaxRoundsReached reports whether the round count alone forbids a new round.
func (c RoundClock) MaxRoundsReached() bool {
	return c.CompletedRounds >= c.MaxRounds
}

// TimeLimitReached reports whether the admission boundary alone forbids a new round.
func (c RoundClock) TimeLimitReached() bool {
	return c.AdmissionTimeout > 0 && c.Elapsed >= c.AdmissionTimeout
}
