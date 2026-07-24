package convergence

import (
	"errors"
	"testing"
)

func verificationCommand() VerificationConfig {
	return VerificationConfig{Command: ptr([]string{"make", "verify"})}
}

func TestFreezeMergeProvenance(t *testing.T) {
	t.Parallel()
	t.Run("Should merge fields within one profile and record per-field source", func(t *testing.T) {
		t.Parallel()
		global := Config{
			Profiles: map[string]ProfileConfig{
				"quality": {MaxReviewRounds: ptr(5)},
				"fast":    {MaxReviewRounds: ptr(2)}, // a second, unselected profile
			},
		}
		workspace := Config{
			Profile:      ptr("quality"),
			Verification: verificationCommand(),
			Profiles: map[string]ProfileConfig{
				"quality": {MaxFindingAttempts: ptr(4)},
			},
		}
		frozen, err := Freeze(FreezeInput{Global: global, Workspace: workspace})
		if err != nil {
			t.Fatalf("freeze: %v", err)
		}
		if frozen.Limits.MaxReviewRounds != 5 || frozen.LimitSources.MaxReviewRounds != SourceGlobal {
			t.Fatalf("expected max_review_rounds 5 from global, got %d/%s",
				frozen.Limits.MaxReviewRounds, frozen.LimitSources.MaxReviewRounds)
		}
		if frozen.Limits.MaxFindingAttempts != 4 || frozen.LimitSources.MaxFindingAttempts != SourceWorkspace {
			t.Fatalf("expected max_finding_attempts 4 from workspace, got %d/%s",
				frozen.Limits.MaxFindingAttempts, frozen.LimitSources.MaxFindingAttempts)
		}
		if frozen.Limits.OscillationCycles != DefaultOscillationCycles ||
			frozen.LimitSources.OscillationCycles != SourceBuiltinDefault {
			t.Fatal("expected unset field to fall back to the built-in default with default provenance")
		}
	})
	t.Run("Should not merge an unselected profile into the selected one", func(t *testing.T) {
		t.Parallel()
		cfg := Config{
			Profile:      ptr("quality"),
			Verification: verificationCommand(),
			Profiles: map[string]ProfileConfig{
				"quality": {MaxReviewRounds: ptr(5)},
				"fast":    {MaxFindingAttempts: ptr(9)},
			},
		}
		frozen, err := Freeze(FreezeInput{Workspace: cfg})
		if err != nil {
			t.Fatalf("freeze: %v", err)
		}
		if frozen.Limits.MaxFindingAttempts != DefaultMaxFindingAttempts {
			t.Fatalf("expected the unselected profile's field to be ignored, got %d",
				frozen.Limits.MaxFindingAttempts)
		}
	})
}

func TestFreezeExplanationFreezingAndWarnings(t *testing.T) {
	t.Parallel()
	t.Run("Should record the verification source", func(t *testing.T) {
		t.Parallel()
		frozen, err := Freeze(FreezeInput{Workspace: Config{Verification: verificationCommand()}})
		if err != nil {
			t.Fatalf("freeze: %v", err)
		}
		if frozen.VerificationSource != SourceWorkspace {
			t.Fatalf("expected workspace verification source, got %s", frozen.VerificationSource)
		}
	})
	t.Run("Should warn but permit auto_commit disabled", func(t *testing.T) {
		t.Parallel()
		frozen, err := Freeze(FreezeInput{Workspace: Config{Verification: verificationCommand()}, AutoCommit: false})
		if err != nil {
			t.Fatalf("freeze: %v", err)
		}
		if len(frozen.Warnings) == 0 {
			t.Fatal("expected an auto_commit warning")
		}
	})
	t.Run("Should freeze config against later source changes", func(t *testing.T) {
		t.Parallel()
		workspace := Config{Verification: verificationCommand(), Profile: ptr("quality"),
			Profiles: map[string]ProfileConfig{"quality": {MaxReviewRounds: ptr(5)}}}
		frozen, err := Freeze(FreezeInput{Workspace: workspace})
		if err != nil {
			t.Fatalf("freeze: %v", err)
		}
		(*workspace.Verification.Command)[0] = "rm"
		workspace.Profiles["quality"] = ProfileConfig{MaxReviewRounds: ptr(1)}
		if frozen.Verification[0] != "make" || frozen.Limits.MaxReviewRounds != 5 {
			t.Fatalf("frozen configuration changed after source mutation: %+v %v",
				frozen.Verification, frozen.Limits.MaxReviewRounds)
		}
	})
	t.Run("Should require a verification command", func(t *testing.T) {
		t.Parallel()
		_, err := Freeze(FreezeInput{Workspace: Config{Profile: ptr("default")}})
		if !errors.Is(err, ErrVerificationRequired) {
			t.Fatalf("expected ErrVerificationRequired, got %v", err)
		}
	})
}

func TestFreezeRejectsRoutesBeyondAuthority(t *testing.T) {
	t.Parallel()
	t.Run("Should reject a configured route interface outside the authority", func(t *testing.T) {
		t.Parallel()
		cfg := Config{
			ModelSetup:   ptr("balanced"),
			Verification: verificationCommand(),
			ModelSetups: map[string]ModelSetupConfig{
				"balanced": {Review: ReviewSetupConfig{RouteConfig: RouteConfig{IDE: ptr("claude")}}},
			},
		}
		_, err := Freeze(FreezeInput{Workspace: cfg, AllowedIDEs: []string{"codex"}})
		if !errors.Is(err, ErrRouteInvalid) {
			t.Fatalf("expected ErrRouteInvalid for unauthorized route, got %v", err)
		}
	})
}
