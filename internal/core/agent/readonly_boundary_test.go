package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/compozy/compozy/internal/core/model"
	"github.com/compozy/compozy/internal/core/worktree"
)

func newReadOnlyClient(workingDir string) (*clientImpl, string) {
	sessionID := "sess-readonly"
	return &clientImpl{
		shutdownTimeout: time.Second,
		cfg:             ClientConfig{AccessMode: model.AccessModeReadOnly},
		sessions: map[string]*sessionImpl{
			sessionID: newSessionWithAccess(sessionID, workingDir, []string{workingDir}),
		},
	}, sessionID
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}
}

func initReadOnlyGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runReadOnlyGit(t, root, "init", "-q", "-b", "main")
	runReadOnlyGit(t, root, "config", "user.email", "readonly@example.com")
	runReadOnlyGit(t, root, "config", "user.name", "Read Only")
	runReadOnlyGit(t, root, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# initial\n"), 0o600); err != nil {
		t.Fatalf("seed README: %v", err)
	}
	runReadOnlyGit(t, root, "add", "README.md")
	runReadOnlyGit(t, root, "commit", "-q", "-m", "initial")
	return root
}

func runReadOnlyGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z",
		"GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, string(out))
	}
}

// TestReadOnlyReviewerFileWriteDenied covers IT-012: a declared read-only
// reviewer's file write is denied at the handler, the Git snapshot is unchanged,
// the denial is structured, and no file is written by the session. Contained
// reads still succeed.
func TestReadOnlyReviewerFileWriteDenied(t *testing.T) {
	t.Parallel()
	requireGit(t)
	root := initReadOnlyGitRepo(t)
	client, sessionID := newReadOnlyClient(root)

	before, err := worktree.Capture(context.Background(), root)
	if err != nil {
		t.Fatalf("capture before: %v", err)
	}

	target := filepath.Join(root, "src.go")
	_, writeErr := client.WriteTextFile(context.Background(), acp.WriteTextFileRequest{
		SessionId: acp.SessionId(sessionID),
		Path:      target,
		Content:   "package main\n",
	})
	if writeErr == nil {
		t.Fatal("WriteTextFile succeeded under read-only, want denial")
	}
	if !IsReadOnlyViolation(writeErr) {
		t.Fatalf("WriteTextFile error = %v, want ReadOnlyViolationError", writeErr)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("denied write left a file at %s (stat err=%v)", target, statErr)
	}

	after, err := worktree.Capture(context.Background(), root)
	if err != nil {
		t.Fatalf("capture after: %v", err)
	}
	if !before.Equal(after) {
		t.Fatalf("git snapshot changed after denied write: before=%s after=%s", before.Digest(), after.Digest())
	}

	readResp, err := client.ReadTextFile(context.Background(), acp.ReadTextFileRequest{
		SessionId: acp.SessionId(sessionID),
		Path:      filepath.Join(root, "README.md"),
	})
	if err != nil {
		t.Fatalf("contained read denied under read-only: %v", err)
	}
	if readResp.Content != "# initial\n" {
		t.Fatalf("unexpected read content: %q", readResp.Content)
	}
}

// TestReadOnlyReviewerPermissionDenied verifies a read-only session never
// escalates authority through a permission request.
func TestReadOnlyReviewerPermissionDenied(t *testing.T) {
	t.Parallel()
	client, _ := newReadOnlyClient(t.TempDir())

	resp, err := client.RequestPermission(context.Background(), acp.RequestPermissionRequest{
		Options: []acp.PermissionOption{
			{OptionId: "allow", Kind: acp.PermissionOptionKindAllowOnce},
			{OptionId: "reject", Kind: acp.PermissionOptionKindRejectOnce},
		},
	})
	if err != nil {
		t.Fatalf("request permission: %v", err)
	}
	if resp.Outcome.Selected == nil || resp.Outcome.Selected.OptionId != "reject" {
		t.Fatalf("read-only permission outcome = %#v, want reject selection", resp.Outcome)
	}

	canceled, err := client.RequestPermission(context.Background(), acp.RequestPermissionRequest{
		Options: []acp.PermissionOption{{OptionId: "allow", Kind: acp.PermissionOptionKindAllowOnce}},
	})
	if err != nil {
		t.Fatalf("request permission without reject option: %v", err)
	}
	if canceled.Outcome.Selected != nil {
		t.Fatalf("read-only permission selected an allow option: %#v", canceled.Outcome)
	}
}

// TestReadOnlyReviewerTerminalPolicy covers IT-013: mutating Git, file, and
// network terminal actions are denied deny-by-default and never start a process,
// while a permitted diagnostic runs to a successful exit.
func TestReadOnlyReviewerTerminalPolicy(t *testing.T) {
	t.Parallel()
	requireGit(t)
	root := initReadOnlyGitRepo(t)
	client, sessionID := newReadOnlyClient(root)

	denied := []struct {
		name    string
		command string
		args    []string
	}{
		{"git commit", "git", []string{"commit", "--allow-empty", "-m", "x"}},
		{"git push", "git", []string{"push", "origin", "main"}},
		{"file delete", "rm", []string{"-rf", "README.md"}},
		{"network fetch", "curl", []string{"https://example.com"}},
		{"shell wrapper", "bash", []string{"-c", "echo hi > pwned"}},
	}
	for _, tc := range denied {
		t.Run("Should deny "+tc.name, func(t *testing.T) {
			_, err := client.CreateTerminal(context.Background(), acp.CreateTerminalRequest{
				SessionId: acp.SessionId(sessionID),
				Command:   tc.command,
				Args:      tc.args,
			})
			if err == nil {
				t.Fatalf("CreateTerminal(%s) succeeded, want read-only denial", tc.name)
			}
			if !IsReadOnlyViolation(err) {
				t.Fatalf("CreateTerminal(%s) error = %v, want ReadOnlyViolationError", tc.name, err)
			}
		})
	}
	client.terminalMu.Lock()
	activeTerminals := len(client.terminals)
	client.terminalMu.Unlock()
	if activeTerminals != 0 {
		t.Fatalf("denied terminals started %d processes, want 0", activeTerminals)
	}
	if _, statErr := os.Stat(filepath.Join(root, "pwned")); !os.IsNotExist(statErr) {
		t.Fatalf("denied shell wrapper produced a file (stat err=%v)", statErr)
	}

	t.Run("Should allow a permitted git diagnostic", func(t *testing.T) {
		resp, err := client.CreateTerminal(context.Background(), acp.CreateTerminalRequest{
			SessionId: acp.SessionId(sessionID),
			Command:   "git",
			Args:      []string{"status", "--porcelain"},
		})
		if err != nil {
			t.Fatalf("permitted diagnostic denied: %v", err)
		}
		waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		exit, err := client.WaitForTerminalExit(waitCtx, acp.WaitForTerminalExitRequest{
			SessionId:  acp.SessionId(sessionID),
			TerminalId: resp.TerminalId,
		})
		if err != nil {
			t.Fatalf("wait for diagnostic: %v", err)
		}
		if exit.ExitCode == nil || *exit.ExitCode != 0 {
			t.Fatalf("diagnostic exit = %#v, want 0", exit.ExitCode)
		}
		if _, err := client.ReleaseTerminal(context.Background(), acp.ReleaseTerminalRequest{
			SessionId:  acp.SessionId(sessionID),
			TerminalId: resp.TerminalId,
		}); err != nil {
			t.Fatalf("release diagnostic terminal: %v", err)
		}
	})
}
