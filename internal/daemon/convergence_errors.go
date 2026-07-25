package daemon

import (
	"errors"
	"net/http"

	"github.com/compozy/compozy/internal/api/contract"
	apicore "github.com/compozy/compozy/internal/api/core"
	"github.com/compozy/compozy/internal/core/convergence"
)

// convergenceCodeStatus maps every stable convergence transport code to its HTTP
// status. Configuration and eligibility problems are unprocessable-entity; state,
// staleness, and integrity problems are conflicts. The map is the single place the
// daemon assigns statuses so every convergence route answers consistently.
var convergenceCodeStatus = map[string]int{
	convergence.CodeConfigInvalid:        http.StatusUnprocessableEntity,
	convergence.CodeVerificationRequired: http.StatusUnprocessableEntity,
	convergence.CodeReadOnlyUnsupported:  http.StatusUnprocessableEntity,
	convergence.CodeTargetIneligible:     http.StatusUnprocessableEntity,
	convergence.CodeAlreadyActive:        http.StatusConflict,
	convergence.CodeFingerprintMismatch:  http.StatusConflict,
	convergence.CodeNotParked:            http.StatusConflict,
	convergence.CodeResumeCursorStale:    http.StatusConflict,
	convergence.CodeApprovalStale:        http.StatusConflict,
	convergence.CodeWorkspaceChanged:     http.StatusConflict,
	convergence.CodeUnknownOutcome:       http.StatusConflict,
	convergence.CodeIntegrityFailed:      http.StatusConflict,
}

// convergenceCodeMessage is the bounded transport message per code. Messages are
// fixed and never echo review prose, model output, absolute paths, or stored
// authorization. The concrete field/identifier context travels in the redacted,
// allow-listed details map instead.
var convergenceCodeMessage = map[string]string{
	convergence.CodeConfigInvalid:        "convergence configuration is invalid",
	convergence.CodeVerificationRequired: "convergence requires a configured verification command",
	convergence.CodeReadOnlyUnsupported:  "a configured reviewer adapter cannot enforce read-only review",
	convergence.CodeTargetIneligible:     "the target is not recognized completed work eligible for convergence",
	convergence.CodeAlreadyActive:        "an active convergence run already exists for this target",
	convergence.CodeFingerprintMismatch:  "the request fingerprint does not match the stored convergence result",
	convergence.CodeNotParked:            "the convergence segment is not parked for the requested operation",
	convergence.CodeResumeCursorStale:    "the resume cursor is stale or already claimed",
	convergence.CodeApprovalStale:        "the approval proposal or snapshot has changed",
	convergence.CodeWorkspaceChanged:     "the workspace changed outside a convergence phase checkpoint",
	convergence.CodeUnknownOutcome:       "the durable convergence outcome cannot be determined",
	convergence.CodeIntegrityFailed:      "the durable convergence state failed integrity validation",
}

// convergenceProblem maps a convergence domain error to a stable, redacted
// transport problem. It returns nil when err carries no convergence transport
// code so callers can fall back to their generic error handling. Details are
// reduced to the allow-listed, redacted subset by convergence.SafeDetails, which
// keeps protected review content, model output, and unrestricted absolute paths
// out of the transport envelope by construction.
func convergenceProblem(err error, details map[string]string) *apicore.Problem {
	if err == nil {
		return nil
	}
	if errors.Is(err, errConvergenceApprovalInvalid) {
		return apicore.NewProblem(
			http.StatusUnprocessableEntity,
			string(contract.CodeValidationError),
			"convergence approval request is invalid",
			convergenceSafeDetails(details),
			err,
		)
	}
	code, ok := convergence.TransportCode(err)
	if !ok {
		return nil
	}
	status, known := convergenceCodeStatus[code]
	if !known {
		status = http.StatusConflict
	}
	message, ok := convergenceCodeMessage[code]
	if !ok {
		message = "convergence request could not be processed"
	}
	return apicore.NewProblem(status, code, message, convergenceSafeDetails(details), err)
}

// convergenceSafeDetails redacts and allow-lists the supplied detail map and
// converts it to the transport envelope's untyped detail shape. It returns nil
// when nothing survives redaction so the envelope omits an empty details object.
func convergenceSafeDetails(details map[string]string) map[string]any {
	safe := convergence.SafeDetails(details)
	if len(safe) == 0 {
		return nil
	}
	out := make(map[string]any, len(safe))
	for key, value := range safe {
		out[key] = value
	}
	return out
}

func convergenceControlError(err error, details map[string]string) error {
	if problem := convergenceProblem(err, details); problem != nil {
		return problem
	}
	return err
}
