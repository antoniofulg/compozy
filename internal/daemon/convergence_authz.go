package daemon

// Convergence authorization operations model the Authorization Rule Pack
// (AUTH-001..AUTH-015) from the user stories. This package does not authenticate
// HTTP or UDS callers, so the rule pack is not a transport authorization gate.
// Local approval and resume endpoints rely on the daemon's local transport
// boundary until a transport-authenticated principal exists.
type convergenceOperation int

const (
	convergenceOpUnspecified convergenceOperation = iota
	convergenceOpCreateRun
	convergenceOpReadReviewEvidence
	convergenceOpUpdateProject
	convergenceOpWeakenVerification
	convergenceOpTransitionFinding
	convergenceOpWaiveFinding
	convergenceOpResume
	convergenceOpSelectFallback
	convergenceOpCancel
	convergenceOpPushIntegrate
	convergenceOpReplay
	convergenceOpReadOrResume
)

// convergencePrincipalRole is the role of the caller or session requesting a
// convergence operation. The pack separates a reviewer session from a fixer
// session even though both are model sessions, and separates the daemon runtime
// evaluator from a local workspace user.
type convergencePrincipalRole string

const (
	convergencePrincipalUser     convergencePrincipalRole = "user"
	convergencePrincipalReviewer convergencePrincipalRole = "reviewer"
	convergencePrincipalFixer    convergencePrincipalRole = "fixer"
	convergencePrincipalRuntime  convergencePrincipalRole = "runtime"
	convergencePrincipalObserver convergencePrincipalRole = "observer"
	convergencePrincipalUnknown  convergencePrincipalRole = "unknown"
)

// convergencePrincipal models a caller or session for rule-pack evaluation.
// RunAuthority means a user holds workspace run authority; CurrentReview means a
// reviewer is scoped to the current snapshot; ScopedToBatch means a fixer is
// scoped to the active correction batch.
type convergencePrincipal struct {
	Role          convergencePrincipalRole
	RunAuthority  bool
	CurrentReview bool
	ScopedToBatch bool
}

// convergenceAuthorization is the decision for one operation. RuleID is the
// AUTH-NNN row that governed the decision. ParkForApproval is set when a denied
// protected change must be routed to an approval proposal rather than rejected
// outright.
type convergenceAuthorization struct {
	Allowed         bool
	RuleID          string
	ParkForApproval bool
}

func convergenceAllow(rule string) convergenceAuthorization {
	return convergenceAuthorization{Allowed: true, RuleID: rule}
}

func convergenceDeny(rule string) convergenceAuthorization {
	return convergenceAuthorization{Allowed: false, RuleID: rule}
}

type convergenceAuthRule func(convergencePrincipal) convergenceAuthorization

// convergenceAuthRules is the deny-by-default policy table. Any operation absent
// from the table is denied by authorizeConvergenceOperation.
var convergenceAuthRules = map[convergenceOperation]convergenceAuthRule{
	convergenceOpCreateRun:          authorizeCreateRun,
	convergenceOpReadReviewEvidence: authorizeReadReviewEvidence,
	convergenceOpUpdateProject:      authorizeUpdateProject,
	convergenceOpWeakenVerification: authorizeWeakenVerification,
	convergenceOpTransitionFinding:  authorizeTransitionFinding,
	convergenceOpWaiveFinding:       authorizeWaiveFinding,
	convergenceOpResume:             authorizeResume,
	convergenceOpSelectFallback:     authorizeSelectFallback,
	convergenceOpCancel:             authorizeCancel,
	convergenceOpPushIntegrate:      authorizePushIntegrate,
	convergenceOpReplay:             authorizeConvergenceRead,
	convergenceOpReadOrResume:       authorizeConvergenceRead,
}

// authorizeConvergenceOperation evaluates the Authorization Rule Pack. It is a
// pure deny-by-default policy: any operation that does not resolve to an explicit
// allow rule is denied.
func authorizeConvergenceOperation(
	op convergenceOperation,
	principal convergencePrincipal,
) convergenceAuthorization {
	rule, ok := convergenceAuthRules[op]
	if !ok {
		return convergenceDeny("AUTH-002")
	}
	return rule(principal)
}

func authorizeCreateRun(p convergencePrincipal) convergenceAuthorization {
	if p.Role == convergencePrincipalUser && p.RunAuthority {
		return convergenceAllow("AUTH-001")
	}
	return convergenceDeny("AUTH-002")
}

func authorizeReadReviewEvidence(p convergencePrincipal) convergenceAuthorization {
	if p.Role == convergencePrincipalReviewer && p.CurrentReview {
		return convergenceAllow("AUTH-003")
	}
	return convergenceDeny("AUTH-004")
}

func authorizeUpdateProject(p convergencePrincipal) convergenceAuthorization {
	if p.Role == convergencePrincipalFixer && p.ScopedToBatch {
		return convergenceAllow("AUTH-005")
	}
	return convergenceDeny("AUTH-004")
}

// authorizeWeakenVerification always denies: a protected weakening by any model
// role is never applied; it parks for explicit user approval instead of failing.
func authorizeWeakenVerification(_ convergencePrincipal) convergenceAuthorization {
	return convergenceAuthorization{Allowed: false, RuleID: "AUTH-006", ParkForApproval: true}
}

func authorizeTransitionFinding(p convergencePrincipal) convergenceAuthorization {
	if p.Role == convergencePrincipalReviewer && p.CurrentReview {
		return convergenceAllow("AUTH-007")
	}
	return convergenceDeny("AUTH-007")
}

func authorizeWaiveFinding(p convergencePrincipal) convergenceAuthorization {
	if p.Role == convergencePrincipalUser && p.RunAuthority {
		return convergenceAllow("AUTH-008")
	}
	return convergenceDeny("AUTH-009")
}

func authorizeResume(p convergencePrincipal) convergenceAuthorization {
	if p.Role == convergencePrincipalUser && p.RunAuthority {
		return convergenceAllow("AUTH-010")
	}
	return convergenceDeny("AUTH-010")
}

func authorizeSelectFallback(p convergencePrincipal) convergenceAuthorization {
	if p.Role == convergencePrincipalRuntime {
		return convergenceAllow("AUTH-011")
	}
	return convergenceDeny("AUTH-011")
}

func authorizeCancel(p convergencePrincipal) convergenceAuthorization {
	if p.Role == convergencePrincipalUser && p.RunAuthority {
		return convergenceAllow("AUTH-012")
	}
	return convergenceDeny("AUTH-012")
}

// authorizePushIntegrate always denies: no convergence model phase may push or
// integrate, ever.
func authorizePushIntegrate(_ convergencePrincipal) convergenceAuthorization {
	return convergenceDeny("AUTH-013")
}

// authorizeConvergenceRead governs receipt/event/snapshot reads and replay: an
// authorized workspace observer may read and replay (AUTH-014); an unauthorized
// observer is denied without disclosing any protected field (AUTH-015).
func authorizeConvergenceRead(p convergencePrincipal) convergenceAuthorization {
	if p.RunAuthority &&
		(p.Role == convergencePrincipalUser || p.Role == convergencePrincipalObserver) {
		return convergenceAllow("AUTH-014")
	}
	return convergenceDeny("AUTH-015")
}
