package convergence

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func ptr[T any](value T) *T { return &value }

func validProfile() ProfileConfig {
	return ProfileConfig{
		MaxReviewRounds:         ptr(4),
		MaxFindingAttempts:      ptr(2),
		MaxVerificationAttempts: ptr(2),
		NoProgressRounds:        ptr(2),
		ReviewAdmissionTimeout:  ptr("45m"),
		OscillationCycles:       ptr(1),
	}
}

func TestBuiltinDefaultProfile(t *testing.T) {
	t.Parallel()
	t.Run("Should match the documented default limits", func(t *testing.T) {
		t.Parallel()
		got := BuiltinDefaultProfile()
		want := Limits{
			MaxReviewRounds:         6,
			MaxFindingAttempts:      3,
			MaxVerificationAttempts: 3,
			NoProgressRounds:        2,
			ReviewAdmissionTimeout:  90 * time.Minute,
			OscillationCycles:       2,
		}
		if got != want {
			t.Fatalf("unexpected built-in default profile: %+v", got)
		}
	})
	t.Run("Should resolve to the built-in default when no profile is selected", func(t *testing.T) {
		t.Parallel()
		frozen, err := Freeze(FreezeInput{
			Workspace: Config{Verification: VerificationConfig{Command: ptr([]string{"make", "verify"})}},
		})
		if err != nil {
			t.Fatalf("freeze: %v", err)
		}
		if frozen.Limits != BuiltinDefaultProfile() {
			t.Fatalf("expected built-in default limits, got %+v", frozen.Limits)
		}
		if frozen.ProfileName != DefaultProfileName {
			t.Fatalf("expected default profile name, got %q", frozen.ProfileName)
		}
	})
}

func TestProfileLimitValidation(t *testing.T) {
	t.Parallel()
	t.Run("Should accept a fully specified positive custom profile", func(t *testing.T) {
		t.Parallel()
		cfg := Config{Profiles: map[string]ProfileConfig{"quality": validProfile()}}
		if err := cfg.Validate("workspace config"); err != nil {
			t.Fatalf("expected valid profile, got %v", err)
		}
	})
	invalid := map[string]ProfileConfig{
		"zero max_review_rounds":  {MaxReviewRounds: ptr(0)},
		"negative attempts":       {MaxFindingAttempts: ptr(-1)},
		"malformed duration":      {ReviewAdmissionTimeout: ptr("half an hour")},
		"no_progress over rounds": {MaxReviewRounds: ptr(3), NoProgressRounds: ptr(4)},
		"oscillation over rounds": {MaxReviewRounds: ptr(3), OscillationCycles: ptr(5)},
	}
	for name, profile := range invalid {
		t.Run("Should reject "+name, func(t *testing.T) {
			t.Parallel()
			cfg := Config{Profiles: map[string]ProfileConfig{"quality": profile}}
			if err := cfg.Validate("workspace config"); !errors.Is(err, ErrConfigInvalid) {
				t.Fatalf("expected ErrConfigInvalid for %s, got %v", name, err)
			}
		})
	}
}

func TestProfileResolvedBoundsWithDefaults(t *testing.T) {
	t.Parallel()
	t.Run("Should reject no_progress above the default max at freeze", func(t *testing.T) {
		t.Parallel()
		cfg := Config{
			Profile:      ptr("wide"),
			Verification: VerificationConfig{Command: ptr([]string{"make", "verify"})},
			Profiles:     map[string]ProfileConfig{"wide": {NoProgressRounds: ptr(8)}},
		}
		_, err := Freeze(FreezeInput{Workspace: cfg})
		if !errors.Is(err, ErrConfigInvalid) {
			t.Fatalf("expected freeze to reject no_progress above default max, got %v", err)
		}
	})
}

func TestProfileAndSetupNameValidation(t *testing.T) {
	t.Parallel()
	longName := "a" + strings.Repeat("b", 63) // 64 characters, the maximum
	valid := []string{"a", "quality", "q1", "with-dash", "with_underscore", longName}
	for _, name := range valid {
		t.Run("Should accept name "+name, func(t *testing.T) {
			t.Parallel()
			cfg := Config{Profiles: map[string]ProfileConfig{name: validProfile()}}
			if err := cfg.Validate("workspace config"); err != nil {
				t.Fatalf("expected %q accepted, got %v", name, err)
			}
		})
	}
	invalid := map[string]string{
		"uppercase":     "Quality",
		"leading digit": "1quality",
		"space":         "bad name",
		"overlength":    "a" + strings.Repeat("b", 64), // 65 characters
		"reserved":      DefaultProfileName,
	}
	for label, name := range invalid {
		t.Run("Should reject "+label+" name", func(t *testing.T) {
			t.Parallel()
			cfg := Config{ModelSetups: map[string]ModelSetupConfig{name: {}}}
			if err := cfg.Validate("workspace config"); !errors.Is(err, ErrConfigInvalid) {
				t.Fatalf("expected ErrConfigInvalid for %q, got %v", name, err)
			}
		})
	}
}
