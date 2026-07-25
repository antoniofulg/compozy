package convergence

// This file transcribes the techspec Operational Event Contract into machine-
// readable contracts. Field names, requiredness, privacy, sources, allowed
// outcomes, and behavior mirror `_techspec.md` "Event Schemas" exactly so a
// single audit (UT-034) can prove the shipped events match the specification.

func taskConvergenceRequestedContract() EventContract {
	return EventContract{
		Kind: "task.convergence_requested",
		Identifiers: identifiers(
			"normalized client request",
			"authorized local principal",
			"execution scope",
			"source task run ID",
		),
		Fields: []FieldSpec{
			{
				Name:     "activation_fingerprint",
				Required: true,
				Privacy:  PrivacyInternalIdentifier,
				Source:   "daemon canonicalizer",
			},
			{Name: "profile", Required: true, Privacy: PrivacyInternalConfig, Source: "frozen preflight"},
			{
				Name:     "model_setup",
				Required: true,
				Privacy:  PrivacyInternalConfig,
				Source:   "frozen preflight or default inheritance",
			},
			{Name: "outcome", Required: true, Privacy: PrivacyPublicMetadata, Source: "request evaluator"},
		},
		AllowedOutcomes: []string{"accepted", "replayed", "rejected", "stale"},
		Behavior: standardBehavior(
			"persist intent before task execution",
			"no task or convergence side effect",
			"return the existing source run",
			"reject mismatched target or snapshot",
		),
	}
}

func taskConvergenceContinuedContract() EventContract {
	return EventContract{
		Kind: "task.convergence_continued",
		Identifiers: identifiers(
			"stored activation",
			"stored authorized principal",
			"execution scope",
			"source task run ID",
		),
		Fields: []FieldSpec{
			{
				Name:     "convergence_run_id",
				Required: true,
				Privacy:  PrivacyInternalIdentifier,
				Source:   "durable child creation",
			},
			{
				Name:     "source_snapshot",
				Required: true,
				Privacy:  PrivacyInternalRepoMetadata,
				Source:   "Git snapshot service",
			},
			{Name: "outcome", Required: true, Privacy: PrivacyPublicMetadata, Source: "continuation transaction"},
		},
		AllowedOutcomes: []string{"created", "replayed", "not_started"},
		Behavior: standardBehavior(
			"observers may follow the convergence run",
			"not_started preserves failed or canceled task outcome",
			"return the previously created child",
			"",
		),
	}
}

func preflightCompletedContract() EventContract {
	return EventContract{
		Kind: "convergence.preflight_completed",
		Identifiers: identifiers(
			"start or resume request",
			"authorized local principal",
			"convergence ID",
			"segment run ID",
		),
		Fields: []FieldSpec{
			{
				Name:     "target_snapshot",
				Required: true,
				Privacy:  PrivacyInternalRepoMetadata,
				Source:   "Git snapshot service",
			},
			{Name: "config_fingerprint", Required: true, Privacy: PrivacyInternalIdentifier, Source: "frozen config"},
			{Name: "route_summary", Required: true, Privacy: PrivacyInternalConfig, Source: "route resolver"},
			{Name: "warnings", Required: true, Privacy: PrivacyInternalRepoMetadata, Source: "preflight checks"},
			{Name: "outcome", Required: true, Privacy: PrivacyPublicMetadata, Source: "preflight validator"},
		},
		AllowedOutcomes: []string{"accepted", "rejected", "replayed", "stale"},
		Behavior: standardBehavior(
			"permits initial verification",
			"starts no model or verification phase",
			"returns the same frozen resolution",
			"rejects changed target/config fingerprint",
		),
	}
}

func phaseStartedContract() EventContract {
	return EventContract{
		Kind:        "convergence.phase_started",
		Identifiers: identifiers("owning segment", "phase authority", "phase ID", "convergence ID"),
		Fields: []FieldSpec{
			{Name: "phase", Required: true, Privacy: PrivacyPublicMetadata, Source: "state machine"},
			{Name: "round", Required: true, Privacy: PrivacyPublicMetadata, Source: "state machine"},
			{Name: "batch_id", Required: false, Privacy: PrivacyInternalIdentifier, Source: "batch planner"},
			{Name: "attempt", Required: true, Privacy: PrivacyPublicMetadata, Source: "durable counters"},
			{Name: "snapshot", Required: true, Privacy: PrivacyInternalRepoMetadata, Source: "Git snapshot service"},
			{Name: "outcome", Required: true, Privacy: PrivacyPublicMetadata, Source: "phase claim transaction"},
		},
		AllowedOutcomes: []string{"started", "replayed", "rejected", "stale"},
		Behavior: standardBehavior(
			"side effect may begin after append",
			"no session/process starts",
			"attaches to the claimed phase",
			"parks or rejects on snapshot mismatch",
		),
	}
}

func routeSelectedContract() EventContract {
	return EventContract{
		Kind:        "convergence.route_selected",
		Identifiers: identifiers("owning segment", "runtime evaluator", "phase ID", "convergence ID"),
		Fields: []FieldSpec{
			{Name: "role", Required: true, Privacy: PrivacyPublicMetadata, Source: "phase kind"},
			{Name: "primary", Required: true, Privacy: PrivacyInternalConfig, Source: "frozen route"},
			{Name: "selected", Required: true, Privacy: PrivacyInternalConfig, Source: "availability evaluation"},
			{Name: "configuration_source", Required: true, Privacy: PrivacyInternalConfig, Source: "route resolver"},
			{Name: "fallback_reason", Required: false, Privacy: PrivacyInternalConfig, Source: "availability failure"},
			{Name: "outcome", Required: true, Privacy: PrivacyPublicMetadata, Source: "route evaluator"},
		},
		AllowedOutcomes: []string{"primary", "fallback", "unavailable", "replayed"},
		Behavior: standardBehavior(
			"selected route starts only in a later phase-start transition",
			"unavailable parks when no legal route remains",
			"returns frozen decision",
			"stale route/config decisions are rejected by preflight fingerprint",
		),
	}
}

func verificationCompletedContract() EventContract {
	return EventContract{
		Kind:        "convergence.verification_completed",
		Identifiers: identifiers("owning segment", "daemon verification authority", "verification ID", "phase ID"),
		Fields: []FieldSpec{
			{
				Name:     "command_fingerprint",
				Required: true,
				Privacy:  PrivacyInternalConfig,
				Source:   "verification config",
			},
			{Name: "snapshot", Required: true, Privacy: PrivacyInternalRepoMetadata, Source: "Git snapshot service"},
			{Name: "exit_code", Required: false, Privacy: PrivacyPublicMetadata, Source: "owned process result"},
			{Name: "evidence_path", Required: true, Privacy: PrivacyConfidentialPath, Source: "run artifact writer"},
			{Name: "outcome", Required: true, Privacy: PrivacyPublicMetadata, Source: "result validator"},
		},
		AllowedOutcomes: []string{"passed", "failed", "canceled", "incomplete", "unknown"},
		Behavior: standardBehavior(
			"passed authorizes the next snapshot-bound transition",
			"failed creates correction work; incomplete/unknown never pass",
			"matching completed evidence is reused",
			"result is rejected when its snapshot changed",
		),
	}
}

func reviewCompletedContract() EventContract {
	return EventContract{
		Kind:        "convergence.review_completed",
		Identifiers: identifiers("owning segment", "reviewer session identity", "review ID", "phase ID"),
		Fields: []FieldSpec{
			{Name: "snapshot", Required: true, Privacy: PrivacyInternalRepoMetadata, Source: "Git snapshot service"},
			{
				Name:     "finding_count",
				Required: true,
				Privacy:  PrivacyPublicMetadata,
				Source:   "validated structured result",
			},
			{Name: "artifact_path", Required: true, Privacy: PrivacyConfidentialPath, Source: "daemon artifact writer"},
			{Name: "read_only_enforced", Required: true, Privacy: PrivacyPublicMetadata, Source: "adapter capability"},
			{Name: "outcome", Required: true, Privacy: PrivacyPublicMetadata, Source: "review validator"},
		},
		AllowedOutcomes: []string{"clean", "findings", "invalid", "canceled", "incomplete", "replayed"},
		Behavior: standardBehavior(
			"accepted findings update current state",
			"invalid/incomplete cannot authorize clean",
			"duplicate review identity is deduplicated",
			"snapshot mismatch rejects the result",
		),
	}
}

func findingChangedContract() EventContract {
	return EventContract{
		Kind: "convergence.finding_changed",
		Identifiers: identifiers(
			"causing review or decision",
			"authorized phase/user",
			"semantic finding fingerprint",
			"review or approval ID",
		),
		Fields: []FieldSpec{
			{Name: "state", Required: true, Privacy: PrivacyInternalReview, Source: "finding projector"},
			{Name: "severity", Required: true, Privacy: PrivacyInternalReview, Source: "current observation"},
			{Name: "snapshot", Required: true, Privacy: PrivacyInternalRepoMetadata, Source: "current observation"},
			{Name: "evidence_ref", Required: true, Privacy: PrivacyConfidentialPath, Source: "daemon artifact writer"},
			{Name: "disposition_reason", Required: false, Privacy: PrivacyInternalReview, Source: "validated decision"},
			{Name: "outcome", Required: true, Privacy: PrivacyPublicMetadata, Source: "state transition"},
		},
		AllowedOutcomes: []string{
			"created",
			"updated",
			"resolved",
			"invalid",
			"duplicate",
			"waived",
			"replayed",
			"stale",
		},
		Behavior: standardBehavior(
			"ordered projection changes current finding state",
			"unauthorized or incomplete disposition leaves state unchanged",
			"decision/event identity is idempotent",
			"old snapshot observations cannot change current state",
		),
	}
}

func batchCompletedContract() EventContract {
	return EventContract{
		Kind:        "convergence.batch_completed",
		Identifiers: identifiers("owning segment", "correction session identity", "batch ID", "phase ID"),
		Fields: []FieldSpec{
			{Name: "finding_fingerprints", Required: true, Privacy: PrivacyInternalReview, Source: "batch plan"},
			{
				Name:     "before_snapshot",
				Required: true,
				Privacy:  PrivacyInternalRepoMetadata,
				Source:   "Git snapshot service",
			},
			{
				Name:     "after_snapshot",
				Required: true,
				Privacy:  PrivacyInternalRepoMetadata,
				Source:   "Git snapshot service",
			},
			{
				Name:     "affected_paths_ref",
				Required: true,
				Privacy:  PrivacyConfidentialPath,
				Source:   "Git evidence writer",
			},
			{Name: "outcome", Required: true, Privacy: PrivacyPublicMetadata, Source: "correction validator"},
		},
		AllowedOutcomes: []string{"changed", "no_change", "canceled", "incomplete", "unknown", "replayed"},
		Behavior: standardBehavior(
			"durable checkpoint permits the next sequential batch",
			"incomplete/unknown enters recovery before new work",
			"duplicate batch result does not reapply edits",
			"unexpected snapshots park as workspace_changed",
		),
	}
}

func progressEvaluatedContract() EventContract {
	return EventContract{
		Kind:        "convergence.progress_evaluated",
		Identifiers: identifiers("owning segment", "daemon evaluator", "round ID", "convergence ID"),
		Fields: []FieldSpec{
			{Name: "resolved", Required: true, Privacy: PrivacyPublicMetadata, Source: "finding projection delta"},
			{Name: "severity_decreased", Required: true, Privacy: PrivacyPublicMetadata, Source: "observation delta"},
			{
				Name:     "verification_improved",
				Required: true,
				Privacy:  PrivacyPublicMetadata,
				Source:   "verification delta",
			},
			{Name: "no_progress_count", Required: true, Privacy: PrivacyPublicMetadata, Source: "limit evaluator"},
			{Name: "oscillation_count", Required: true, Privacy: PrivacyPublicMetadata, Source: "finding history"},
			{Name: "outcome", Required: true, Privacy: PrivacyPublicMetadata, Source: "state machine"},
		},
		AllowedOutcomes: []string{"progress", "no_progress", "oscillation", "replayed"},
		Behavior: standardBehavior(
			"updates counters once per round",
			"invalid or out-of-order round cannot update counters",
			"evaluation identity is idempotent",
			"later snapshots require a new round evaluation",
		),
	}
}

func approvalRequestedContract() EventContract {
	return EventContract{
		Kind: "convergence.approval_requested",
		Identifiers: identifiers(
			"causing phase",
			"proposing session identity",
			"proposal ID",
			"finding or verification ID",
		),
		Fields: []FieldSpec{
			{
				Name:     "proposal_fingerprint",
				Required: true,
				Privacy:  PrivacyInternalIdentifier,
				Source:   "daemon canonicalizer",
			},
			{Name: "snapshot", Required: true, Privacy: PrivacyInternalRepoMetadata, Source: "Git snapshot service"},
			{Name: "action", Required: true, Privacy: PrivacyInternalReview, Source: "validated proposal"},
			{Name: "evidence_ref", Required: true, Privacy: PrivacyConfidentialPath, Source: "daemon artifact writer"},
			{Name: "outcome", Required: true, Privacy: PrivacyPublicMetadata, Source: "approval gate"},
		},
		AllowedOutcomes: []string{"pending", "replayed", "rejected", "stale"},
		Behavior: standardBehavior(
			"parks the segment before applying protected action",
			"malformed or unauthorized proposal is not applied",
			"same fingerprint returns existing proposal",
			"changed snapshot invalidates proposal",
		),
	}
}

func approvalDecidedContract() EventContract {
	return EventContract{
		Kind: "convergence.approval_decided",
		Identifiers: identifiers(
			"client decision request",
			"authorized local principal",
			"proposal ID",
			"parked segment run ID",
		),
		Fields: []FieldSpec{
			{
				Name:     "proposal_fingerprint",
				Required: true,
				Privacy:  PrivacyInternalIdentifier,
				Source:   "stored proposal",
			},
			{Name: "snapshot", Required: true, Privacy: PrivacyInternalRepoMetadata, Source: "decision precondition"},
			{Name: "decision", Required: true, Privacy: PrivacyInternalReview, Source: "authorized request"},
			{Name: "reason", Required: true, Privacy: PrivacyInternalReview, Source: "authorized request"},
			{Name: "outcome", Required: true, Privacy: PrivacyPublicMetadata, Source: "approval service"},
		},
		AllowedOutcomes: []string{"approved", "rejected", "replayed", "stale", "denied"},
		Behavior: standardBehavior(
			"appends decision but does not resume",
			"denied/stale leaves proposal unresolved or invalidated",
			"same request fingerprint returns stored decision",
			"snapshot or proposal mismatch rejects mutation",
		),
	}
}

func segmentParkedContract() EventContract {
	return EventContract{
		Kind:        "convergence.segment_parked",
		Identifiers: identifiers("owning segment", "daemon state machine", "segment run ID", "convergence ID"),
		Fields: []FieldSpec{
			{Name: "reason", Required: true, Privacy: PrivacyPublicMetadata, Source: "terminal evaluator"},
			{Name: "snapshot", Required: true, Privacy: PrivacyInternalRepoMetadata, Source: "Git snapshot service"},
			{Name: "unresolved_count", Required: true, Privacy: PrivacyPublicMetadata, Source: "finding projection"},
			{Name: "receipt_path", Required: true, Privacy: PrivacyConfidentialPath, Source: "receipt writer"},
			{Name: "resume_available", Required: true, Privacy: PrivacyPublicMetadata, Source: "terminal policy"},
			{Name: "outcome", Required: true, Privacy: PrivacyPublicMetadata, Source: "state machine"},
		},
		AllowedOutcomes: []string{"parked", "replayed"},
		Behavior: standardBehavior(
			"emits public run.parked after durable convergence state",
			"terminal transaction failure becomes failed/incomplete, never parked",
			"terminal segment is immutable",
			"nonterminal events after this sequence are rejected",
		),
	}
}

func segmentCompletedContract() EventContract {
	return EventContract{
		Kind:        "convergence.segment_completed",
		Identifiers: identifiers("owning segment", "daemon state machine", "segment run ID", "convergence ID"),
		Fields: []FieldSpec{
			{Name: "snapshot", Required: true, Privacy: PrivacyInternalRepoMetadata, Source: "Git snapshot service"},
			{
				Name:     "verification_id",
				Required: true,
				Privacy:  PrivacyInternalIdentifier,
				Source:   "current passing result",
			},
			{Name: "review_id", Required: true, Privacy: PrivacyInternalIdentifier, Source: "current clean review"},
			{Name: "receipt_path", Required: true, Privacy: PrivacyConfidentialPath, Source: "receipt writer"},
			{Name: "outcome", Required: true, Privacy: PrivacyPublicMetadata, Source: "clean evaluator"},
		},
		AllowedOutcomes: []string{"clean", "replayed"},
		Behavior: standardBehavior(
			"emits run.completed and immutable clean receipt",
			"missing or stale evidence forbids completion",
			"no model or repository side effect repeats",
			"later repository changes do not alter historical receipt",
		),
	}
}

func runParkedContract() EventContract {
	return EventContract{
		Kind: "run.parked",
		Identifiers: identifiers(
			"owning run request",
			"terminal transition authority",
			"terminal run ID",
			"convergence ID or parent run ID",
		),
		Fields: []FieldSpec{
			{Name: "reason", Required: true, Privacy: PrivacyPublicMetadata, Source: "terminal evaluator"},
			{Name: "result_path", Required: true, Privacy: PrivacyConfidentialPath, Source: "run artifact layout"},
			{
				Name:     "resume_available",
				Required: true,
				Privacy:  PrivacyPublicMetadata,
				Source:   "run-mode terminal policy",
			},
			{
				Name:     "summary",
				Required: true,
				Privacy:  PrivacyInternalRepoMetadata,
				Source:   "bounded terminal projection",
			},
			{Name: "outcome", Required: true, Privacy: PrivacyPublicMetadata, Source: "run state machine"},
		},
		AllowedOutcomes: []string{"parked", "replayed"},
		Behavior: standardBehavior(
			"terminates the run segment after its mode-specific durable event",
			"append failure prevents terminal publication",
			"returns the immutable terminal event",
			"later nonterminal events are rejected",
		),
	}
}
