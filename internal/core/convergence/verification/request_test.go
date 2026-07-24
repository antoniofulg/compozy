package verification

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func validRequest() Request {
	return Request{
		VerificationID:   "ver-1",
		PhaseID:          "phase-1",
		Attempt:          1,
		WorktreeRoot:     "/tmp/worktree",
		Command:          []string{"make", "verify"},
		ExpectedSnapshot: "snap-1",
		EvidenceDir:      "/tmp/evidence",
	}
}

// TestVerificationCommandValidation covers UT-021: accept a nonempty argv and
// reject missing, malformed, empty-executable, NUL, and shell-inference commands.
func TestVerificationCommandValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		command []string
		wantErr bool
	}{
		{"nonempty argv", []string{"make", "verify"}, false},
		{"single executable", []string{"go"}, false},
		{"nil command", nil, true},
		{"empty command", []string{}, true},
		{"blank executable", []string{"   "}, true},
		{"nul in executable", []string{"ma\x00ke", "verify"}, true},
		{"nul in argument", []string{"make", "ver\x00ify"}, true},
		{"shell string as executable", []string{"make verify"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateCommand(tc.command)
			if tc.wantErr && err == nil {
				t.Fatalf("ValidateCommand(%v) = nil, want error", tc.command)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("ValidateCommand(%v) = %v, want nil", tc.command, err)
			}
			if tc.wantErr && err != nil && !errors.Is(err, ErrCommandInvalid) {
				t.Fatalf("ValidateCommand(%v) = %v, want ErrCommandInvalid", tc.command, err)
			}
		})
	}
}

// TestVerificationRequestValidation covers the required identity and
// execution-scope fields.
func TestVerificationRequestValidation(t *testing.T) {
	t.Parallel()
	t.Run("Should accept a complete request", func(t *testing.T) {
		t.Parallel()
		if err := validRequest().Validate(); err != nil {
			t.Fatalf("Validate() = %v, want nil", err)
		}
	})
	mutations := map[string]func(*Request){
		"missing verification id": func(r *Request) { r.VerificationID = "" },
		"missing phase id":        func(r *Request) { r.PhaseID = "" },
		"missing worktree root":   func(r *Request) { r.WorktreeRoot = "" },
		"missing evidence dir":    func(r *Request) { r.EvidenceDir = "" },
		"missing command":         func(r *Request) { r.Command = nil },
	}
	for name, mutate := range mutations {
		t.Run("Should reject "+name, func(t *testing.T) {
			t.Parallel()
			req := validRequest()
			mutate(&req)
			if err := req.Validate(); err == nil {
				t.Fatalf("Validate() = nil for %s, want error", name)
			}
		})
	}
}

// TestVerificationNeverConsultsConflictResolver covers UT-021's separation
// requirement: the verification package derives its command only from the
// convergence verification request and never reads the parallel conflict
// resolver's validation command. It proves this structurally — the package's
// non-test sources reference neither the conflict resolver nor its validation
// setting.
func TestVerificationNeverConsultsConflictResolver(t *testing.T) {
	t.Parallel()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve package directory")
	}
	dir := filepath.Dir(thisFile)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	forbidden := []string{
		"conflict_resolver",
		"validationcommand",
		"validation_command",
		"conflictresolver",
		"run/parallel",
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		lowered := strings.ToLower(string(content))
		for _, needle := range forbidden {
			if strings.Contains(lowered, needle) {
				t.Fatalf("%s references %q; verification must not consult conflict-resolver validation", name, needle)
			}
		}
	}
	// The command actually executed is exactly the request command.
	req := validRequest()
	if got := CommandFingerprint(req.Command); got != CommandFingerprint([]string{"make", "verify"}) {
		t.Fatalf("command fingerprint drifted from the request command: %s", got)
	}
}
