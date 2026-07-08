package main

import (
	"testing"

	extension "github.com/compozy/compozy/sdk/extension"
)

func TestShouldNudge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mode   extension.ExecutionMode
		status string
		want   bool
	}{
		{
			name:   "successful prd-tasks run nudges",
			mode:   extension.ExecutionModePRDTasks,
			status: "succeeded",
			want:   true,
		},
		{
			name:   "failed prd-tasks run does not nudge",
			mode:   extension.ExecutionModePRDTasks,
			status: "failed",
			want:   false,
		},
		{
			name:   "canceled prd-tasks run does not nudge",
			mode:   extension.ExecutionModePRDTasks,
			status: "canceled",
			want:   false,
		},
		{
			name:   "successful exec run does not nudge",
			mode:   extension.ExecutionModeExec,
			status: "succeeded",
			want:   false,
		},
		{
			name:   "successful pr-review run does not nudge",
			mode:   extension.ExecutionModePRReview,
			status: "succeeded",
			want:   false,
		},
		{
			name:   "empty mode does not nudge",
			mode:   extension.ExecutionMode(""),
			status: "succeeded",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldNudge(tt.mode, tt.status); got != tt.want {
				t.Fatalf("shouldNudge(%q, %q) = %v, want %v", tt.mode, tt.status, got, tt.want)
			}
		})
	}
}

func TestRunModeTrackerRecordThenTake(t *testing.T) {
	t.Parallel()

	tracker := newRunModeTracker()
	tracker.record("run-1", extension.ExecutionModePRDTasks)
	tracker.record("run-2", extension.ExecutionModeExec)

	mode, ok := tracker.take("run-1")
	if !ok || mode != extension.ExecutionModePRDTasks {
		t.Fatalf("take(run-1) = (%q, %v), want (prd-tasks, true)", mode, ok)
	}

	// The entry is forgotten after it is taken, so a run's state is released
	// once its shutdown has been observed.
	if _, ok := tracker.take("run-1"); ok {
		t.Fatal("take(run-1) second call returned ok=true, want forgotten")
	}

	// An unrelated run is unaffected.
	if mode, ok := tracker.take("run-2"); !ok || mode != extension.ExecutionModeExec {
		t.Fatalf("take(run-2) = (%q, %v), want (exec, true)", mode, ok)
	}
}

func TestRunModeTrackerTakeUnknownRun(t *testing.T) {
	t.Parallel()

	tracker := newRunModeTracker()
	if _, ok := tracker.take("missing"); ok {
		t.Fatal("take(missing) returned ok=true, want false")
	}
}
