package convergence

import "fmt"

// resolveSetup resolves the selected model setup. An empty or default selection
// means review and correction inherit the base execution route. A named setup
// must exist in the merged configuration.
func resolveSetup(merged Config) (name string, setup ModelSetupConfig, inherits bool, err error) {
	selection := selectionName(merged.ModelSetup)
	if selection == "" || selection == DefaultProfileName {
		return DefaultProfileName, ModelSetupConfig{}, true, nil
	}
	resolved, ok := merged.ModelSetups[selection]
	if !ok {
		return "", ModelSetupConfig{}, false, fmt.Errorf("%w: %q", ErrModelSetupNotFound, selection)
	}
	return selection, resolved, false, nil
}

// checkAuthority rejects any configured route whose interface lies outside the
// run's authorized interfaces. An empty allowlist authorizes every validated
// interface.
func (f FrozenConfiguration) checkAuthority(allowed []string) error {
	if len(allowed) == 0 || f.inheritsRoute {
		return nil
	}
	permitted := make(map[string]struct{}, len(allowed))
	for _, ide := range allowed {
		permitted[ide] = struct{}{}
	}
	for _, ide := range f.configuredIDEs() {
		if _, ok := permitted[ide]; !ok {
			return fmt.Errorf("%w: interface %q requires broader authority than the run holds", ErrRouteInvalid, ide)
		}
	}
	return nil
}

// configuredIDEs lists every interface named by the frozen setup's routes.
func (f FrozenConfiguration) configuredIDEs() []string {
	routes := []RouteConfig{f.setup.Review.RouteConfig, f.setup.Correction.RouteConfig}
	if f.setup.Review.Fallback != nil {
		routes = append(routes, *f.setup.Review.Fallback)
	}
	if f.setup.Correction.Fallback != nil {
		routes = append(routes, *f.setup.Correction.Fallback)
	}
	for _, key := range sortedKeys(f.setup.Correction.BySeverity) {
		routes = append(routes, f.setup.Correction.BySeverity[key])
	}
	var ides []string
	for _, route := range routes {
		if route.IDE != nil {
			ides = append(ides, *route.IDE)
		}
	}
	return ides
}
