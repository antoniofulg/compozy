package daemon

import "testing"

// TestAuthorizeConvergenceOperation implements UT-036: it table-tests the full
// Authorization Rule Pack (AUTH-001 through AUTH-015) covering allowed side
// effects, denied side effects, the deny-by-default fallthrough, and the
// protected-weakening park-for-approval behavior for lower-risk transitions.
func TestAuthorizeConvergenceOperation(t *testing.T) {
	t.Parallel()
	user := convergencePrincipal{Role: convergencePrincipalUser, RunAuthority: true}
	userNoAuth := convergencePrincipal{Role: convergencePrincipalUser}
	reviewerCurrent := convergencePrincipal{Role: convergencePrincipalReviewer, CurrentReview: true}
	reviewerStale := convergencePrincipal{Role: convergencePrincipalReviewer}
	fixerScoped := convergencePrincipal{Role: convergencePrincipalFixer, ScopedToBatch: true}
	runtime := convergencePrincipal{Role: convergencePrincipalRuntime}
	observer := convergencePrincipal{Role: convergencePrincipalObserver, RunAuthority: true}
	observerNoAuth := convergencePrincipal{Role: convergencePrincipalObserver}
	unknown := convergencePrincipal{Role: convergencePrincipalUnknown}

	cases := []struct {
		name      string
		op        convergenceOperation
		principal convergencePrincipal
		allowed   bool
		rule      string
		park      bool
	}{
		{"AUTH-001 authorized create", convergenceOpCreateRun, user, true, "AUTH-001", false},
		{"AUTH-002 unauthorized create", convergenceOpCreateRun, unknown, false, "AUTH-002", false},
		{"AUTH-002 create without authority", convergenceOpCreateRun, userNoAuth, false, "AUTH-002", false},
		{"AUTH-003 reviewer reads evidence", convergenceOpReadReviewEvidence, reviewerCurrent, true, "AUTH-003", false},
		{"AUTH-004 reviewer project write", convergenceOpUpdateProject, reviewerStale, false, "AUTH-004", false},
		{"AUTH-005 fixer project write", convergenceOpUpdateProject, fixerScoped, true, "AUTH-005", false},
		{"AUTH-006 fixer weakens verification", convergenceOpWeakenVerification, fixerScoped, false, "AUTH-006", true},
		{
			"AUTH-006 reviewer weakens verification",
			convergenceOpWeakenVerification,
			reviewerCurrent,
			false,
			"AUTH-006",
			true,
		},
		{
			"AUTH-007 reviewer transitions finding",
			convergenceOpTransitionFinding,
			reviewerCurrent,
			true,
			"AUTH-007",
			false,
		},
		{"AUTH-007 stale reviewer denied", convergenceOpTransitionFinding, reviewerStale, false, "AUTH-007", false},
		{"AUTH-008 user waives finding", convergenceOpWaiveFinding, user, true, "AUTH-008", false},
		{"AUTH-009 model waiver denied", convergenceOpWaiveFinding, fixerScoped, false, "AUTH-009", false},
		{"AUTH-009 reviewer waiver denied", convergenceOpWaiveFinding, reviewerCurrent, false, "AUTH-009", false},
		{"AUTH-010 user resumes", convergenceOpResume, user, true, "AUTH-010", false},
		{"AUTH-010 unauthorized resume denied", convergenceOpResume, unknown, false, "AUTH-010", false},
		{"AUTH-011 runtime selects fallback", convergenceOpSelectFallback, runtime, true, "AUTH-011", false},
		{"AUTH-011 model fallback denied", convergenceOpSelectFallback, fixerScoped, false, "AUTH-011", false},
		{"AUTH-012 user cancels", convergenceOpCancel, user, true, "AUTH-012", false},
		{"AUTH-012 unauthorized cancel denied", convergenceOpCancel, userNoAuth, false, "AUTH-012", false},
		{"AUTH-013 fixer push denied", convergenceOpPushIntegrate, fixerScoped, false, "AUTH-013", false},
		{"AUTH-013 user push denied", convergenceOpPushIntegrate, user, false, "AUTH-013", false},
		{"AUTH-014 observer replays", convergenceOpReplay, observer, true, "AUTH-014", false},
		{"AUTH-014 user reads", convergenceOpReadOrResume, user, true, "AUTH-014", false},
		{
			"AUTH-015 unauthorized observer read denied",
			convergenceOpReadOrResume,
			observerNoAuth,
			false,
			"AUTH-015",
			false,
		},
		{"AUTH-015 unknown replay denied", convergenceOpReplay, unknown, false, "AUTH-015", false},
	}
	for _, tc := range cases {
		t.Run("Should enforce "+tc.name, func(t *testing.T) {
			t.Parallel()
			got := authorizeConvergenceOperation(tc.op, tc.principal)
			if got.Allowed != tc.allowed {
				t.Fatalf("allowed = %v, want %v (rule %s)", got.Allowed, tc.allowed, got.RuleID)
			}
			if got.RuleID != tc.rule {
				t.Fatalf("rule = %q, want %q", got.RuleID, tc.rule)
			}
			if got.ParkForApproval != tc.park {
				t.Fatalf("park = %v, want %v", got.ParkForApproval, tc.park)
			}
		})
	}
}

// TestAuthorizeConvergenceDeniesUnknownOperation proves the policy is deny-by-
// default: an unrecognized operation is denied rather than silently allowed.
func TestAuthorizeConvergenceDeniesUnknownOperation(t *testing.T) {
	t.Parallel()
	t.Run("Should deny an unrecognized operation", func(t *testing.T) {
		t.Parallel()
		got := authorizeConvergenceOperation(
			convergenceOperation(999),
			convergencePrincipal{Role: convergencePrincipalUser, RunAuthority: true},
		)
		if got.Allowed {
			t.Fatalf("unknown operation must be denied by default")
		}
	})
}
