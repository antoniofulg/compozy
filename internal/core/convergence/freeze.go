package convergence

import (
	"fmt"
	"strings"
)

// ProfileSources records the resolution source of each profile limit.
type ProfileSources struct {
	MaxReviewRounds         Source
	MaxFindingAttempts      Source
	MaxVerificationAttempts Source
	NoProgressRounds        Source
	ReviewAdmissionTimeout  Source
	OscillationCycles       Source
}

// FrozenConfiguration is the immutable, fully explained convergence configuration
// resolved at preflight. It is a deep value copy, so later edits to the source
// configuration files cannot change a frozen run.
type FrozenConfiguration struct {
	ProfileName        string
	ModelSetupName     string
	Limits             Limits
	LimitSources       ProfileSources
	Verification       []string
	VerificationSource Source
	BaseRoute          Route
	Review             ResolvedRoute
	Correction         map[Severity]ResolvedRoute
	AutoCommit         bool
	Warnings           []string

	setup         ModelSetupConfig
	inheritsRoute bool
}

// FreezeInput carries every source needed to freeze one convergence run.
type FreezeInput struct {
	// Global is the global-scope convergence configuration.
	Global Config
	// Workspace is the workspace-scope convergence configuration.
	Workspace Config
	// BaseRoute is the frozen base execution route inherited from the task or Task
	// Group run. Convergence never recomputes it.
	BaseRoute Route
	// AutoCommit reflects the resolved auto_commit setting for the warning contract.
	AutoCommit bool
	// AllowedIDEs optionally restricts which interfaces the run is authorized to
	// launch. An empty list authorizes every validated interface.
	AllowedIDEs []string
}

// Freeze validates the merged configuration, resolves the selected profile and
// model setup with per-field provenance, and produces the immutable frozen
// result. It rejects a missing verification command, an unknown selection, a
// route beyond the run's authority, and any field-local configuration error.
func Freeze(input FreezeInput) (FrozenConfiguration, error) {
	merged := Merge(input.Global, input.Workspace)
	if err := merged.Validate("convergence"); err != nil {
		return FrozenConfiguration{}, err
	}
	verification, verificationSource, err := resolveVerification(input.Global, input.Workspace)
	if err != nil {
		return FrozenConfiguration{}, err
	}
	profileName := selectionName(merged.Profile)
	limits, limitSources, err := resolveProfile(input.Global, input.Workspace, profileName)
	if err != nil {
		return FrozenConfiguration{}, err
	}
	setupName, setup, inherits, err := resolveSetup(merged)
	if err != nil {
		return FrozenConfiguration{}, err
	}
	frozen := FrozenConfiguration{
		ProfileName:        effectiveProfileName(profileName),
		ModelSetupName:     setupName,
		Limits:             limits,
		LimitSources:       limitSources,
		Verification:       verification,
		VerificationSource: verificationSource,
		BaseRoute:          input.BaseRoute,
		AutoCommit:         input.AutoCommit,
		setup:              setup,
		inheritsRoute:      inherits,
	}
	if err := frozen.checkAuthority(input.AllowedIDEs); err != nil {
		return FrozenConfiguration{}, err
	}
	frozen.Review = frozen.ReviewRoute(nil)
	frozen.Correction = make(map[Severity]ResolvedRoute, 4)
	for _, severity := range []Severity{SeverityCritical, SeverityHigh, SeverityMedium, SeverityLow} {
		frozen.Correction[severity] = frozen.CorrectionRoute(severity, nil)
	}
	frozen.Warnings = frozen.buildWarnings(input.AutoCommit)
	return frozen, nil
}

func resolveVerification(global, workspace Config) ([]string, Source, error) {
	if workspace.Verification.Command != nil {
		return cloneStrings(*workspace.Verification.Command), SourceWorkspace, nil
	}
	if global.Verification.Command != nil {
		return cloneStrings(*global.Verification.Command), SourceGlobal, nil
	}
	return nil, "", fmt.Errorf("%w: [convergence.verification].command must be set", ErrVerificationRequired)
}

func selectionName(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func effectiveProfileName(name string) string {
	if name == "" {
		return DefaultProfileName
	}
	return name
}

func (f FrozenConfiguration) buildWarnings(autoCommit bool) []string {
	var warnings []string
	if !autoCommit {
		warnings = append(warnings,
			"auto_commit is disabled; convergence will not commit and the receipt will flag uncommitted work")
	}
	return warnings
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}
