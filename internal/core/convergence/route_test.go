package convergence

import (
	"errors"
	"testing"

	"github.com/compozy/compozy/internal/core/model"
)

func balancedSetup() Config {
	return Config{
		ModelSetup:   ptr("balanced"),
		Verification: verificationCommand(),
		ModelSetups: map[string]ModelSetupConfig{
			"balanced": {
				Review: ReviewSetupConfig{
					RouteConfig: RouteConfig{IDE: ptr("codex"), Model: ptr("reviewer"), ReasoningEffort: ptr("high")},
					Fallback: &RouteConfig{
						IDE:             ptr("claude"),
						Model:           ptr("reviewer-fallback"),
						ReasoningEffort: ptr("high"),
					},
				},
				Correction: CorrectionSetupConfig{
					RouteConfig: RouteConfig{IDE: ptr("codex"), Model: ptr("fixer"), ReasoningEffort: ptr("medium")},
					BySeverity: map[string]RouteConfig{
						"critical": {Model: ptr("fixer-critical"), ReasoningEffort: ptr("high")},
					},
				},
			},
		},
	}
}

func freezeBalanced(t *testing.T, base Route) FrozenConfiguration {
	t.Helper()
	frozen, err := Freeze(FreezeInput{Workspace: balancedSetup(), BaseRoute: base})
	if err != nil {
		t.Fatalf("freeze: %v", err)
	}
	return frozen
}

func TestRouteResolutionPrecedence(t *testing.T) {
	t.Parallel()
	base := Route{IDE: "codex", Model: "base-model", ReasoningEffort: "low"}
	t.Run("Should prefer the severity override over the setup base", func(t *testing.T) {
		t.Parallel()
		route := freezeBalanced(t, base).CorrectionRoute(SeverityCritical, nil)
		if route.Primary.Model != "fixer-critical" || route.Sources.Model != SourceSeverityOverride {
			t.Fatalf("expected severity override model, got %+v", route)
		}
		if route.Primary.IDE != "codex" || route.Sources.IDE != SourceSetupBase {
			t.Fatalf("expected setup-base ide, got %+v", route)
		}
	})
	t.Run("Should prefer a resume override over everything else", func(t *testing.T) {
		t.Parallel()
		resume := &RouteConfig{Model: ptr("resume-model")}
		route := freezeBalanced(t, base).CorrectionRoute(SeverityCritical, resume)
		if route.Primary.Model != "resume-model" || route.Sources.Model != SourceResumeOverride {
			t.Fatalf("expected resume override model, got %+v", route)
		}
	})
	t.Run("Should use the setup base when no severity override matches", func(t *testing.T) {
		t.Parallel()
		route := freezeBalanced(t, base).CorrectionRoute(SeverityLow, nil)
		if route.Primary.Model != "fixer" || route.Sources.Model != SourceSetupBase {
			t.Fatalf("expected setup-base model, got %+v", route)
		}
	})
	t.Run("Should inherit the base route when a field is unset everywhere", func(t *testing.T) {
		t.Parallel()
		frozen, err := Freeze(FreezeInput{Workspace: Config{Verification: verificationCommand()}, BaseRoute: base})
		if err != nil {
			t.Fatalf("freeze: %v", err)
		}
		route := frozen.ReviewRoute(nil)
		if route.Primary != base || route.Sources.Model != SourceTaskRoute {
			t.Fatalf("expected inherited base route, got %+v", route)
		}
	})
}

func TestHighestSeveritySelection(t *testing.T) {
	t.Parallel()
	t.Run("Should select the highest severity in a mixed batch", func(t *testing.T) {
		t.Parallel()
		highest, ok := HighestSeverity([]Severity{SeverityMedium, SeverityCritical, SeverityLow, SeverityHigh})
		if !ok || highest != SeverityCritical {
			t.Fatalf("expected critical, got %q (%v)", highest, ok)
		}
	})
	t.Run("Should report no severity for an empty batch", func(t *testing.T) {
		t.Parallel()
		if _, ok := HighestSeverity(nil); ok {
			t.Fatal("expected no severity for empty batch")
		}
	})
}

func TestFallbackSelectionPolicy(t *testing.T) {
	t.Parallel()
	base := Route{IDE: "codex", Model: "base", ReasoningEffort: "low"}
	route := freezeBalanced(t, base).ReviewRoute(nil)
	t.Run("Should freeze exactly one fallback", func(t *testing.T) {
		t.Parallel()
		if !route.HasFallback || route.Fallback == nil {
			t.Fatal("expected a single frozen fallback")
		}
	})
	t.Run("Should allow the fallback only before phase start after primary unavailability", func(t *testing.T) {
		t.Parallel()
		got, err := route.SelectFallback(FallbackRequest{PrimaryUnavailable: true})
		if err != nil || got.Model != "reviewer-fallback" {
			t.Fatalf("expected the fallback route, got %+v err=%v", got, err)
		}
	})
	rejections := map[string]FallbackRequest{
		"before primary evaluation": {PrimaryUnavailable: false},
		"mid-session switch":        {PrimaryUnavailable: true, PhaseStarted: true},
		"quality driven":            {PrimaryUnavailable: true, QualityDriven: true},
	}
	for name, req := range rejections {
		t.Run("Should reject a "+name+" fallback", func(t *testing.T) {
			t.Parallel()
			if _, err := route.SelectFallback(req); !errors.Is(err, ErrRouteInvalid) {
				t.Fatalf("expected ErrRouteInvalid for %s, got %v", name, err)
			}
		})
	}
	t.Run("Should report runtime unavailability when neither route can run", func(t *testing.T) {
		t.Parallel()
		if _, _, unavailable := route.AvailableRoute(false, false); !unavailable {
			t.Fatal("expected runtime unavailability")
		}
	})
	t.Run("Should reject a fallback when none is configured", func(t *testing.T) {
		t.Parallel()
		plain := ResolvedRoute{Role: RoleReview}
		if _, err := plain.SelectFallback(FallbackRequest{PrimaryUnavailable: true}); !errors.Is(err, ErrRouteInvalid) {
			t.Fatalf("expected ErrRouteInvalid, got %v", err)
		}
	})
}

func TestTaskRoutingUnchangedAndGroupInheritance(t *testing.T) {
	t.Parallel()
	t.Run("Should preserve existing by_complexity and type task routing", func(t *testing.T) {
		t.Parallel()
		cfg := &model.RuntimeConfig{
			IDE: "codex", Model: "base-model", ReasoningEffort: "medium",
			TaskRuntimeRules: []model.TaskRuntimeRule{
				{Complexity: ptr("high"), Model: ptr("high-model")},
				{Type: ptr("backend"), IDE: ptr("claude")},
			},
		}
		resolved := cfg.RuntimeForTask(model.TaskRuntimeTarget{Type: "backend", Complexity: "high"})
		if resolved.IDE != "claude" || resolved.Model != "high-model" {
			t.Fatalf("task routing changed: ide=%q model=%q", resolved.IDE, resolved.Model)
		}
		if cfg.IDE != "codex" || cfg.Model != "base-model" {
			t.Fatalf("base runtime was mutated by task resolution: %+v", cfg)
		}
	})
	t.Run("Should inherit the durable group base route, not a task-resolved route", func(t *testing.T) {
		t.Parallel()
		groupBase := &model.RuntimeConfig{IDE: "codex", Model: "group-model", ReasoningEffort: "medium",
			TaskRuntimeRules: []model.TaskRuntimeRule{{Complexity: ptr("high"), Model: ptr("task-model")}}}
		base := BaseRouteFromRuntime(groupBase)
		if base != (Route{IDE: "codex", Model: "group-model", ReasoningEffort: "medium"}) {
			t.Fatalf("expected group base route, got %+v", base)
		}
		frozen, err := Freeze(FreezeInput{Workspace: Config{Verification: verificationCommand()}, BaseRoute: base})
		if err != nil {
			t.Fatalf("freeze: %v", err)
		}
		if frozen.CorrectionRoute(SeverityHigh, nil).Primary.Model != "group-model" {
			t.Fatal("expected convergence to inherit the group base model")
		}
	})
}
