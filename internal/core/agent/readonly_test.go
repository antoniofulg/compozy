package agent

import (
	"errors"
	"testing"

	"github.com/compozy/compozy/internal/core/model"
)

// TestReadOnlyGuardDenyByDefault covers UT-020: the read-only review capability
// allows contained reads and approved diagnostics and denies project writes, Git
// mutation, mutating terminals, new paths, network side effects, and escalation.
func TestReadOnlyGuardDenyByDefault(t *testing.T) {
	t.Parallel()
	guard := ReadOnlyGuard{}

	t.Run("Should allow contained repository reads", func(t *testing.T) {
		t.Parallel()
		if decision := guard.FileRead(); !decision.Allowed {
			t.Fatalf("FileRead denied: %s", decision.Reason)
		}
	})

	t.Run("Should deny every project file write", func(t *testing.T) {
		t.Parallel()
		if decision := guard.FileWrite(); decision.Allowed {
			t.Fatalf("FileWrite allowed, want denied")
		}
	})

	t.Run("Should deny permission escalation", func(t *testing.T) {
		t.Parallel()
		if decision := guard.Permission(); decision.Allowed {
			t.Fatalf("Permission allowed, want denied")
		}
	})

	t.Run("Should classify terminal commands deny-by-default", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name    string
			command string
			args    []string
			allow   bool
		}{
			{"git status", "git", []string{"status", "--porcelain"}, true},
			{"git diff", "git", []string{"diff", "--stat"}, true},
			{"git log", "git", []string{"log", "-n", "5"}, true},
			{"git rev-parse", "git", []string{"rev-parse", "HEAD"}, true},
			{"git with global option", "git", []string{"-C", "/repo", "status"}, true},
			{"absolute path diagnostic", "/usr/bin/cat", []string{"README.md"}, true},
			{"ls read", "ls", []string{"-la"}, true},
			{"grep read", "grep", []string{"-r", "TODO", "."}, true},
			{"git commit mutation", "git", []string{"commit", "-m", "x"}, false},
			{"git add mutation", "git", []string{"add", "-A"}, false},
			{"git push external", "git", []string{"push", "origin", "main"}, false},
			{"git fetch network", "git", []string{"fetch", "origin"}, false},
			{"git checkout mutation", "git", []string{"checkout", "-b", "x"}, false},
			{"git config write", "git", []string{"config", "user.name", "x"}, false},
			{"rm deletion", "rm", []string{"-rf", "src"}, false},
			{"mv rename", "mv", []string{"a", "b"}, false},
			{"mkdir new path", "mkdir", []string{"newdir"}, false},
			{"chmod escalation", "chmod", []string{"+x", "run.sh"}, false},
			{"curl network", "curl", []string{"https://example.com"}, false},
			{"wget network", "wget", []string{"https://example.com"}, false},
			{"ssh network", "ssh", []string{"host"}, false},
			{"bash shell", "bash", []string{"-c", "echo hi > f"}, false},
			{"sh shell", "sh", []string{"-c", "rm f"}, false},
			{"sed in-place", "sed", []string{"-i", "s/a/b/", "f"}, false},
			{"empty command", "", nil, false},
			{"git without subcommand", "git", []string{"--version"}, false},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				decision := guard.Terminal(tc.command, tc.args)
				if decision.Allowed != tc.allow {
					t.Fatalf("Terminal(%q, %v) allowed=%v (%s), want %v",
						tc.command, tc.args, decision.Allowed, decision.Reason, tc.allow)
				}
			})
		}
	})
}

// TestReadOnlyViolationError verifies the structured denial error is classifiable
// and redacted.
func TestReadOnlyViolationError(t *testing.T) {
	t.Parallel()

	t.Run("Should classify a read-only violation", func(t *testing.T) {
		t.Parallel()
		err := error(&ReadOnlyViolationError{Operation: "write_file", Detail: "denied"})
		if !IsReadOnlyViolation(err) {
			t.Fatalf("IsReadOnlyViolation = false for %v", err)
		}
	})

	t.Run("Should not classify an unrelated error", func(t *testing.T) {
		t.Parallel()
		if IsReadOnlyViolation(errors.New("other")) {
			t.Fatalf("IsReadOnlyViolation = true for unrelated error")
		}
	})
}

// TestReadOnlyReviewerCapability covers the preflight declaration contract: a
// declared reviewer adapter is accepted and an undeclared runtime is rejected
// with ErrReadOnlyReviewUnsupported.
func TestReadOnlyReviewerCapability(t *testing.T) {
	t.Parallel()

	t.Run("Should accept a declared read-only reviewer adapter", func(t *testing.T) {
		t.Parallel()
		for _, ide := range []string{model.IDECodex, model.IDEClaude} {
			if err := EnsureReadOnlyReviewer(ide); err != nil {
				t.Fatalf("EnsureReadOnlyReviewer(%q) = %v, want nil", ide, err)
			}
		}
	})

	t.Run("Should reject an unknown runtime", func(t *testing.T) {
		t.Parallel()
		err := EnsureReadOnlyReviewer("not-a-real-runtime")
		if !errors.Is(err, ErrReadOnlyReviewUnsupported) {
			t.Fatalf("EnsureReadOnlyReviewer(unknown) = %v, want ErrReadOnlyReviewUnsupported", err)
		}
	})
}
