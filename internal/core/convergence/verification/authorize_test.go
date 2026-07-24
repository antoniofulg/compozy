package verification

import "testing"

func exitCode(code int) *int { return &code }

func completedResult(exit *int, start, end string) Result {
	return Result{
		VerificationID: "ver-1",
		PhaseID:        "phase-1",
		Completion:     CompletionCompleted,
		ExitCode:       exit,
		StartSnapshot:  start,
		EndSnapshot:    end,
	}
}

// TestAuthorizeSnapshotBound covers UT-022: a phase is authorized only from a
// completed exit-zero verification on the unchanged snapshot; a trusted
// completion replays; partial, canceled, timed-out, missing-exit, nonzero, and
// stale results never authorize.
func TestAuthorizeSnapshotBound(t *testing.T) {
	t.Parallel()
	const snap = "snap-current"
	cases := []struct {
		name     string
		result   Result
		expected string
		want     bool
	}{
		{"completed exit zero unchanged", completedResult(exitCode(0), snap, snap), snap, true},
		{"nonzero exit", completedResult(exitCode(1), snap, snap), snap, false},
		{"missing exit code", completedResult(nil, snap, snap), snap, false},
		{"stale end snapshot", completedResult(exitCode(0), snap, "snap-other"), snap, false},
		{"changed start snapshot", completedResult(exitCode(0), "snap-old", snap), snap, false},
		{
			"canceled",
			Result{Completion: CompletionCanceled, StartSnapshot: snap, EndSnapshot: snap},
			snap,
			false,
		},
		{
			"timed out",
			Result{Completion: CompletionTimedOut, StartSnapshot: snap, EndSnapshot: snap},
			snap,
			false,
		},
		{
			"incomplete partial",
			Result{Completion: CompletionIncomplete, StartSnapshot: snap, EndSnapshot: snap},
			snap,
			false,
		},
		{"empty expected snapshot", completedResult(exitCode(0), snap, snap), "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Authorize(tc.result, tc.expected)
			if got.Authorized != tc.want {
				t.Fatalf("Authorize(%s) = %v (%s), want %v", tc.name, got.Authorized, got.Reason, tc.want)
			}
		})
	}
}

// TestCanReplayTrustedCompletion verifies a completed exit-zero result on the
// unchanged snapshot replays, and a stale one does not.
func TestCanReplayTrustedCompletion(t *testing.T) {
	t.Parallel()
	const snap = "snap-current"
	trusted := completedResult(exitCode(0), snap, snap)
	if !CanReplay(trusted, snap) {
		t.Fatal("CanReplay(trusted, unchanged) = false, want true")
	}
	if CanReplay(trusted, "snap-changed") {
		t.Fatal("CanReplay(trusted, changed) = true, want false")
	}
	if CanReplay(completedResult(exitCode(1), snap, snap), snap) {
		t.Fatal("CanReplay(failed) = true, want false")
	}
}
