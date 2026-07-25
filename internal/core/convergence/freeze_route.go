package convergence

import "strings"

// ReviewRoute resolves the review phase route. Review has no severity overrides,
// so each field resolves through resume override, setup base, then the inherited
// base execution route.
func (f FrozenConfiguration) ReviewRoute(resume *RouteConfig) ResolvedRoute {
	if f.Review.Role != "" {
		return applyRouteOverride(f.Review, resume)
	}
	var setupBase, fallback *RouteConfig
	if !f.inheritsRoute {
		base := f.setup.Review.RouteConfig
		setupBase = &base
		fallback = f.setup.Review.Fallback
	}
	return f.buildRoute(RoleReview, resume, nil, setupBase, fallback)
}

// CorrectionRoute resolves the correction phase route for the highest severity in
// a batch. Each field resolves through resume override, the severity override,
// setup base, then the inherited base execution route.
func (f FrozenConfiguration) CorrectionRoute(highest Severity, resume *RouteConfig) ResolvedRoute {
	if route, ok := f.Correction[highest]; ok {
		return applyRouteOverride(route, resume)
	}
	var setupBase, severity, fallback *RouteConfig
	if !f.inheritsRoute {
		base := f.setup.Correction.RouteConfig
		setupBase = &base
		fallback = f.setup.Correction.Fallback
		if override, ok := f.setup.Correction.BySeverity[string(highest)]; ok {
			severity = &override
		}
	}
	return f.buildRoute(RoleCorrection, resume, severity, setupBase, fallback)
}

func applyRouteOverride(base ResolvedRoute, override *RouteConfig) ResolvedRoute {
	if override == nil {
		return base
	}
	result := base
	if override.IDE != nil && strings.TrimSpace(*override.IDE) != "" {
		result.Primary.IDE = strings.TrimSpace(*override.IDE)
		result.Sources.IDE = SourceResumeOverride
	}
	if override.Model != nil && strings.TrimSpace(*override.Model) != "" {
		result.Primary.Model = strings.TrimSpace(*override.Model)
		result.Sources.Model = SourceResumeOverride
	}
	if override.ReasoningEffort != nil && strings.TrimSpace(*override.ReasoningEffort) != "" {
		result.Primary.ReasoningEffort = strings.TrimSpace(*override.ReasoningEffort)
		result.Sources.ReasoningEffort = SourceResumeOverride
	}
	return result
}

func (f FrozenConfiguration) buildRoute(
	role RouteRole,
	resume, severity, setupBase, fallback *RouteConfig,
) ResolvedRoute {
	ide, ideSource := resolveRouteField(
		candidates(resume, severity, setupBase, ideOf), f.BaseRoute.IDE, SourceTaskRoute)
	model, modelSource := resolveRouteField(
		candidates(resume, severity, setupBase, modelOf), f.BaseRoute.Model, SourceTaskRoute)
	effort, effortSource := resolveRouteField(
		candidates(resume, severity, setupBase, effortOf), f.BaseRoute.ReasoningEffort, SourceTaskRoute)
	resolved := ResolvedRoute{
		Role:    role,
		Primary: Route{IDE: ide, Model: model, ReasoningEffort: effort},
		Sources: RouteSources{IDE: ideSource, Model: modelSource, ReasoningEffort: effortSource},
	}
	if fallback != nil {
		resolved.Fallback = resolveFallbackRoute(*fallback, resolved.Primary)
		resolved.HasFallback = true
	}
	return resolved
}

func candidates(resume, severity, setupBase *RouteConfig, pick func(RouteConfig) *string) []fieldCandidate {
	var out []fieldCandidate
	if resume != nil {
		out = append(out, fieldCandidate{value: pick(*resume), source: SourceResumeOverride})
	}
	if severity != nil {
		out = append(out, fieldCandidate{value: pick(*severity), source: SourceSeverityOverride})
	}
	if setupBase != nil {
		out = append(out, fieldCandidate{value: pick(*setupBase), source: SourceSetupBase})
	}
	return out
}

func resolveFallbackRoute(fallback RouteConfig, primary Route) *Route {
	resolved := Route{
		IDE:             fallbackField(fallback.IDE, primary.IDE),
		Model:           fallbackField(fallback.Model, primary.Model),
		ReasoningEffort: fallbackField(fallback.ReasoningEffort, primary.ReasoningEffort),
	}
	return &resolved
}

func fallbackField(value *string, primary string) string {
	if value != nil {
		if trimmed := strings.TrimSpace(*value); trimmed != "" {
			return trimmed
		}
	}
	return primary
}

func ideOf(route RouteConfig) *string    { return route.IDE }
func modelOf(route RouteConfig) *string  { return route.Model }
func effortOf(route RouteConfig) *string { return route.ReasoningEffort }
