package convergence

import (
	"errors"
	"testing"

	"github.com/compozy/compozy/internal/core/model"
)

func recognizedScope() model.ExecutionScope {
	return model.ExecutionScope{
		SpecDir:        "/repo/.compozy/tasks/init",
		OperationalDir: "/repo/.compozy/tasks/init/TG-004",
		WorkflowRef:    "TG-004",
		TasksDir:       "/repo/.compozy/tasks/init/TG-004/tasks",
		ReviewsDir:     "/repo/.compozy/tasks/init/TG-004/reviews",
		MemoryDir:      "/repo/.compozy/tasks/init/TG-004/memory",
	}
}

func TestTargetEligibility(t *testing.T) {
	t.Parallel()
	t.Run("Should accept recognized completed work", func(t *testing.T) {
		t.Parallel()
		if err := EvaluateEligibility(
			EligibilityInput{Scope: recognizedScope(), CompletionEvidence: true},
		); err != nil {
			t.Fatalf("expected eligible target, got %v", err)
		}
	})
	t.Run("Should reject an unrecognized scope", func(t *testing.T) {
		t.Parallel()
		err := EvaluateEligibility(EligibilityInput{Scope: model.ExecutionScope{}, CompletionEvidence: true})
		if !errors.Is(err, ErrTargetIneligible) {
			t.Fatalf("expected ErrTargetIneligible, got %v", err)
		}
	})
	t.Run("Should reject missing completion evidence", func(t *testing.T) {
		t.Parallel()
		err := EvaluateEligibility(EligibilityInput{Scope: recognizedScope(), CompletionEvidence: false})
		if !errors.Is(err, ErrTargetIneligible) {
			t.Fatalf("expected ErrTargetIneligible, got %v", err)
		}
	})
}

func TestSeedManualFindings(t *testing.T) {
	t.Parallel()
	t.Run("Should seed only same-snapshot unresolved findings and keep the rest historical", func(t *testing.T) {
		t.Parallel()
		findings := []ManualFinding{
			{Fingerprint: "match-open", Snapshot: "snap-1", Resolved: false},
			{Fingerprint: "match-resolved", Snapshot: "snap-1", Resolved: true},
			{Fingerprint: "stale-open", Snapshot: "snap-0", Resolved: false},
		}
		seed, historical := SeedManualFindings("snap-1", findings)
		if len(seed) != 1 || seed[0].Fingerprint != "match-open" {
			t.Fatalf("expected one seeded finding, got %+v", seed)
		}
		if len(historical) != 2 {
			t.Fatalf("expected two historical findings, got %+v", historical)
		}
	})
	t.Run("Should seed nothing for an empty current snapshot", func(t *testing.T) {
		t.Parallel()
		seed, _ := SeedManualFindings("", []ManualFinding{{Fingerprint: "x", Snapshot: ""}})
		if len(seed) != 0 {
			t.Fatalf("expected no seed for empty snapshot, got %+v", seed)
		}
	})
}
