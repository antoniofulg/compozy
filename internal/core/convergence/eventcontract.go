package convergence

import (
	"fmt"
	"sort"
	"strings"
)

// Privacy classifies a canonical event field's disclosure sensitivity. Every
// field in every convergence event contract carries exactly one classification so
// redaction and access control are decided from metadata, not ad hoc filtering.
type Privacy string

const (
	PrivacyPublicMetadata         Privacy = "public-metadata"
	PrivacyInternalIdentifier     Privacy = "internal-identifier"
	PrivacyConfidentialIdentifier Privacy = "confidential-identifier"
	PrivacyInternalConfig         Privacy = "internal-config"
	PrivacyInternalRepoMetadata   Privacy = "internal-repository-metadata"
	PrivacyConfidentialPath       Privacy = "confidential-path"
	PrivacyInternalReview         Privacy = "internal-review"
)

// IsValid reports whether p is a recognized privacy classification.
func (p Privacy) IsValid() bool {
	switch p {
	case PrivacyPublicMetadata, PrivacyInternalIdentifier, PrivacyConfidentialIdentifier,
		PrivacyInternalConfig, PrivacyInternalRepoMetadata, PrivacyConfidentialPath,
		PrivacyInternalReview:
		return true
	default:
		return false
	}
}

// FieldSpec is one canonical event field's contract: its wire name, whether it is
// required, its privacy classification, and where its value originates.
type FieldSpec struct {
	Name     string
	Required bool
	Privacy  Privacy
	Source   string
}

// EventContract is the complete operational contract for one canonical event kind:
// its four identifiers, payload fields, allowed outcomes, and per-outcome behavior.
type EventContract struct {
	Kind            string
	Identifiers     []FieldSpec
	Fields          []FieldSpec
	AllowedOutcomes []string
	Behavior        map[string]string
}

// DeliveryPolicy is the shared delivery contract for every convergence event.
type DeliveryPolicy struct {
	AtLeastOnceLive    bool
	OrderedPerRun      bool
	DedupeBy           string
	ReplayBeforeLive   bool
	DuplicatesAllowed  bool
	RejectOutOfOrder   bool
	RejectPostTerminal bool
}

// OperationalDeliveryPolicy returns the delivery contract shared by all
// convergence events: at-least-once to live observers, exactly ordered per run in
// the durable journal, deduplicated by (run_id, seq), replay before live, with
// out-of-order canonical appends and post-terminal nonterminal events rejected.
func OperationalDeliveryPolicy() DeliveryPolicy {
	return DeliveryPolicy{
		AtLeastOnceLive:    true,
		OrderedPerRun:      true,
		DedupeBy:           "(run_id, seq)",
		ReplayBeforeLive:   true,
		DuplicatesAllowed:  true,
		RejectOutOfOrder:   true,
		RejectPostTerminal: true,
	}
}

// ForbiddenPayloadFields lists field-name fragments no canonical event payload may
// carry. It encodes the techspec rule that events never serialize credentials,
// tokens, environment values, complete prompts or transcripts, unrestricted
// absolute paths, unredacted raw command output, or unrelated repository content.
func ForbiddenPayloadFields() []string {
	return []string{
		"credential",
		"token",
		"secret",
		"password",
		"env_value",
		"environment_value",
		"prompt",
		"transcript",
		"absolute_path",
		"raw_output",
		"raw_stdout",
		"raw_stderr",
	}
}

func standardBehavior(success, rejection, replay, stale string) map[string]string {
	behavior := map[string]string{
		"success":   success,
		"rejection": rejection,
		"replay":    replay,
	}
	if stale != "" {
		behavior["stale"] = stale
	}
	return behavior
}

func identifiers(reqSrc, actorSrc, resSrc, corrSrc string) []FieldSpec {
	return []FieldSpec{
		{Name: "request_id", Required: true, Privacy: PrivacyInternalIdentifier, Source: reqSrc},
		{Name: "actor_id", Required: true, Privacy: PrivacyConfidentialIdentifier, Source: actorSrc},
		{Name: "resource_id", Required: true, Privacy: PrivacyInternalIdentifier, Source: resSrc},
		{Name: "correlation_id", Required: true, Privacy: PrivacyInternalIdentifier, Source: corrSrc},
	}
}

// EventContracts returns the complete, deterministic contract registry for every
// convergence and task-continuation event kind. The order is stable so contract
// audits and documentation stay reproducible.
func EventContracts() []EventContract {
	contracts := []EventContract{
		taskConvergenceRequestedContract(),
		taskConvergenceContinuedContract(),
		preflightCompletedContract(),
		phaseStartedContract(),
		routeSelectedContract(),
		verificationCompletedContract(),
		reviewCompletedContract(),
		findingChangedContract(),
		batchCompletedContract(),
		progressEvaluatedContract(),
		approvalRequestedContract(),
		approvalDecidedContract(),
		segmentParkedContract(),
		segmentCompletedContract(),
		runParkedContract(),
	}
	return contracts
}

// EventContractFor returns the contract for one kind, or false when kind has no
// convergence contract.
func EventContractFor(kind string) (EventContract, bool) {
	for _, contract := range EventContracts() {
		if contract.Kind == kind {
			return contract, true
		}
	}
	return EventContract{}, false
}

// Validate checks the structural completeness of one event contract: four
// identifiers, every field carrying a valid privacy classification and a source,
// at least one allowed outcome, and behavior for every allowed outcome category.
func (c EventContract) Validate() error {
	if strings.TrimSpace(c.Kind) == "" {
		return fmt.Errorf("%w: event contract missing kind", ErrConfigInvalid)
	}
	if len(c.Identifiers) != 4 {
		return fmt.Errorf("%w: %s must declare four identifiers", ErrConfigInvalid, c.Kind)
	}
	for _, field := range append(append([]FieldSpec{}, c.Identifiers...), c.Fields...) {
		if strings.TrimSpace(field.Name) == "" {
			return fmt.Errorf("%w: %s has an unnamed field", ErrConfigInvalid, c.Kind)
		}
		if !field.Privacy.IsValid() {
			return fmt.Errorf("%w: %s field %q has invalid privacy", ErrConfigInvalid, c.Kind, field.Name)
		}
		if strings.TrimSpace(field.Source) == "" {
			return fmt.Errorf("%w: %s field %q missing source", ErrConfigInvalid, c.Kind, field.Name)
		}
	}
	if len(c.AllowedOutcomes) == 0 {
		return fmt.Errorf("%w: %s declares no allowed outcomes", ErrConfigInvalid, c.Kind)
	}
	if len(c.Behavior) == 0 {
		return fmt.Errorf("%w: %s declares no outcome behavior", ErrConfigInvalid, c.Kind)
	}
	return nil
}

// AllowsOutcome reports whether outcome is one of the contract's allowed outcomes.
func (c EventContract) AllowsOutcome(outcome string) bool {
	for _, allowed := range c.AllowedOutcomes {
		if allowed == outcome {
			return true
		}
	}
	return false
}

// ValidatePayload checks one decoded event payload against its contract: every
// required identifier and field is present and non-empty, the reported outcome is
// allowed, and no forbidden field name appears. It returns the first violation.
func ValidatePayload(kind string, payload map[string]any) error {
	contract, ok := EventContractFor(kind)
	if !ok {
		return fmt.Errorf("%w: no contract for event kind %q", ErrConfigInvalid, kind)
	}
	if err := rejectForbiddenFields(kind, payload); err != nil {
		return err
	}
	for _, field := range append(append([]FieldSpec{}, contract.Identifiers...), contract.Fields...) {
		if !field.Required {
			continue
		}
		value, present := payload[field.Name]
		if !present || isEmptyPayloadValue(value) {
			return fmt.Errorf("%w: %s missing required field %q", ErrConfigInvalid, kind, field.Name)
		}
	}
	outcome, ok := payload["outcome"].(string)
	if !ok {
		return fmt.Errorf("%w: %s outcome must be a string", ErrConfigInvalid, kind)
	}
	if !contract.AllowsOutcome(outcome) {
		return fmt.Errorf("%w: %s outcome %q is not allowed", ErrConfigInvalid, kind, outcome)
	}
	return nil
}

func rejectForbiddenFields(kind string, payload map[string]any) error {
	forbidden := ForbiddenPayloadFields()
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		lower := strings.ToLower(key)
		for _, fragment := range forbidden {
			if strings.Contains(lower, fragment) {
				return fmt.Errorf("%w: %s carries forbidden field %q", ErrConfigInvalid, kind, key)
			}
		}
	}
	return nil
}

func isEmptyPayloadValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	default:
		return false
	}
}
