package convergence

import (
	"errors"
	"reflect"
	"testing"
)

func TestPlanCorrectionBatchesGroupsOrdersAndRoutes(t *testing.T) {
	// UT-018: every same-file finding forms one logical batch, batches order by
	// first-finding sequence then path, findings within a batch order by sequence,
	// and the highest current severity selects the batch route.
	t.Parallel()

	findings := []BatchFinding{
		{Fingerprint: "fp-b1", File: "pkg/b.go", Severity: SeverityLow, Sequence: 2},
		{Fingerprint: "fp-a2", File: "pkg/a.go", Severity: SeverityCritical, Sequence: 3},
		{Fingerprint: "fp-c1", File: "pkg/c.go", Severity: SeverityMedium, Sequence: 0},
		{Fingerprint: "fp-a1", File: "pkg/a.go", Severity: SeverityHigh, Sequence: 1},
	}
	batches, err := PlanCorrectionBatches(findings, 0)
	if err != nil {
		t.Fatalf("PlanCorrectionBatches() = %v", err)
	}
	if len(batches) != 3 {
		t.Fatalf("batches len = %d, want 3", len(batches))
	}

	wantFiles := []string{"pkg/c.go", "pkg/a.go", "pkg/b.go"}
	for i, want := range wantFiles {
		if batches[i].File != want {
			t.Fatalf("batch %d file = %q, want %q", i, batches[i].File, want)
		}
		if batches[i].Order != i {
			t.Fatalf("batch %d Order = %d, want %d", i, batches[i].Order, i)
		}
	}

	same := batches[1] // pkg/a.go
	if got := same.FindingFingerprints; !reflect.DeepEqual(
		got, []FindingFingerprint{"fp-a1", "fp-a2"},
	) {
		t.Fatalf("same-file batch order = %v, want [fp-a1 fp-a2]", got)
	}
	if same.RouteSeverity != SeverityCritical {
		t.Fatalf("same-file RouteSeverity = %q, want critical (highest of high+critical)", same.RouteSeverity)
	}
	if batches[0].RouteSeverity != SeverityMedium || batches[2].RouteSeverity != SeverityLow {
		t.Fatalf("route severities = %q,%q; want medium,low", batches[0].RouteSeverity, batches[2].RouteSeverity)
	}
}

func TestPlanCorrectionBatchesSplitsSessionsSequentially(t *testing.T) {
	// UT-018 boundary: a large same-file group splits into bounded ordered sessions
	// while remaining one logical batch for ordering and verification.
	t.Parallel()

	findings := []BatchFinding{
		{Fingerprint: "fp-1", File: "pkg/big.go", Severity: SeverityLow, Sequence: 1},
		{Fingerprint: "fp-2", File: "pkg/big.go", Severity: SeverityMedium, Sequence: 2},
		{Fingerprint: "fp-3", File: "pkg/big.go", Severity: SeverityHigh, Sequence: 3},
		{Fingerprint: "fp-4", File: "pkg/big.go", Severity: SeverityLow, Sequence: 4},
		{Fingerprint: "fp-5", File: "pkg/big.go", Severity: SeverityLow, Sequence: 5},
	}
	batches, err := PlanCorrectionBatches(findings, 2)
	if err != nil {
		t.Fatalf("PlanCorrectionBatches() = %v", err)
	}
	if len(batches) != 1 {
		t.Fatalf("batches len = %d, want 1 logical batch", len(batches))
	}
	batch := batches[0]
	if batch.RouteSeverity != SeverityHigh {
		t.Fatalf("RouteSeverity = %q, want high", batch.RouteSeverity)
	}
	if len(batch.Sessions) != 3 {
		t.Fatalf("sessions len = %d, want 3 bounded sessions", len(batch.Sessions))
	}
	wantSessions := [][]FindingFingerprint{
		{"fp-1", "fp-2"},
		{"fp-3", "fp-4"},
		{"fp-5"},
	}
	for i, want := range wantSessions {
		if batch.Sessions[i].Index != i {
			t.Fatalf("session %d Index = %d, want %d", i, batch.Sessions[i].Index, i)
		}
		if !reflect.DeepEqual(batch.Sessions[i].FindingFingerprints, want) {
			t.Fatalf("session %d = %v, want %v", i, batch.Sessions[i].FindingFingerprints, want)
		}
	}
}

func TestPlanCorrectionBatchesTieBreaksByPathThenFingerprint(t *testing.T) {
	// Equal first-finding sequences order deterministically by path, and equal
	// sequences within a group order by fingerprint.
	t.Parallel()

	findings := []BatchFinding{
		{Fingerprint: "fp-z", File: "pkg/z.go", Severity: SeverityLow, Sequence: 5},
		{Fingerprint: "fp-a", File: "pkg/a.go", Severity: SeverityLow, Sequence: 5},
		{Fingerprint: "fp-a2", File: "pkg/a.go", Severity: SeverityLow, Sequence: 5},
	}
	batches, err := PlanCorrectionBatches(findings, 0)
	if err != nil {
		t.Fatalf("PlanCorrectionBatches() = %v", err)
	}
	if batches[0].File != "pkg/a.go" || batches[1].File != "pkg/z.go" {
		t.Fatalf("batch files = %q,%q; want pkg/a.go,pkg/z.go", batches[0].File, batches[1].File)
	}
	if !reflect.DeepEqual(batches[0].FindingFingerprints, []FindingFingerprint{"fp-a", "fp-a2"}) {
		t.Fatalf("interior order = %v, want [fp-a fp-a2]", batches[0].FindingFingerprints)
	}
}

func TestPlanCorrectionBatchesRejectsBadPath(t *testing.T) {
	t.Parallel()
	_, err := PlanCorrectionBatches([]BatchFinding{
		{Fingerprint: "fp", File: "/etc/passwd", Severity: SeverityLow, Sequence: 1},
	}, 0)
	if !errors.Is(err, ErrFindingIdentityInvalid) {
		t.Fatalf("PlanCorrectionBatches() = %v, want ErrFindingIdentityInvalid", err)
	}
}

func TestCorrectionResultValidate(t *testing.T) {
	t.Parallel()

	base := CorrectionResult{
		BatchID:        "batch-1",
		PhaseID:        "phase-1",
		BeforeSnapshot: "snap-before",
	}
	tests := []struct {
		name    string
		mutate  func(CorrectionResult) CorrectionResult
		wantErr bool
	}{
		{
			name: "changed with moved snapshot and paths",
			mutate: func(r CorrectionResult) CorrectionResult {
				r.Outcome = CorrectionChanged
				r.AfterSnapshot = "snap-after"
				r.AffectedPaths = []string{"pkg/a.go"}
				return r
			},
		},
		{
			name: "no_change with equal snapshot and no paths",
			mutate: func(r CorrectionResult) CorrectionResult {
				r.Outcome = CorrectionNoChange
				r.AfterSnapshot = "snap-before"
				return r
			},
		},
		{
			name: "reject changed without moved snapshot",
			mutate: func(r CorrectionResult) CorrectionResult {
				r.Outcome = CorrectionChanged
				r.AfterSnapshot = "snap-before"
				r.AffectedPaths = []string{"pkg/a.go"}
				return r
			},
			wantErr: true,
		},
		{
			name: "reject changed without affected paths",
			mutate: func(r CorrectionResult) CorrectionResult {
				r.Outcome = CorrectionChanged
				r.AfterSnapshot = "snap-after"
				return r
			},
			wantErr: true,
		},
		{
			name: "reject no_change that moved the snapshot",
			mutate: func(r CorrectionResult) CorrectionResult {
				r.Outcome = CorrectionNoChange
				r.AfterSnapshot = "snap-after"
				return r
			},
			wantErr: true,
		},
		{
			name: "reject missing before snapshot",
			mutate: func(r CorrectionResult) CorrectionResult {
				r.Outcome = CorrectionNoChange
				r.BeforeSnapshot = ""
				return r
			},
			wantErr: true,
		},
		{
			name: "reject unknown outcome enum",
			mutate: func(r CorrectionResult) CorrectionResult {
				r.Outcome = "bogus"
				return r
			},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.mutate(base).Validate()
			if tc.wantErr {
				if !errors.Is(err, ErrCorrectionInvalid) {
					t.Fatalf("Validate() = %v, want ErrCorrectionInvalid", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestClassifyProtectedChanges(t *testing.T) {
	// UT-043: detect proposed test removal, skip, assertion weakening, verification
	// command change, and gate bypass; allow additive and repairing test changes
	// without approval.
	t.Parallel()

	protectedKinds := []ChangeKind{
		ChangeRemoveTest,
		ChangeSkipTest,
		ChangeWeakenAssertion,
		ChangeMutateVerification,
		ChangeBypassGate,
	}
	for _, kind := range protectedKinds {
		if !kind.Protected() {
			t.Fatalf("%q must be protected", kind)
		}
		report := ClassifyProtectedChanges([]ProposedChange{{Kind: kind, Path: "pkg/a_test.go"}})
		if !report.RequiresApproval() {
			t.Fatalf("%q must require approval", kind)
		}
		if len(report.Protected) != 1 || report.Protected[0].Action != kind {
			t.Fatalf("%q protected report = %+v", kind, report.Protected)
		}
		if report.Protected[0].Reason == "" {
			t.Fatalf("%q protected report missing reason", kind)
		}
	}

	allowed := []ChangeKind{ChangeAddTest, ChangeRepairTest}
	for _, kind := range allowed {
		if kind.Protected() {
			t.Fatalf("%q must be allowed", kind)
		}
		report := ClassifyProtectedChanges([]ProposedChange{{Kind: kind, Path: "pkg/a_test.go"}})
		if report.RequiresApproval() {
			t.Fatalf("%q must not require approval", kind)
		}
		if len(report.Allowed) != 1 {
			t.Fatalf("%q allowed report = %+v", kind, report.Allowed)
		}
	}
}

func TestClassifyProtectedChangesMixedAndUnknown(t *testing.T) {
	// A mixed set separates allowed additions from protected weakenings, and an
	// unrecognized kind is treated as protected so no unknown mutation slips
	// through without approval.
	t.Parallel()

	report := ClassifyProtectedChanges([]ProposedChange{
		{Kind: ChangeAddTest, Path: "pkg/new_test.go"},
		{Kind: ChangeSkipTest, Path: "pkg/old_test.go"},
		{Kind: ChangeRepairTest, Path: "pkg/fix_test.go"},
		{Kind: "rewrite_history", Path: "pkg/x.go"},
	})
	if !report.RequiresApproval() {
		t.Fatal("mixed set with a skip and unknown kind must require approval")
	}
	if len(report.Allowed) != 2 {
		t.Fatalf("allowed len = %d, want 2", len(report.Allowed))
	}
	if len(report.Protected) != 2 {
		t.Fatalf("protected len = %d, want 2 (skip + unknown)", len(report.Protected))
	}
}
