package convergence

import "strings"

// ReviewRoute resolves the review phase route. Review has no severity overrides,
// so each field resolves through resume override, setup base, then the inherited
// base execution route.
func (f FrozenConfiguration) ReviewRoute(resume *RouteConfig) ResolvedRoute {
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
