package convergence

import (
	"fmt"
	"strings"
)

// validateModelSetup checks the review and correction routes of one model setup,
// including the single optional fallback per phase and the per-severity overrides.
func validateModelSetup(scope, field string, setup ModelSetupConfig) error {
	if err := validateRoute(scope, field+".review", setup.Review.RouteConfig); err != nil {
		return err
	}
	if err := validateFallback(scope, field+".review", setup.Review.Fallback); err != nil {
		return err
	}
	if err := validateRoute(scope, field+".correction", setup.Correction.RouteConfig); err != nil {
		return err
	}
	if err := validateFallback(scope, field+".correction", setup.Correction.Fallback); err != nil {
		return err
	}
	return validateSeverityRoutes(scope, field+".correction.by_severity", setup.Correction.BySeverity)
}

// validateRoute checks one route's interface, model, and reasoning effort.
func validateRoute(scope, field string, route RouteConfig) error {
	if err := validateIDE(scope, field+".ide", route.IDE); err != nil {
		return err
	}
	if route.Model != nil && strings.TrimSpace(*route.Model) == "" {
		return configError(scope, field+".model", "cannot be empty")
	}
	return validateReasoningEffort(scope, field+".reasoning_effort", route.ReasoningEffort)
}

// validateFallback checks the single optional fallback route. Its type carries no
// nested fallback, which structurally forbids fallback chains.
func validateFallback(scope, field string, fallback *RouteConfig) error {
	if fallback == nil {
		return nil
	}
	return validateRoute(scope, field+".fallback", *fallback)
}

// validateSeverityRoutes checks that every by_severity key is a recognized
// severity and each override route is valid.
func validateSeverityRoutes(scope, field string, routes map[string]RouteConfig) error {
	for _, key := range sortedKeys(routes) {
		if !Severity(key).IsValid() {
			return configError(
				scope,
				fmt.Sprintf("%s.%s", field, key),
				"must be one of critical, high, medium, low",
			)
		}
		if err := validateRoute(scope, field+"."+key, routes[key]); err != nil {
			return err
		}
	}
	return nil
}
