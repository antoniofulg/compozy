package workspace

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/compozy/compozy/internal/core/convergence"
)

const convergenceGlobalTOML = `
[convergence]
profile = "quality"
model_setup = "balanced"

[convergence.verification]
command = ["make", "verify"]

[convergence.profiles.quality]
max_review_rounds = 5

[convergence.model_setups.balanced.review]
ide = "codex"
model = "reviewer"
reasoning_effort = "high"

[convergence.model_setups.balanced.review.fallback]
ide = "claude"
model = "reviewer-fallback"
reasoning_effort = "high"

[convergence.model_setups.balanced.correction]
ide = "codex"
model = "fixer"

[convergence.model_setups.balanced.correction.by_severity.critical]
model = "fixer-critical"
reasoning_effort = "high"

[convergence.model_setups.balanced.correction.fallback]
ide = "claude"
model = "fixer-fallback"
`

const convergenceWorkspaceTOML = `
[convergence.profiles.quality]
max_finding_attempts = 4
`

func TestLoadConfigMergesConvergenceWorkspaceOverGlobal(t *testing.T) {
	homeDir := isolateWorkspaceConfigHome(t)
	root := t.TempDir()
	writeGlobalConfig(t, homeDir, convergenceGlobalTOML)
	writeWorkspaceConfig(t, root, convergenceWorkspaceTOML)

	cfg, _, err := LoadConfig(context.Background(), root)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	assertConvergenceMerge(t, cfg.Convergence)
	assertConvergenceProvenance(t, homeDir, root)
}

func assertConvergenceMerge(t *testing.T, cfg convergence.Config) {
	t.Helper()
	if cfg.Profile == nil || *cfg.Profile != "quality" {
		t.Fatalf("unexpected profile selection: %#v", cfg.Profile)
	}
	if cfg.ModelSetup == nil || *cfg.ModelSetup != "balanced" {
		t.Fatalf("unexpected model setup selection: %#v", cfg.ModelSetup)
	}
	quality := cfg.Profiles["quality"]
	if quality.MaxReviewRounds == nil || *quality.MaxReviewRounds != 5 {
		t.Fatalf("expected global max_review_rounds=5, got %#v", quality.MaxReviewRounds)
	}
	if quality.MaxFindingAttempts == nil || *quality.MaxFindingAttempts != 4 {
		t.Fatalf("expected workspace max_finding_attempts=4, got %#v", quality.MaxFindingAttempts)
	}
	if cfg.Verification.Command == nil || strings.Join(*cfg.Verification.Command, " ") != "make verify" {
		t.Fatalf("unexpected verification command: %#v", cfg.Verification.Command)
	}
	balanced := cfg.ModelSetups["balanced"]
	if balanced.Review.Fallback == nil || balanced.Review.Fallback.Model == nil ||
		*balanced.Review.Fallback.Model != "reviewer-fallback" {
		t.Fatalf("unexpected review fallback: %#v", balanced.Review.Fallback)
	}
	if route, ok := balanced.Correction.BySeverity["critical"]; !ok ||
		route.Model == nil || *route.Model != "fixer-critical" {
		t.Fatalf("unexpected critical severity route: %#v", balanced.Correction.BySeverity)
	}
}

func assertConvergenceProvenance(t *testing.T, homeDir, root string) {
	t.Helper()
	globalCfg, _, err := LoadGlobalConfigFile(
		context.Background(), filepath.Join(homeDir, ".compozy", "config.toml"))
	if err != nil {
		t.Fatalf("load global convergence: %v", err)
	}
	workspaceCfg, _, err := LoadConfigFile(
		context.Background(), filepath.Join(root, ".compozy", "config.toml"))
	if err != nil {
		t.Fatalf("load workspace convergence: %v", err)
	}
	frozen, err := convergence.Freeze(convergence.FreezeInput{
		Global:    globalCfg.Convergence,
		Workspace: workspaceCfg.Convergence,
	})
	if err != nil {
		t.Fatalf("freeze: %v", err)
	}
	if frozen.LimitSources.MaxReviewRounds != convergence.SourceGlobal {
		t.Fatalf("expected global provenance for max_review_rounds, got %s", frozen.LimitSources.MaxReviewRounds)
	}
	if frozen.LimitSources.MaxFindingAttempts != convergence.SourceWorkspace {
		t.Fatalf("expected workspace provenance for max_finding_attempts, got %s",
			frozen.LimitSources.MaxFindingAttempts)
	}
}

func TestLoadConfigRejectsInvalidConvergenceFields(t *testing.T) {
	cases := map[string]struct {
		toml string
		want string
	}{
		"zero no_progress_rounds": {
			"[convergence.profiles.quality]\nno_progress_rounds = 0\n",
			"no_progress_rounds",
		},
		"malformed duration": {
			"[convergence.profiles.quality]\nreview_admission_timeout = \"soon\"\n",
			"review_admission_timeout",
		},
		"reserved default profile": {
			"[convergence.profiles.default]\nmax_review_rounds = 3\n",
			"reserved",
		},
		"blank verification executable": {
			"[convergence.verification]\ncommand = [\"\"]\n",
			"nonblank executable",
		},
		"unknown severity key": {
			"[convergence.model_setups.x.correction.by_severity.urgent]\nmodel = \"fixer\"\n",
			"critical, high, medium, low",
		},
		"invalid review ide": {
			"[convergence.model_setups.x.review]\nide = \"not-an-ide\"\n",
			"convergence.model_setups.x.review.ide",
		},
	}
	for name, tc := range cases {
		t.Run("Should reject "+name, func(t *testing.T) {
			root := t.TempDir()
			writeWorkspaceConfig(t, root, tc.toml)
			_, _, err := loadConfigWithIsolatedHome(t, root)
			if err == nil {
				t.Fatalf("expected error for %s", name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error to mention %q, got %v", tc.want, err)
			}
		})
	}
}
