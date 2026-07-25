package agent

import (
	"context"
	"errors"
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

func writeExternalDiffProbe(t *testing.T) (string, string) {
	t.Helper()
	probe := filepath.Join(t.TempDir(), "external-diff")
	marker := probe + ".ran"
	if err := os.WriteFile(probe, []byte("#!/bin/sh\n: > \"$0.ran\"\n"), 0o700); err != nil {
		t.Fatalf("write external diff probe: %v", err)
	}
	return probe, marker
}

func runReadOnlyTerminal(t *testing.T, client *clientImpl, sessionID string, args ...string) {
	t.Helper()
	resp, err := client.CreateTerminal(context.Background(), acp.CreateTerminalRequest{
		SessionId: acp.SessionId(sessionID),
		Command:   "git",
		Args:      args,
	})
	if err != nil {
		t.Fatalf("CreateTerminal(git %s): %v", strings.Join(args, " "), err)
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	exit, err := client.WaitForTerminalExit(waitCtx, acp.WaitForTerminalExitRequest{
		SessionId:  acp.SessionId(sessionID),
		TerminalId: resp.TerminalId,
	})
	if err != nil {
		t.Fatalf("wait for git %s: %v", strings.Join(args, " "), err)
	}
	if exit.ExitCode == nil || *exit.ExitCode != 0 {
		t.Fatalf("git %s exit = %#v, want 0", strings.Join(args, " "), exit.ExitCode)
	}
	if _, err := client.ReleaseTerminal(context.Background(), acp.ReleaseTerminalRequest{
		SessionId:  acp.SessionId(sessionID),
		TerminalId: resp.TerminalId,
	}); err != nil {
		t.Fatalf("release git terminal: %v", err)
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
	outside := t.TempDir()
	client, sessionID := newReadOnlyClient(root)

	denied := []struct {
		name    string
		command string
		args    []string
		env     []acp.EnvVariable
	}{
		{name: "git commit", command: "git", args: []string{"commit", "--allow-empty", "-m", "x"}},
		{name: "git push", command: "git", args: []string{"push", "origin", "main"}},
		{name: "file delete", command: "rm", args: []string{"-rf", "README.md"}},
		{name: "network fetch", command: "curl", args: []string{"https://example.com"}},
		{name: "shell wrapper", command: "bash", args: []string{"-c", "echo hi > pwned"}},
		{name: "git alternate cwd", command: "git", args: []string{"-C", outside, "status"}},
		{name: "git alternate git dir", command: "git", args: []string{"--git-dir", outside, "status"}},
		{name: "git alternate work tree", command: "git", args: []string{"--work-tree=" + outside, "status"}},
		{
			name:    "git external diff environment",
			command: "git",
			args:    []string{"diff"},
			env: []acp.EnvVariable{
				{Name: "GIT_EXTERNAL_DIFF", Value: filepath.Join(root, "writer")},
			},
		},
		{
			name:    "git config environment",
			command: "git",
			args:    []string{"status", "--porcelain"},
			env: []acp.EnvVariable{
				{Name: "GIT_CONFIG_COUNT", Value: "1"},
				{Name: "GIT_CONFIG_KEY_0", Value: "diff.external"},
				{Name: "GIT_CONFIG_VALUE_0", Value: filepath.Join(root, "writer")},
			},
		},
	}
	for _, tc := range denied {
		t.Run("Should deny "+tc.name, func(t *testing.T) {
			_, err := client.CreateTerminal(context.Background(), acp.CreateTerminalRequest{
				SessionId: acp.SessionId(sessionID),
				Command:   tc.command,
				Args:      tc.args,
				Env:       tc.env,
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
			Env:       []acp.EnvVariable{{Name: "NO_COLOR", Value: "1"}},
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

func TestReadOnlyReviewerStripsInheritedGitEnvironment(t *testing.T) {
	requireGit(t)

	cases := []struct {
		name string
		set  func(*testing.T, string)
	}{
		{
			name: "external diff",
			set: func(t *testing.T, probe string) {
				t.Helper()
				t.Setenv("GIT_EXTERNAL_DIFF", probe)
			},
		},
		{
			name: "config parameters",
			set: func(t *testing.T, probe string) {
				t.Helper()
				t.Setenv("GIT_CONFIG_COUNT", "1")
				t.Setenv("GIT_CONFIG_KEY_0", "diff.external")
				t.Setenv("GIT_CONFIG_VALUE_0", probe)
			},
		},
	}
	for _, tc := range cases {
		t.Run("Should strip inherited Git "+tc.name, func(t *testing.T) {
			root := initReadOnlyGitRepo(t)
			if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# changed\n"), 0o600); err != nil {
				t.Fatalf("change README: %v", err)
			}
			probe, marker := writeExternalDiffProbe(t)
			tc.set(t, probe)
			client, sessionID := newReadOnlyClient(root)

			runReadOnlyTerminal(t, client, sessionID, "diff")

			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("inherited Git %s executed external diff (stat error = %v)", tc.name, err)
			}
		})
	}
}
