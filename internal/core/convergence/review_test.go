package convergence

import (
	"errors"
	"testing"
)

// validIdentity returns a deterministic, fingerprintable finding identity for the
// given file so tests can vary the observation fields independently of identity.
func validIdentity(file, symbol string) FindingIdentity {
	return FindingIdentity{
		File:     file,
		Category: "correctness",
		Anchor:   Anchor{Kind: AnchorSymbol, Value: symbol},
		Claim:    "the function returns before closing the file",
	}
}

// validReviewedFinding returns a complete actionable reviewed finding.
func validReviewedFinding(file, symbol string, severity Severity) ReviewedFinding {
	return ReviewedFinding{
		Identity: validIdentity(file, symbol),
		Severity: severity,
		Outcome:  ObservationActionable,
		Evidence: "the deferred close never runs on the early return path",
		Line:     42,
		Column:   3,
	}
}

func TestReviewResultValidate(t *testing.T) {
	// UT-019: structured clean/findings validation and malformed-result rejection.
	t.Parallel()

	clean := ReviewResult{
		ReviewID:    "rev-1",
		Snapshot:    "snap-abc",
		SnapshotSeq: 1,
		Outcome:     ReviewOutcomeClean,
		Explanation: "no actionable findings on the current snapshot",
	}
	withFindings := ReviewResult{
		ReviewID:    "rev-2",
		Snapshot:    "snap-abc",
		SnapshotSeq: 1,
		Outcome:     ReviewOutcomeFindings,
		Explanation: "one actionable finding remains",
		Findings:    []ReviewedFinding{validReviewedFinding("pkg/a.go", "pkg.A", SeverityHigh)},
	}

	tests := []struct {
		name    string
		result  ReviewResult
		wantErr error
	}{
		{name: "accept structured clean with explanation and empty actionable list", result: clean},
		{name: "accept complete findings", result: withFindings},
		{
			name: "reject missing snapshot",
			result: func() ReviewResult {
				r := withFindings
				r.Snapshot = "  "
				return r
			}(),
			wantErr: ErrReviewInvalid,
		},
		{
			name: "reject missing review id",
			result: func() ReviewResult {
				r := withFindings
				r.ReviewID = ""
				return r
			}(),
			wantErr: ErrReviewInvalid,
		},
		{
			name: "reject empty unexplained response",
			result: func() ReviewResult {
				r := clean
				r.Explanation = "   "
				return r
			}(),
			wantErr: ErrReviewInvalid,
		},
		{
			name: "reject unknown enum",
			result: func() ReviewResult {
				r := withFindings
				r.Outcome = "approved"
				return r
			}(),
			wantErr: ErrReviewInvalid,
		},
		{
			name: "reject missing finding identity",
			result: func() ReviewResult {
				r := withFindings
				bad := r.Findings[0]
				bad.Identity.Claim = "   "
				r.Findings = []ReviewedFinding{bad}
				return r
			}(),
			wantErr: ErrFindingIdentityInvalid,
		},
		{
			name: "reject missing finding evidence",
			result: func() ReviewResult {
				r := withFindings
				bad := r.Findings[0]
				bad.Evidence = ""
				r.Findings = []ReviewedFinding{bad}
				return r
			}(),
			wantErr: ErrReviewInvalid,
		},
		{
			name: "reject partial findings result with no actionable finding",
			result: func() ReviewResult {
				r := withFindings
				r.Findings = nil
				return r
			}(),
			wantErr: ErrReviewInvalid,
		},
		{
			name: "reject clean result carrying an actionable finding",
			result: func() ReviewResult {
				r := clean
				r.Findings = []ReviewedFinding{validReviewedFinding("pkg/a.go", "pkg.A", SeverityLow)}
				return r
			}(),
			wantErr: ErrReviewInvalid,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.result.Validate()
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Validate() = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestReviewResultObservationsAndDispositions(t *testing.T) {
	// UT-019 mapping: a validated review projects ordered observations bound to its
	// snapshot and evidence-backed dispositions for closed findings.
	t.Parallel()

	result := ReviewResult{
		ReviewID:    "rev-map",
		Snapshot:    "snap-xyz",
		SnapshotSeq: 7,
		Outcome:     ReviewOutcomeFindings,
		Explanation: "one actionable and one invalid finding",
		Findings: []ReviewedFinding{
			validReviewedFinding("pkg/a.go", "pkg.A", SeverityHigh),
			{
				Identity: validIdentity("pkg/b.go", "pkg.B"),
				Severity: SeverityLow,
				Outcome:  ObservationActionable,
				Evidence: "not a real issue after review",
				Disposition: &ReviewedDisposition{
					Type:     DispositionInvalid,
					Evidence: "the guard clause already covers this path",
				},
			},
		},
	}

	observations, err := result.Observations(100)
	if err != nil {
		t.Fatalf("Observations() = %v", err)
	}
	if len(observations) != 2 {
		t.Fatalf("Observations() len = %d, want 2", len(observations))
	}
	for i, obs := range observations {
		if obs.SnapshotSeq != result.SnapshotSeq {
			t.Fatalf("observation %d SnapshotSeq = %d, want %d", i, obs.SnapshotSeq, result.SnapshotSeq)
		}
		if obs.ReviewID != result.ReviewID {
			t.Fatalf("observation %d ReviewID = %q, want %q", i, obs.ReviewID, result.ReviewID)
		}
		if obs.Sequence != uint64(100+i) {
			t.Fatalf("observation %d Sequence = %d, want %d", i, obs.Sequence, 100+i)
		}
		if obs.ObservationID == "" || obs.Fingerprint == "" {
			t.Fatalf("observation %d missing identity: %+v", i, obs)
		}
	}

	dispositions, err := result.Dispositions()
	if err != nil {
		t.Fatalf("Dispositions() = %v", err)
	}
	if len(dispositions) != 1 {
		t.Fatalf("Dispositions() len = %d, want 1", len(dispositions))
	}
	decision := dispositions[0]
	if decision.Type != DispositionInvalid {
		t.Fatalf("disposition Type = %q, want invalid", decision.Type)
	}
	if decision.Actor.Kind != ActorReview || !decision.Actor.CurrentReview {
		t.Fatalf("disposition actor = %+v, want current review actor", decision.Actor)
	}
	if decision.SnapshotSeq != result.SnapshotSeq {
		t.Fatalf("disposition SnapshotSeq = %d, want %d", decision.SnapshotSeq, result.SnapshotSeq)
	}
}

func TestReviewResultProjectsThroughFindingProjection(t *testing.T) {
	// The mapped observations and dispositions apply cleanly to the shared finding
	// projection: an actionable finding stays open and a review-invalidated finding
	// closes with evidence.
	t.Parallel()

	result := ReviewResult{
		ReviewID:    "rev-proj",
		Snapshot:    "snap-1",
		SnapshotSeq: 3,
		Outcome:     ReviewOutcomeFindings,
		Explanation: "mixed outcomes",
		Findings: []ReviewedFinding{
			validReviewedFinding("pkg/a.go", "pkg.A", SeverityHigh),
			{
				Identity: validIdentity("pkg/b.go", "pkg.B"),
				Severity: SeverityMedium,
				Outcome:  ObservationActionable,
				Evidence: "duplicate of the first finding",
				Disposition: &ReviewedDisposition{
					Type:     DispositionDuplicate,
					Evidence: "same defect surfaced twice",
				},
			},
		},
	}
	observations, err := result.Observations(1)
	if err != nil {
		t.Fatalf("Observations() = %v", err)
	}
	dispositions, err := result.Dispositions()
	if err != nil {
		t.Fatalf("Dispositions() = %v", err)
	}

	projection := NewFindingProjection()
	for _, obs := range observations {
		if _, err := projection.ApplyObservation(obs); err != nil {
			t.Fatalf("ApplyObservation() = %v", err)
		}
	}
	for _, decision := range dispositions {
		if _, err := projection.ApplyDisposition(decision); err != nil {
			t.Fatalf("ApplyDisposition() = %v", err)
		}
	}
	if got := projection.OpenActionableCount(); got != 1 {
		t.Fatalf("OpenActionableCount() = %d, want 1", got)
	}
}
