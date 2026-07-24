package verification

import (
	"fmt"
	"strings"
)

// Authorization is the decision to permit another convergence phase from a
// verification result. Reason is a short, redacted justification for logs and
// receipts.
type Authorization struct {
	Authorized bool
	Reason     string
}

// Authorize reports whether result may authorize progress on expectedSnapshot.
// Progress is authorized only from a completed exit-zero verification whose start
// and end snapshots both equal the unchanged expected snapshot. Failed,
// canceled, timed-out, incomplete, missing-exit, and stale results never
// authorize a phase; the absence of a trustworthy completed exit is never a pass.
func Authorize(result Result, expectedSnapshot string) Authorization {
	expected := strings.TrimSpace(expectedSnapshot)
	if expected == "" {
		return deny("expected snapshot is required")
	}
	if result.Completion != CompletionCompleted {
		return deny(fmt.Sprintf("verification did not complete (%s)", result.Completion))
	}
	if result.ExitCode == nil {
		return deny("verification produced no exit code")
	}
	if *result.ExitCode != 0 {
		return deny(fmt.Sprintf("verification exited nonzero (%d)", *result.ExitCode))
	}
	if strings.TrimSpace(result.StartSnapshot) != expected ||
		strings.TrimSpace(result.EndSnapshot) != expected {
		return deny("verification snapshot changed; passing evidence is stale")
	}
	return Authorization{Authorized: true, Reason: "completed exit-zero on the unchanged expected snapshot"}
}

// CanReplay reports whether a stored completed result can be replayed against the
// current snapshot after a transport loss, without re-running the command. Replay
// is legal exactly when the same result would authorize the current snapshot.
func CanReplay(result Result, currentSnapshot string) bool {
	return Authorize(result, currentSnapshot).Authorized
}

func deny(reason string) Authorization {
	return Authorization{Authorized: false, Reason: reason}
}
