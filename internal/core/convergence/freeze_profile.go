package convergence

import (
	"fmt"
	"time"
)

// resolveProfile resolves the selected profile's limits with per-field
// provenance. The reserved default profile resolves entirely from built-in
// values. A named profile resolves each field from workspace, then global, then
// the built-in default, and is validated with defaults applied.
func resolveProfile(global, workspace Config, name string) (Limits, ProfileSources, error) {
	if name == "" || name == DefaultProfileName {
		return BuiltinDefaultProfile(), builtinSources(), nil
	}
	gEntry, gOK := global.Profiles[name]
	wEntry, wOK := workspace.Profiles[name]
	if !gOK && !wOK {
		return Limits{}, ProfileSources{}, fmt.Errorf("%w: %q", ErrProfileNotFound, name)
	}
	limits, sources, err := mergeProfileLimits(gEntry, wEntry)
	if err != nil {
		return Limits{}, ProfileSources{}, err
	}
	if err := validateResolvedLimits(name, limits); err != nil {
		return Limits{}, ProfileSources{}, err
	}
	return limits, sources, nil
}

func mergeProfileLimits(global, workspace ProfileConfig) (Limits, ProfileSources, error) {
	defaults := BuiltinDefaultProfile()
	limits := Limits{}
	sources := ProfileSources{}
	limits.MaxReviewRounds, sources.MaxReviewRounds =
		resolveIntField(workspace.MaxReviewRounds, global.MaxReviewRounds, defaults.MaxReviewRounds)
	limits.MaxFindingAttempts, sources.MaxFindingAttempts =
		resolveIntField(workspace.MaxFindingAttempts, global.MaxFindingAttempts, defaults.MaxFindingAttempts)
	limits.MaxVerificationAttempts, sources.MaxVerificationAttempts =
		resolveIntField(
			workspace.MaxVerificationAttempts,
			global.MaxVerificationAttempts,
			defaults.MaxVerificationAttempts,
		)
	limits.NoProgressRounds, sources.NoProgressRounds =
		resolveIntField(workspace.NoProgressRounds, global.NoProgressRounds, defaults.NoProgressRounds)
	limits.OscillationCycles, sources.OscillationCycles =
		resolveIntField(workspace.OscillationCycles, global.OscillationCycles, defaults.OscillationCycles)
	timeout, timeoutSource, err := resolveDurationField(
		workspace.ReviewAdmissionTimeout,
		global.ReviewAdmissionTimeout,
		defaults.ReviewAdmissionTimeout,
	)
	if err != nil {
		return Limits{}, ProfileSources{}, err
	}
	limits.ReviewAdmissionTimeout = timeout
	sources.ReviewAdmissionTimeout = timeoutSource
	return limits, sources, nil
}

func resolveIntField(workspace, global *int, def int) (int, Source) {
	if workspace != nil {
		return *workspace, SourceWorkspace
	}
	if global != nil {
		return *global, SourceGlobal
	}
	return def, SourceBuiltinDefault
}

func resolveDurationField(workspace, global *string, def time.Duration) (time.Duration, Source, error) {
	switch {
	case workspace != nil:
		parsed, err := time.ParseDuration(*workspace)
		return parsed, SourceWorkspace, err
	case global != nil:
		parsed, err := time.ParseDuration(*global)
		return parsed, SourceGlobal, err
	default:
		return def, SourceBuiltinDefault, nil
	}
}

func builtinSources() ProfileSources {
	return ProfileSources{
		MaxReviewRounds:         SourceBuiltinDefault,
		MaxFindingAttempts:      SourceBuiltinDefault,
		MaxVerificationAttempts: SourceBuiltinDefault,
		NoProgressRounds:        SourceBuiltinDefault,
		ReviewAdmissionTimeout:  SourceBuiltinDefault,
		OscillationCycles:       SourceBuiltinDefault,
	}
}

// validateResolvedLimits enforces positivity and cross-field bounds on the fully
// resolved profile.
func validateResolvedLimits(name string, limits Limits) error {
	positives := []struct {
		field string
		value int
	}{
		{"max_review_rounds", limits.MaxReviewRounds},
		{"max_finding_attempts", limits.MaxFindingAttempts},
		{"max_verification_attempts", limits.MaxVerificationAttempts},
		{"no_progress_rounds", limits.NoProgressRounds},
		{"oscillation_cycles", limits.OscillationCycles},
	}
	for _, entry := range positives {
		if entry.value <= 0 {
			return fmt.Errorf("%w: profile %q %s must be greater than zero (got %d)",
				ErrConfigInvalid, name, entry.field, entry.value)
		}
	}
	if limits.ReviewAdmissionTimeout <= 0 {
		return fmt.Errorf("%w: profile %q review_admission_timeout must be positive", ErrConfigInvalid, name)
	}
	return validateResolvedBounds(name, limits)
}

func validateResolvedBounds(name string, limits Limits) error {
	if limits.NoProgressRounds > limits.MaxReviewRounds {
		return fmt.Errorf("%w: profile %q no_progress_rounds cannot exceed max_review_rounds (%d > %d)",
			ErrConfigInvalid, name, limits.NoProgressRounds, limits.MaxReviewRounds)
	}
	if limits.OscillationCycles > limits.MaxReviewRounds {
		return fmt.Errorf("%w: profile %q oscillation_cycles cannot exceed max_review_rounds (%d > %d)",
			ErrConfigInvalid, name, limits.OscillationCycles, limits.MaxReviewRounds)
	}
	return nil
}
