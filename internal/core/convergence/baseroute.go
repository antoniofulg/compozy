package convergence

import (
	"strings"

	"github.com/compozy/compozy/internal/core/model"
)

// BaseRouteFromRuntime reads the frozen base execution route from a resolved
// runtime. Convergence inherits this route unchanged; it never reapplies task
// runtime rules such as by_complexity, which continue to route task execution on
// their own. For a Task Group with several task-specific routes, callers pass the
// group run's base runtime so convergence inherits the durable group base
// provenance rather than an arbitrary last task route.
func BaseRouteFromRuntime(cfg *model.RuntimeConfig) Route {
	if cfg == nil {
		return Route{}
	}
	return Route{
		IDE:             strings.TrimSpace(cfg.IDE),
		Model:           strings.TrimSpace(cfg.Model),
		ReasoningEffort: strings.TrimSpace(cfg.ReasoningEffort),
	}
}
