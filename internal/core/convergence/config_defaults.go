package convergence

import "time"

// Reserved and default identifiers.
const (
	// DefaultProfileName is the reserved built-in profile and implicit inheritance
	// name. Users cannot redefine it.
	DefaultProfileName = "default"
)

// Built-in default profile limit values.
const (
	// DefaultMaxReviewRounds is the built-in maximum review rounds.
	DefaultMaxReviewRounds = 6
	// DefaultMaxFindingAttempts is the built-in maximum correction attempts per finding.
	DefaultMaxFindingAttempts = 3
	// DefaultMaxVerificationAttempts is the built-in maximum attempts per verification failure.
	DefaultMaxVerificationAttempts = 3
	// DefaultNoProgressRounds is the built-in consecutive no-progress round limit.
	DefaultNoProgressRounds = 2
	// DefaultReviewAdmissionTimeout is the built-in review admission boundary.
	DefaultReviewAdmissionTimeout = 90 * time.Minute
	// DefaultOscillationCycles is the built-in oscillation cycle limit.
	DefaultOscillationCycles = 2
)

// Limits is one resolved, immutable convergence profile. All values are positive
// and satisfy the cross-field bounds enforced during validation.
type Limits struct {
	MaxReviewRounds         int
	MaxFindingAttempts      int
	MaxVerificationAttempts int
	NoProgressRounds        int
	ReviewAdmissionTimeout  time.Duration
	OscillationCycles       int
}

// BuiltinDefaultProfile returns the immutable built-in `default` profile.
func BuiltinDefaultProfile() Limits {
	return Limits{
		MaxReviewRounds:         DefaultMaxReviewRounds,
		MaxFindingAttempts:      DefaultMaxFindingAttempts,
		MaxVerificationAttempts: DefaultMaxVerificationAttempts,
		NoProgressRounds:        DefaultNoProgressRounds,
		ReviewAdmissionTimeout:  DefaultReviewAdmissionTimeout,
		OscillationCycles:       DefaultOscillationCycles,
	}
}
