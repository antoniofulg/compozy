package convergence

import "errors"

// Sentinel errors for convergence domain rejections. Callers match them with
// errors.Is. Configuration validation additionally wraps these with the exact
// offending field path so operators can locate the problem. Transport layers map
// each sentinel to a stable error code in a later slice; the pure domain never
// depends on that mapping.
var (
	// ErrConfigInvalid marks any convergence configuration that fails validation.
	ErrConfigInvalid = errors.New("convergence: configuration invalid")
	// ErrVerificationRequired marks a missing or empty verification command.
	ErrVerificationRequired = errors.New("convergence: verification command required")
	// ErrProfileNotFound marks a selected profile that is not defined.
	ErrProfileNotFound = errors.New("convergence: profile not found")
	// ErrModelSetupNotFound marks a selected model setup that is not defined.
	ErrModelSetupNotFound = errors.New("convergence: model setup not found")
	// ErrRouteInvalid marks a route field that cannot be resolved or is unauthorized.
	ErrRouteInvalid = errors.New("convergence: route invalid")
	// ErrFindingIdentityInvalid marks reviewer identity input that cannot produce a
	// deterministic semantic-v1 fingerprint.
	ErrFindingIdentityInvalid = errors.New("convergence: finding identity invalid")
	// ErrObservationStale marks an observation bound to a superseded snapshot that
	// cannot change current finding state.
	ErrObservationStale = errors.New("convergence: stale observation")
	// ErrDispositionUnauthorized marks a disposition attempted by an actor that lacks
	// authority for it.
	ErrDispositionUnauthorized = errors.New("convergence: disposition unauthorized")
	// ErrDispositionIncomplete marks a disposition missing required evidence or reason.
	ErrDispositionIncomplete = errors.New("convergence: disposition incomplete")
	// ErrTransitionInvalid marks an out-of-order or otherwise illegal phase transition.
	ErrTransitionInvalid = errors.New("convergence: invalid phase transition")
	// ErrReplayConflict marks a duplicate, out-of-order, or concurrent update that the
	// deterministic projection rejects.
	ErrReplayConflict = errors.New("convergence: replay conflict")
	// ErrTargetIneligible marks a convergence target that is not recognized completed
	// Compozy work.
	ErrTargetIneligible = errors.New("convergence: target ineligible")
	// ErrActivationInvalid marks an activation request that cannot be canonicalized.
	ErrActivationInvalid = errors.New("convergence: activation request invalid")
)
