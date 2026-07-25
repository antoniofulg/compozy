package convergence

import "fmt"

// Public run status spellings. These mirror the run store's bare status values
// (see globaldb/run_status.go): American "canceled", additive "parked".
const (
	// PublicStatusCompleted is the public run status for a clean convergence segment.
	PublicStatusCompleted = "completed"
	// PublicStatusParked is the public run status for a parked segment.
	PublicStatusParked = "parked"
	// PublicStatusFailed is the public run status for an untrustworthy segment.
	PublicStatusFailed = "failed"
	// PublicStatusCanceled is the public run status for a canceled segment.
	PublicStatusCanceled = "canceled"
)

// Public event kind wire values. These mirror pkg/compozy/events EventKind
// constants; the domain keeps them as strings so it imports no transport package.
// UT-037 cross-checks these against the events package to guard against drift.
const (
	// PublicEventCompleted is the public terminal event for a clean segment.
	PublicEventCompleted = "run.completed"
	// PublicEventParked is the public terminal event for a parked segment.
	PublicEventParked = "run.parked"
	// PublicEventFailed is the public terminal event for an untrustworthy segment.
	PublicEventFailed = "run.failed"
	// PublicEventCancelled is the public terminal event for a canceled segment.
	// The event spelling stays dotted "run.cancelled" even though the status is
	// the bare "canceled".
	PublicEventCancelled = "run.cancelled"
)

// PublicTerminal is the public run projection of a convergence terminal outcome.
type PublicTerminal struct {
	// EventKind is the public run.* terminal event kind wire value.
	EventKind string
	// Status is the public run status wire value.
	Status string
	// Reason is the precise parked reason, set only for a parked outcome.
	Reason string
}

// Public maps a fully determined convergence terminal outcome to its public run
// event kind and status. A clean outcome maps to run.completed, parked to the
// generic run.parked with its exact reason, cancellation to run.cancelled, and an
// untrustworthy segment to run.failed. It rejects a malformed outcome rather than
// guessing a status.
func (o TerminalOutcome) Public() (PublicTerminal, error) {
	switch o.Kind {
	case TerminalClean:
		return PublicTerminal{EventKind: PublicEventCompleted, Status: PublicStatusCompleted}, nil
	case TerminalParked:
		if !o.Reason.IsValid() {
			return PublicTerminal{}, fmt.Errorf("%w: parked outcome requires a valid reason", ErrTransitionInvalid)
		}
		return PublicTerminal{
			EventKind: PublicEventParked,
			Status:    PublicStatusParked,
			Reason:    string(o.Reason),
		}, nil
	case TerminalFailed:
		return PublicTerminal{EventKind: PublicEventFailed, Status: PublicStatusFailed}, nil
	case TerminalCancelled:
		return PublicTerminal{EventKind: PublicEventCancelled, Status: PublicStatusCanceled}, nil
	default:
		return PublicTerminal{}, fmt.Errorf("%w: unrecognized terminal kind %q", ErrTransitionInvalid, o.Kind)
	}
}
