package convergence

import "fmt"

// validateProfileLimits checks a single profile entry for field-local validity:
// every present integer limit must be positive, the admission timeout must parse
// as a positive duration, and any explicit max_review_rounds bounds the
// no_progress and oscillation limits. The selected effective profile is
// re-validated with defaults applied during Freeze.
func validateProfileLimits(scope, field string, cfg ProfileConfig) error {
	positives := []struct {
		name  string
		value *int
	}{
		{"max_review_rounds", cfg.MaxReviewRounds},
		{"max_finding_attempts", cfg.MaxFindingAttempts},
		{"max_verification_attempts", cfg.MaxVerificationAttempts},
		{"no_progress_rounds", cfg.NoProgressRounds},
		{"oscillation_cycles", cfg.OscillationCycles},
	}
	for _, entry := range positives {
		if err := requirePositive(scope, field+"."+entry.name, entry.value); err != nil {
			return err
		}
	}
	if _, err := validateDuration(scope, field+".review_admission_timeout", cfg.ReviewAdmissionTimeout); err != nil {
		return err
	}
	return validateProfileBounds(scope, field, cfg)
}

func requirePositive(scope, field string, value *int) error {
	if value == nil {
		return nil
	}
	if *value <= 0 {
		return configError(scope, field, fmt.Sprintf("must be greater than zero (got %d)", *value))
	}
	return nil
}

// validateProfileBounds enforces no_progress_rounds <= max_review_rounds and
// oscillation_cycles <= max_review_rounds when max_review_rounds is set in the
// same entry.
func validateProfileBounds(scope, field string, cfg ProfileConfig) error {
	if cfg.MaxReviewRounds == nil {
		return nil
	}
	maxRounds := *cfg.MaxReviewRounds
	bounded := []struct {
		name  string
		value *int
	}{
		{"no_progress_rounds", cfg.NoProgressRounds},
		{"oscillation_cycles", cfg.OscillationCycles},
	}
	for _, entry := range bounded {
		if entry.value != nil && *entry.value > maxRounds {
			return configError(
				scope,
				field+"."+entry.name,
				fmt.Sprintf("cannot exceed max_review_rounds (%d > %d)", *entry.value, maxRounds),
			)
		}
	}
	return nil
}
