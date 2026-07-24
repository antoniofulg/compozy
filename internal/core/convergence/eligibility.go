package convergence

import (
	"fmt"

	"github.com/compozy/compozy/internal/core/model"
)

// RecognizedScope reports whether an execution scope is complete enough to be
// recognized Compozy work. It mirrors the completeness the execution-scope
// builder enforces without widening eligibility.
func RecognizedScope(scope model.ExecutionScope) bool {
	return scope.SpecDir != "" &&
		scope.OperationalDir != "" &&
		scope.WorkflowRef != "" &&
		scope.TasksDir != "" &&
		scope.ReviewsDir != "" &&
		scope.MemoryDir != ""
}

// EligibilityInput describes a standalone convergence target.
type EligibilityInput struct {
	// Scope is the recognized execution scope.
	Scope model.ExecutionScope
	// CompletionEvidence is durable proof the task or Task Group completed.
	CompletionEvidence bool
}

// EvaluateEligibility accepts a standalone convergence target only when it is
// recognized Compozy work with durable completion evidence. It rejects
// unrecognized scopes and missing completion evidence rather than guessing.
func EvaluateEligibility(in EligibilityInput) error {
	if !RecognizedScope(in.Scope) {
		return fmt.Errorf("%w: execution scope is not recognized Compozy work", ErrTargetIneligible)
	}
	if !in.CompletionEvidence {
		return fmt.Errorf("%w: durable completion evidence is required", ErrTargetIneligible)
	}
	return nil
}

// ManualFinding is one historical manual review finding considered for seeding a
// standalone convergence run.
type ManualFinding struct {
	Fingerprint FindingFingerprint
	Snapshot    string
	Resolved    bool
}

// SeedManualFindings splits historical manual findings into those that seed the
// current run and those that remain historical. Only unresolved findings whose
// recorded snapshot matches the current frozen snapshot exactly may seed the
// loop; every other finding stays historical evidence.
func SeedManualFindings(current string, findings []ManualFinding) (seed, historical []ManualFinding) {
	for _, finding := range findings {
		if !finding.Resolved && finding.Snapshot == current && current != "" {
			seed = append(seed, finding)
			continue
		}
		historical = append(historical, finding)
	}
	return seed, historical
}
