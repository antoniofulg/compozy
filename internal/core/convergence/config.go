package convergence

// Config is the `[convergence]` workspace configuration. It is optional and only
// required once convergence is selected. Every field uses pointer or map
// semantics so an unset value can be distinguished from an explicit zero and
// merged field-by-field across global and workspace sources.
type Config struct {
	// Profile selects a named limit profile. Empty selects the built-in default.
	Profile *string `toml:"profile,omitempty"`
	// ModelSetup selects a named model setup. Empty inherits the task route.
	ModelSetup *string `toml:"model_setup,omitempty"`
	// Verification is the mandatory authoritative verification command.
	Verification VerificationConfig `toml:"verification,omitempty"`
	// Profiles holds user-defined limit profiles keyed by name.
	Profiles map[string]ProfileConfig `toml:"profiles,omitempty"`
	// ModelSetups holds user-defined phase routes keyed by name.
	ModelSetups map[string]ModelSetupConfig `toml:"model_setups,omitempty"`
}

// VerificationConfig holds the explicit argv command executed to verify the
// repository. It is never inferred and never shares conflict-resolution config.
type VerificationConfig struct {
	// Command is the argv array executed directly in the target worktree root.
	Command *[]string `toml:"command,omitempty"`
}

// ProfileConfig declares bounded convergence limits. All limits are optional at
// the type level; validation enforces positivity and cross-field bounds.
type ProfileConfig struct {
	// MaxReviewRounds bounds review rounds.
	MaxReviewRounds *int `toml:"max_review_rounds,omitempty"`
	// MaxFindingAttempts bounds correction attempts per finding.
	MaxFindingAttempts *int `toml:"max_finding_attempts,omitempty"`
	// MaxVerificationAttempts bounds correction attempts per verification failure.
	MaxVerificationAttempts *int `toml:"max_verification_attempts,omitempty"`
	// NoProgressRounds bounds consecutive no-progress rounds before parking.
	NoProgressRounds *int `toml:"no_progress_rounds,omitempty"`
	// ReviewAdmissionTimeout is the review-loop admission boundary as a Go duration.
	ReviewAdmissionTimeout *string `toml:"review_admission_timeout,omitempty"`
	// OscillationCycles bounds disappearance-and-return cycles before parking.
	OscillationCycles *int `toml:"oscillation_cycles,omitempty"`
}

// RouteConfig is one runtime route: interface, model alias, and reasoning effort.
// It has no nested fallback or severity map, which structurally forbids fallback
// chains and severity nesting.
type RouteConfig struct {
	// IDE names the runtime interface.
	IDE *string `toml:"ide,omitempty"`
	// Model names the model alias.
	Model *string `toml:"model,omitempty"`
	// ReasoningEffort names the reasoning effort.
	ReasoningEffort *string `toml:"reasoning_effort,omitempty"`
}

// ReviewSetupConfig is the review phase route with one optional fallback.
type ReviewSetupConfig struct {
	RouteConfig
	// Fallback is the single optional fallback used only before the phase starts.
	Fallback *RouteConfig `toml:"fallback,omitempty"`
}

// CorrectionSetupConfig is the correction phase route with one optional fallback
// and optional per-severity overrides.
type CorrectionSetupConfig struct {
	RouteConfig
	// Fallback is the single optional fallback used only before the phase starts.
	Fallback *RouteConfig `toml:"fallback,omitempty"`
	// BySeverity overrides the route for the highest severity in a batch.
	BySeverity map[string]RouteConfig `toml:"by_severity,omitempty"`
}

// ModelSetupConfig routes the review and correction phases. It never configures
// task execution, which keeps its own routing.
type ModelSetupConfig struct {
	// Review routes the read-only review phase.
	Review ReviewSetupConfig `toml:"review,omitempty"`
	// Correction routes the correction phase.
	Correction CorrectionSetupConfig `toml:"correction,omitempty"`
}

// IsZero reports whether the config carries no convergence configuration at all.
func (c Config) IsZero() bool {
	return c.Profile == nil &&
		c.ModelSetup == nil &&
		c.Verification.Command == nil &&
		len(c.Profiles) == 0 &&
		len(c.ModelSetups) == 0
}
