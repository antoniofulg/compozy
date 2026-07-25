package convergence

import (
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/compozy/compozy/pkg/compozy/events/kinds"
)

func TestEventContractsAreStructurallyComplete(t *testing.T) {
	// UT-034 [contract,boundary,error]: every operational event field carries
	// required/privacy/source metadata, four identifiers, allowed outcomes, and
	// per-outcome behavior; plus the delivery policy and forbidden-field policy.
	t.Parallel()

	contracts := EventContracts()

	t.Run("contract: the registry covers exactly the techspec event kinds", func(t *testing.T) {
		t.Parallel()
		want := []string{
			"convergence.approval_decided",
			"convergence.approval_requested",
			"convergence.batch_completed",
			"convergence.finding_changed",
			"convergence.phase_started",
			"convergence.preflight_completed",
			"convergence.progress_evaluated",
			"convergence.review_completed",
			"convergence.route_selected",
			"convergence.segment_completed",
			"convergence.segment_parked",
			"convergence.verification_completed",
			"run.parked",
			"task.convergence_continued",
			"task.convergence_requested",
		}
		got := make([]string, 0, len(contracts))
		for _, c := range contracts {
			got = append(got, c.Kind)
		}
		sort.Strings(got)
		if len(got) != len(want) {
			t.Fatalf("event kind count = %d, want %d (%v)", len(got), len(want), got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("event kind[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("contract: every field carries required/privacy/source metadata", func(t *testing.T) {
		t.Parallel()
		for _, c := range contracts {
			if err := c.Validate(); err != nil {
				t.Fatalf("contract %q failed structural validation: %v", c.Kind, err)
			}
			if len(c.Identifiers) != 4 {
				t.Fatalf("contract %q has %d identifiers, want 4", c.Kind, len(c.Identifiers))
			}
			assertIdentifierNames(t, c)
			for _, field := range append(append([]FieldSpec{}, c.Identifiers...), c.Fields...) {
				if field.Name == "" {
					t.Fatalf("contract %q has an unnamed field", c.Kind)
				}
				if !field.Privacy.IsValid() {
					t.Fatalf("contract %q field %q has invalid privacy %q", c.Kind, field.Name, field.Privacy)
				}
				if field.Source == "" {
					t.Fatalf("contract %q field %q has no source", c.Kind, field.Name)
				}
			}
			if len(c.AllowedOutcomes) == 0 {
				t.Fatalf("contract %q declares no allowed outcomes", c.Kind)
			}
			for _, category := range []string{"success", "rejection", "replay"} {
				if _, ok := c.Behavior[category]; !ok {
					t.Fatalf("contract %q missing %q behavior", c.Kind, category)
				}
			}
		}
	})

	t.Run("contract: delivery policy matches the techspec envelope rules", func(t *testing.T) {
		t.Parallel()
		policy := OperationalDeliveryPolicy()
		if !policy.AtLeastOnceLive || !policy.OrderedPerRun || !policy.ReplayBeforeLive ||
			!policy.DuplicatesAllowed || !policy.RejectOutOfOrder || !policy.RejectPostTerminal {
			t.Fatalf("delivery policy = %+v", policy)
		}
		if policy.DedupeBy != "(run_id, seq)" {
			t.Fatalf("dedupe key = %q, want (run_id, seq)", policy.DedupeBy)
		}
	})
}

func TestOperationalPayloadStructsMatchEventContracts(t *testing.T) {
	// UT-034 drift guard: the actual exported payload structs, not only the
	// machine-readable registry, carry every specified wire field with the same
	// requiredness.
	t.Parallel()

	payloadTypes := map[string]reflect.Type{
		"task.convergence_requested":         reflect.TypeOf(kinds.TaskConvergenceRequestedPayload{}),
		"task.convergence_continued":         reflect.TypeOf(kinds.TaskConvergenceContinuedPayload{}),
		"convergence.preflight_completed":    reflect.TypeOf(kinds.ConvergencePreflightCompletedPayload{}),
		"convergence.phase_started":          reflect.TypeOf(kinds.ConvergencePhaseStartedPayload{}),
		"convergence.route_selected":         reflect.TypeOf(kinds.ConvergenceRouteSelectedPayload{}),
		"convergence.verification_completed": reflect.TypeOf(kinds.ConvergenceVerificationCompletedPayload{}),
		"convergence.review_completed":       reflect.TypeOf(kinds.ConvergenceReviewCompletedPayload{}),
		"convergence.finding_changed":        reflect.TypeOf(kinds.ConvergenceFindingChangedPayload{}),
		"convergence.batch_completed":        reflect.TypeOf(kinds.ConvergenceBatchCompletedPayload{}),
		"convergence.progress_evaluated":     reflect.TypeOf(kinds.ConvergenceProgressEvaluatedPayload{}),
		"convergence.approval_requested":     reflect.TypeOf(kinds.ConvergenceApprovalRequestedPayload{}),
		"convergence.approval_decided":       reflect.TypeOf(kinds.ConvergenceApprovalDecidedPayload{}),
		"convergence.segment_parked":         reflect.TypeOf(kinds.ConvergenceSegmentParkedPayload{}),
		"convergence.segment_completed":      reflect.TypeOf(kinds.ConvergenceSegmentCompletedPayload{}),
		"run.parked":                         reflect.TypeOf(kinds.RunParkedPayload{}),
	}

	for _, contract := range EventContracts() {
		payloadType, ok := payloadTypes[contract.Kind]
		if !ok {
			t.Fatalf("no payload type registered for %q", contract.Kind)
		}
		actual := payloadJSONFields(payloadType)
		expected := append(append([]FieldSpec{}, contract.Identifiers...), contract.Fields...)
		if len(actual) != len(expected) {
			t.Fatalf("%s payload fields = %v, want %d contract fields", contract.Kind, actual, len(expected))
		}
		for _, spec := range expected {
			optional, exists := actual[spec.Name]
			if !exists {
				t.Fatalf("%s payload is missing %q", contract.Kind, spec.Name)
			}
			if optional == spec.Required {
				t.Fatalf(
					"%s payload field %q optional=%t, contract required=%t",
					contract.Kind,
					spec.Name,
					optional,
					spec.Required,
				)
			}
		}
	}
}

func payloadJSONFields(payloadType reflect.Type) map[string]bool {
	fields := make(map[string]bool)
	for i := 0; i < payloadType.NumField(); i++ {
		field := payloadType.Field(i)
		if field.Anonymous {
			for name, optional := range payloadJSONFields(field.Type) {
				fields[name] = optional
			}
			continue
		}
		parts := strings.Split(field.Tag.Get("json"), ",")
		if len(parts) == 0 || parts[0] == "" || parts[0] == "-" {
			continue
		}
		fields[parts[0]] = len(parts) > 1 && parts[1] == "omitempty"
	}
	return fields
}

func TestValidatePayloadEnforcesContract(t *testing.T) {
	// UT-034 error/boundary half: required fields, allowed outcomes, and the
	// forbidden-field policy are enforced per event kind.
	t.Parallel()

	valid := func() map[string]any {
		return map[string]any{
			"request_id":       "req-1",
			"actor_id":         "actor-1",
			"resource_id":      "run-1",
			"correlation_id":   "cvg-1",
			"reason":           "approval_required",
			"result_path":      "artifacts/receipt.json",
			"resume_available": true,
			"summary":          "parked",
			"outcome":          "parked",
		}
	}

	t.Run("happy: a complete run.parked payload validates", func(t *testing.T) {
		t.Parallel()
		if err := ValidatePayload("run.parked", valid()); err != nil {
			t.Fatalf("ValidatePayload(valid) = %v, want nil", err)
		}
	})

	t.Run("error: a missing required field is rejected", func(t *testing.T) {
		t.Parallel()
		payload := valid()
		delete(payload, "reason")
		if err := ValidatePayload("run.parked", payload); !errors.Is(err, ErrConfigInvalid) {
			t.Fatalf("ValidatePayload(missing) = %v, want ErrConfigInvalid", err)
		}
	})

	t.Run("error: a disallowed outcome is rejected", func(t *testing.T) {
		t.Parallel()
		payload := valid()
		payload["outcome"] = "completed" // not in run.parked allowed_outcomes
		if err := ValidatePayload("run.parked", payload); !errors.Is(err, ErrConfigInvalid) {
			t.Fatalf("ValidatePayload(bad outcome) = %v, want ErrConfigInvalid", err)
		}
	})

	t.Run("error: a forbidden field name is rejected even when otherwise valid", func(t *testing.T) {
		t.Parallel()
		payload := valid()
		payload["access_token"] = "should-never-ship"
		if err := ValidatePayload("run.parked", payload); !errors.Is(err, ErrConfigInvalid) {
			t.Fatalf("ValidatePayload(forbidden) = %v, want ErrConfigInvalid", err)
		}
	})

	t.Run("boundary: an unknown kind has no contract", func(t *testing.T) {
		t.Parallel()
		if _, ok := EventContractFor("no.such_kind"); ok {
			t.Fatal("EventContractFor(unknown) reported a contract")
		}
		if err := ValidatePayload("no.such_kind", map[string]any{}); !errors.Is(err, ErrConfigInvalid) {
			t.Fatalf("ValidatePayload(unknown kind) = %v, want ErrConfigInvalid", err)
		}
	})
}

func assertIdentifierNames(t *testing.T, c EventContract) {
	t.Helper()
	want := []string{"request_id", "actor_id", "resource_id", "correlation_id"}
	for i, spec := range c.Identifiers {
		if spec.Name != want[i] {
			t.Fatalf("contract %q identifier[%d] = %q, want %q", c.Kind, i, spec.Name, want[i])
		}
		if !spec.Required {
			t.Fatalf("contract %q identifier %q must be required", c.Kind, spec.Name)
		}
	}
}
