package convergence

import (
	"fmt"
	"strings"
)

// RouteRole names a convergence phase role that carries its own route.
type RouteRole string

const (
	// RoleReview is the read-only review phase role.
	RoleReview RouteRole = "review"
	// RoleCorrection is the correction phase role.
	RoleCorrection RouteRole = "correction"
)

// Source identifies where a resolved value came from, for explanation and receipts.
type Source string

const (
	// SourceBuiltinDefault is the immutable built-in default.
	SourceBuiltinDefault Source = "builtin-default"
	// SourceGlobal is the global configuration file.
	SourceGlobal Source = "global"
	// SourceWorkspace is the workspace configuration file.
	SourceWorkspace Source = "workspace"
	// SourceTaskRoute is the frozen base execution route inherited from the task.
	SourceTaskRoute Source = "task-route"
	// SourceSetupBase is the selected model setup's phase base route.
	SourceSetupBase Source = "setup-base"
	// SourceSeverityOverride is the correction by_severity override.
	SourceSeverityOverride Source = "severity-override"
	// SourceResumeOverride is an explicit prospective resume override.
	SourceResumeOverride Source = "resume-override"
)

// Route is one fully resolved runtime route.
type Route struct {
	IDE             string
	Model           string
	ReasoningEffort string
}

// RouteSources records the resolution source of each route field.
type RouteSources struct {
	IDE             Source
	Model           Source
	ReasoningEffort Source
}

// ResolvedRoute is a resolved primary route, its per-field provenance, and one
// optional frozen fallback.
type ResolvedRoute struct {
	Role        RouteRole
	Primary     Route
	Sources     RouteSources
	Fallback    *Route
	HasFallback bool
}

// fieldCandidate is one prospective source for a single route field, in
// precedence order.
type fieldCandidate struct {
	value  *string
	source Source
}

// resolveRouteField selects the first set candidate in precedence order, falling
// back to the inherited base value.
func resolveRouteField(candidates []fieldCandidate, base string, baseSource Source) (string, Source) {
	for _, candidate := range candidates {
		if candidate.value == nil {
			continue
		}
		if trimmed := strings.TrimSpace(*candidate.value); trimmed != "" {
			return trimmed, candidate.source
		}
	}
	return base, baseSource
}

// FallbackRequest describes why and when a fallback is being requested. A
// fallback is legal only before the phase starts and only because the primary is
// unavailable; it is never quality-driven and never chains.
type FallbackRequest struct {
	PrimaryUnavailable bool
	PhaseStarted       bool
	QualityDriven      bool
}

// SelectFallback validates a fallback request and returns the single frozen
// fallback route. It rejects a request placed before the primary is evaluated, a
// mid-session switch, and a quality-driven switch.
func (r ResolvedRoute) SelectFallback(req FallbackRequest) (Route, error) {
	if !r.HasFallback || r.Fallback == nil {
		return Route{}, fmt.Errorf("%w: no fallback configured for %s", ErrRouteInvalid, r.Role)
	}
	if req.QualityDriven {
		return Route{}, fmt.Errorf("%w: fallback is not selected for an undesirable result", ErrRouteInvalid)
	}
	if !req.PrimaryUnavailable {
		return Route{}, fmt.Errorf("%w: fallback is legal only after primary unavailability", ErrRouteInvalid)
	}
	if req.PhaseStarted {
		return Route{}, fmt.Errorf("%w: fallback cannot switch an active session", ErrRouteInvalid)
	}
	return *r.Fallback, nil
}

// AvailableRoute returns the route to run before a phase starts. It prefers the
// primary, uses the one fallback when the primary is unavailable, and reports
// unavailability when neither route can run.
func (r ResolvedRoute) AvailableRoute(
	primaryAvailable, fallbackAvailable bool,
) (route Route, usedFallback, unavailable bool) {
	if primaryAvailable {
		return r.Primary, false, false
	}
	if r.HasFallback && r.Fallback != nil && fallbackAvailable {
		return *r.Fallback, true, false
	}
	return Route{}, false, true
}
