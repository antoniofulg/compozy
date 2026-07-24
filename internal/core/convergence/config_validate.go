package convergence

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/compozy/compozy/internal/core/agent"
)

// namePattern is the required slug for user-defined profile and model setup names.
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// reasoningEffortValues is the accepted reasoning effort set, mirroring the
// workspace runtime contract without importing it (which would create a cycle).
var reasoningEffortValues = map[string]struct{}{
	"low": {}, "medium": {}, "high": {}, "xhigh": {}, "max": {}, "ultra": {},
}

const reasoningEffortList = "low, medium, high, xhigh, max, ultra"

// Validate checks one convergence configuration source for exact field errors
// before any side effect. It validates names, limits, durations, route fields,
// verification argv, severity keys, and the reserved `default` name. Existence of
// a selected profile or setup is a preflight concern handled by Freeze.
func (c Config) Validate(scope string) error {
	if c.IsZero() {
		return nil
	}
	if err := validateSelection(scope, "convergence.profile", c.Profile); err != nil {
		return err
	}
	if err := validateSelection(scope, "convergence.model_setup", c.ModelSetup); err != nil {
		return err
	}
	if err := validateVerification(scope, c.Verification); err != nil {
		return err
	}
	if err := validateProfiles(scope, c.Profiles); err != nil {
		return err
	}
	return validateModelSetups(scope, c.ModelSetups)
}

func validateSelection(scope, field string, value *string) error {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return configError(scope, field, "cannot be empty")
	}
	if trimmed == DefaultProfileName || namePattern.MatchString(trimmed) {
		return nil
	}
	return configError(scope, field, fmt.Sprintf("must be %q or match %s", DefaultProfileName, namePattern.String()))
}

func validateVerification(scope string, cfg VerificationConfig) error {
	if cfg.Command == nil {
		return nil
	}
	command := *cfg.Command
	if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		return configError(scope, "convergence.verification.command", "requires a nonblank executable")
	}
	for idx, arg := range command {
		if strings.ContainsRune(arg, 0) {
			return configError(
				scope,
				fmt.Sprintf("convergence.verification.command[%d]", idx),
				"must not contain NUL bytes",
			)
		}
	}
	return nil
}

func validateProfiles(scope string, profiles map[string]ProfileConfig) error {
	for _, name := range sortedKeys(profiles) {
		field := fmt.Sprintf("convergence.profiles.%s", name)
		if err := validateName(scope, field, name); err != nil {
			return err
		}
		if err := validateProfileLimits(scope, field, profiles[name]); err != nil {
			return err
		}
	}
	return nil
}

func validateModelSetups(scope string, setups map[string]ModelSetupConfig) error {
	for _, name := range sortedKeys(setups) {
		field := fmt.Sprintf("convergence.model_setups.%s", name)
		if err := validateName(scope, field, name); err != nil {
			return err
		}
		if err := validateModelSetup(scope, field, setups[name]); err != nil {
			return err
		}
	}
	return nil
}

func validateName(scope, field, name string) error {
	if name == DefaultProfileName {
		return configError(scope, field, fmt.Sprintf("%q is reserved and cannot be redefined", DefaultProfileName))
	}
	if !namePattern.MatchString(name) {
		return configError(scope, field, fmt.Sprintf("name must match %s", namePattern.String()))
	}
	return nil
}

func configError(scope, field, message string) error {
	return fmt.Errorf("%w: %s %s %s", ErrConfigInvalid, scope, field, message)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// validateReasoningEffort accepts an optional reasoning effort against the shared set.
func validateReasoningEffort(scope, field string, value *string) error {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if _, ok := reasoningEffortValues[trimmed]; ok {
		return nil
	}
	return configError(scope, field, fmt.Sprintf("must be one of %s (got %q)", reasoningEffortList, trimmed))
}

// validateIDE accepts an optional interface against the driver catalog.
func validateIDE(scope, field string, value *string) error {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return configError(scope, field, "cannot be empty")
	}
	if _, err := agent.DriverCatalogEntryForIDE(trimmed); err != nil {
		return configError(scope, field, err.Error())
	}
	return nil
}

// validateDuration parses an optional positive Go duration and returns it.
func validateDuration(scope, field string, value *string) (time.Duration, error) {
	if value == nil {
		return 0, nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return 0, configError(scope, field, "cannot be empty")
	}
	duration, err := time.ParseDuration(trimmed)
	if err != nil {
		return 0, configError(scope, field, err.Error())
	}
	if duration <= 0 {
		return 0, configError(scope, field, fmt.Sprintf("must be greater than zero (got %s)", trimmed))
	}
	return duration, nil
}
