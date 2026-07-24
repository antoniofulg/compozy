package convergence

// Merge overlays workspace convergence configuration onto global configuration.
// Fields merge within the same named profile or model setup; selecting one
// profile never merges it with another. The result is a fresh deep copy so
// callers cannot mutate either source through it.
func Merge(global, workspace Config) Config {
	return Config{
		Profile:      overlayPtr(global.Profile, workspace.Profile),
		ModelSetup:   overlayPtr(global.ModelSetup, workspace.ModelSetup),
		Verification: mergeVerification(global.Verification, workspace.Verification),
		Profiles:     mergeProfiles(global.Profiles, workspace.Profiles),
		ModelSetups:  mergeModelSetups(global.ModelSetups, workspace.ModelSetups),
	}
}

func mergeVerification(base, overlay VerificationConfig) VerificationConfig {
	return VerificationConfig{Command: overlaySlicePtr(base.Command, overlay.Command)}
}

func mergeProfiles(base, overlay map[string]ProfileConfig) map[string]ProfileConfig {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}
	merged := make(map[string]ProfileConfig)
	for name, cfg := range base {
		merged[name] = cfg
	}
	for name, cfg := range overlay {
		merged[name] = mergeProfile(merged[name], cfg)
	}
	return merged
}

func mergeProfile(base, overlay ProfileConfig) ProfileConfig {
	return ProfileConfig{
		MaxReviewRounds:         overlayPtr(base.MaxReviewRounds, overlay.MaxReviewRounds),
		MaxFindingAttempts:      overlayPtr(base.MaxFindingAttempts, overlay.MaxFindingAttempts),
		MaxVerificationAttempts: overlayPtr(base.MaxVerificationAttempts, overlay.MaxVerificationAttempts),
		NoProgressRounds:        overlayPtr(base.NoProgressRounds, overlay.NoProgressRounds),
		ReviewAdmissionTimeout:  overlayPtr(base.ReviewAdmissionTimeout, overlay.ReviewAdmissionTimeout),
		OscillationCycles:       overlayPtr(base.OscillationCycles, overlay.OscillationCycles),
	}
}

func mergeModelSetups(base, overlay map[string]ModelSetupConfig) map[string]ModelSetupConfig {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}
	merged := make(map[string]ModelSetupConfig)
	for name, cfg := range base {
		merged[name] = cfg
	}
	for name, cfg := range overlay {
		merged[name] = mergeModelSetup(merged[name], cfg)
	}
	return merged
}

func mergeModelSetup(base, overlay ModelSetupConfig) ModelSetupConfig {
	return ModelSetupConfig{
		Review: ReviewSetupConfig{
			RouteConfig: mergeRoute(base.Review.RouteConfig, overlay.Review.RouteConfig),
			Fallback:    mergeRoutePtr(base.Review.Fallback, overlay.Review.Fallback),
		},
		Correction: CorrectionSetupConfig{
			RouteConfig: mergeRoute(base.Correction.RouteConfig, overlay.Correction.RouteConfig),
			Fallback:    mergeRoutePtr(base.Correction.Fallback, overlay.Correction.Fallback),
			BySeverity:  mergeSeverityRoutes(base.Correction.BySeverity, overlay.Correction.BySeverity),
		},
	}
}

func mergeRoute(base, overlay RouteConfig) RouteConfig {
	return RouteConfig{
		IDE:             overlayPtr(base.IDE, overlay.IDE),
		Model:           overlayPtr(base.Model, overlay.Model),
		ReasoningEffort: overlayPtr(base.ReasoningEffort, overlay.ReasoningEffort),
	}
}

func mergeRoutePtr(base, overlay *RouteConfig) *RouteConfig {
	if base == nil && overlay == nil {
		return nil
	}
	baseRoute := RouteConfig{}
	if base != nil {
		baseRoute = *base
	}
	overlayRoute := RouteConfig{}
	if overlay != nil {
		overlayRoute = *overlay
	}
	merged := mergeRoute(baseRoute, overlayRoute)
	return &merged
}

func mergeSeverityRoutes(base, overlay map[string]RouteConfig) map[string]RouteConfig {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}
	merged := make(map[string]RouteConfig)
	for key, route := range base {
		merged[key] = route
	}
	for key, route := range overlay {
		merged[key] = mergeRoute(merged[key], route)
	}
	return merged
}
