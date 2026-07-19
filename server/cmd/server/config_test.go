package main

import (
	"strings"
	"testing"
)

// setEnv sets envs for the duration of a test (auto-restored).
func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

// TestLoadConfig_RealConstruction_FailFast — DRYRUN=false requires all construction
// creds; missing any one must return an error at startup.
func TestLoadConfig_RealConstruction_FailFast(t *testing.T) {
	setEnv(t, map[string]string{
		"ARCHISTRATOR_POSTGRES_URL":               "postgres://x",
		"ARCHISTRATOR_CONSTRUCTION_DRYRUN":        "false",
		"ARCHISTRATOR_CONSTRUCTION_REPO_OWNER":    "mixofreality-studio",
		"ARCHISTRATOR_CONSTRUCTION_REPO_NAME":     "archistrator",
		"ARCHISTRATOR_CONSTRUCTION_WORKFLOW_FILE": "aiarch-construct.yml",
		"ARCHISTRATOR_CONSTRUCTION_REF":           "main",
		// App creds intentionally absent
	})
	if _, err := loadResolvedConfig(); err == nil {
		t.Fatal("expected error when DRYRUN=false and app creds missing")
	}
}

// TestLoadConfig_RealConstruction_OK — all creds present → no error.
func TestLoadConfig_RealConstruction_OK(t *testing.T) {
	setEnv(t, map[string]string{
		"ARCHISTRATOR_POSTGRES_URL":               "postgres://x",
		"ARCHISTRATOR_CONSTRUCTION_DRYRUN":        "false",
		"ARCHISTRATOR_CONSTRUCTION_REPO_OWNER":    "mixofreality-studio",
		"ARCHISTRATOR_CONSTRUCTION_REPO_NAME":     "archistrator",
		"ARCHISTRATOR_CONSTRUCTION_WORKFLOW_FILE": "aiarch-construct.yml",
		"ARCHISTRATOR_CONSTRUCTION_REF":           "main",
		"ARCHISTRATOR_GITHUB_APP_ID":              "12345",
		"ARCHISTRATOR_GITHUB_APP_PRIVATE_KEY_PEM": "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAA==\n-----END RSA PRIVATE KEY-----",
		"ARCHISTRATOR_GITHUB_APP_SLUG":            "archistrator",
		"ARCHISTRATOR_ARTIFACT_REPO_URL":          "https://github.com/mixofreality-studio/archistrator.git",
	})
	if _, err := loadResolvedConfig(); err != nil {
		t.Fatalf("expected no error with all real-construction creds: %v", err)
	}
}

// TestLoadConfig_CloudEmptyAppSlug_FailFast — a CLOUD-profile server (git-local off)
// with the GitHub App rail configured but NO app slug must fail at boot: the seated
// design workflow would render WITHOUT allowed_bots and every bot-dispatched
// claude-code-action draft run would fail with "Workflow initiated by non-human
// actor" (production QA 2026-07-10).
func TestLoadConfig_CloudEmptyAppSlug_FailFast(t *testing.T) {
	setEnv(t, map[string]string{
		"ARCHISTRATOR_POSTGRES_URL":               "postgres://x",
		"ARCHISTRATOR_CONSTRUCTION_DRYRUN":        "true",
		"ARCHISTRATOR_GITHUB_APP_ID":              "12345",
		"ARCHISTRATOR_GITHUB_APP_PRIVATE_KEY_PEM": "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAA==\n-----END RSA PRIVATE KEY-----",
		"ARCHISTRATOR_GITHUB_ACCOUNT":             "mixofreality-studio",
		"ARCHISTRATOR_GITHUB_APP_SLUG":            "",
		"ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL":    "", // default false → cloud profile
	})
	_, err := loadResolvedConfig()
	if err == nil {
		t.Fatal("expected boot error: cloud profile + App creds + empty ARCHISTRATOR_GITHUB_APP_SLUG")
	}
	if !strings.Contains(err.Error(), "ARCHISTRATOR_GITHUB_APP_SLUG") {
		t.Fatalf("expected error to name ARCHISTRATOR_GITHUB_APP_SLUG, got: %v", err)
	}
	if !strings.Contains(err.Error(), "allowed_bots") || !strings.Contains(err.Error(), "non-human actor") {
		t.Fatalf("expected error to explain the allowed_bots consequence, got: %v", err)
	}
}

// TestLoadConfig_GitLocalEmptyAppSlug_OK — the same App creds on a git-LOCAL dev
// server stay valid without a slug: the design workflow simply omits allowed_bots,
// which is the documented unconfigured-dev posture (agenticdesign.go).
func TestLoadConfig_GitLocalEmptyAppSlug_OK(t *testing.T) {
	setEnv(t, map[string]string{
		"ARCHISTRATOR_POSTGRES_URL":               "postgres://x",
		"ARCHISTRATOR_CONSTRUCTION_DRYRUN":        "true",
		"ARCHISTRATOR_GITHUB_APP_ID":              "12345",
		"ARCHISTRATOR_GITHUB_APP_PRIVATE_KEY_PEM": "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAA==\n-----END RSA PRIVATE KEY-----",
		"ARCHISTRATOR_GITHUB_ACCOUNT":             "mixofreality-studio",
		"ARCHISTRATOR_GITHUB_APP_SLUG":            "",
		"ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL":    "true",
		"ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL": "file:///tmp/proj.git",
	})
	if _, err := loadResolvedConfig(); err != nil {
		t.Fatalf("git-local profile must not require the app slug, got: %v", err)
	}
}

// TestLoadConfig_CloudNoAppCreds_NoSlugNeeded — a cloud-profile server with NO App
// creds (dormant design rail) never seats a workflow, so no slug is required.
func TestLoadConfig_CloudNoAppCreds_NoSlugNeeded(t *testing.T) {
	setEnv(t, map[string]string{
		"ARCHISTRATOR_POSTGRES_URL":               "postgres://x",
		"ARCHISTRATOR_CONSTRUCTION_DRYRUN":        "true",
		"ARCHISTRATOR_GITHUB_APP_ID":              "",
		"ARCHISTRATOR_GITHUB_APP_PRIVATE_KEY_PEM": "",
		"ARCHISTRATOR_GITHUB_ACCOUNT":             "",
		"ARCHISTRATOR_CONSTRUCTION_REPO_OWNER":    "", // account chains off this too
		"ARCHISTRATOR_GITHUB_APP_SLUG":            "",
	})
	if _, err := loadResolvedConfig(); err != nil {
		t.Fatalf("repo-less cloud server must boot without the app slug, got: %v", err)
	}
}

// TestLoadConfig_RealConstruction_RequiresArtifactRepoURL — the real-path selection
// needs the git-forward artifact store (main.go: pipeline != nil && artifacts != nil),
// which is constructed only when ArtifactRepoURL is set. DRYRUN=false without it must
// fail fast and name the missing var, not silently skip construction registration.
func TestLoadConfig_RealConstruction_RequiresArtifactRepoURL(t *testing.T) {
	setEnv(t, map[string]string{
		"ARCHISTRATOR_POSTGRES_URL":               "postgres://x",
		"ARCHISTRATOR_CONSTRUCTION_DRYRUN":        "false",
		"ARCHISTRATOR_CONSTRUCTION_REPO_OWNER":    "mixofreality-studio",
		"ARCHISTRATOR_CONSTRUCTION_REPO_NAME":     "archistrator",
		"ARCHISTRATOR_CONSTRUCTION_WORKFLOW_FILE": "aiarch-construct.yml",
		"ARCHISTRATOR_CONSTRUCTION_REF":           "main",
		"ARCHISTRATOR_GITHUB_APP_ID":              "12345",
		"ARCHISTRATOR_GITHUB_APP_PRIVATE_KEY_PEM": "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAA==\n-----END RSA PRIVATE KEY-----",
		// ARCHISTRATOR_ARTIFACT_REPO_URL intentionally absent
	})
	_, err := loadResolvedConfig()
	if err == nil {
		t.Fatal("expected error when DRYRUN=false and ARCHISTRATOR_ARTIFACT_REPO_URL missing")
	}
	if !strings.Contains(err.Error(), "ARCHISTRATOR_ARTIFACT_REPO_URL") {
		t.Fatalf("expected error to name ARCHISTRATOR_ARTIFACT_REPO_URL, got: %v", err)
	}
}

// TestMissingFor_Local_NoPostgres — the local (git-local) profile declares
// ZERO required env vars: project state is git, and Postgres exists only for
// usage metering, which the local profile's usageAccess arm no-ops (Task 2,
// local-first-init-funnel). Regression guard on the generated
// requiredEnvByProfile table (config.gen.go, emitted by configgen from
// project.json's deployment.infrastructure "postgres" decl's profiles list).
func TestMissingFor_Local_NoPostgres(t *testing.T) {
	if got := MissingFor("local"); len(got) != 0 {
		t.Fatalf(`MissingFor("local") = %v, want empty (no infra is required-class in every... local's declared profiles)`, got)
	}
}

// TestLoadConfig_Local_NoPostgres_OK — a bare git-local boot with NO
// ARCHISTRATOR_POSTGRES_URL set must resolve: local mode has zero external
// dependencies (no database, period — project state is git; usageAccess is a
// local no-op arm; there is no other Postgres-required check left in the
// local path). Was: FAIL ("ARCHISTRATOR_POSTGRES_URL is required") before
// Task 2 — config_adapter.go's Postgres guard was unconditional across every
// profile.
func TestLoadConfig_Local_NoPostgres_OK(t *testing.T) {
	setEnv(t, map[string]string{
		"ARCHISTRATOR_POSTGRES_URL":               "",
		"ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL":    "true",
		"ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL": "file:///tmp/proj.git",
		"ARCHISTRATOR_CONSTRUCTION_DRYRUN":        "true",
	})
	if _, err := loadResolvedConfig(); err != nil {
		t.Fatalf("git-local profile must boot without Postgres, got: %v", err)
	}
}

// TestLoadConfig_Cloud_NoPostgres_FailFast — the cloud profile is UNCHANGED:
// Postgres stays a hard dependency there (operatedSystemStateAccess +
// usageAccess are both still Postgres-backed on cloud).
func TestLoadConfig_Cloud_NoPostgres_FailFast(t *testing.T) {
	setEnv(t, map[string]string{
		"ARCHISTRATOR_POSTGRES_URL":            "",
		"ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL": "false",
		"ARCHISTRATOR_CONSTRUCTION_DRYRUN":     "true",
	})
	if _, err := loadResolvedConfig(); err == nil {
		t.Fatal("expected error: cloud profile still requires ARCHISTRATOR_POSTGRES_URL")
	}
}

// TestConstructionWorkflowFileDefault verifies the default construction workflow
// file is aiarch-construct.yml when the env var is unset.
func TestConstructionWorkflowFileDefault(t *testing.T) {
	setEnv(t, map[string]string{
		"ARCHISTRATOR_POSTGRES_URL":               "postgres://x",
		"ARCHISTRATOR_CONSTRUCTION_DRYRUN":        "true",
		"ARCHISTRATOR_CONSTRUCTION_WORKFLOW_FILE": "",
	})
	cfg, err := loadResolvedConfig()
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	if cfg.ConstructionWorkflowFile != "aiarch-construct.yml" {
		t.Errorf("default ConstructionWorkflowFile = %q, want aiarch-construct.yml", cfg.ConstructionWorkflowFile)
	}
}

// TestLoadConfig_LocalRealConstruction_NoGitHubCreds_OK — local-first-init-funnel
// Task 6: a LOCAL profile with DRYRUN=false and NO GitHub App creds at all must
// boot cleanly (the local construction executor needs none of the GitHub-Actions-
// specific creds validateConstructionCreds still requires on the cloud profile).
func TestLoadConfig_LocalRealConstruction_NoGitHubCreds_OK(t *testing.T) {
	setEnv(t, map[string]string{
		"ARCHISTRATOR_POSTGRES_URL":               "",
		"ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL":    "true",
		"ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL": "file:///tmp/proj.git",
		"ARCHISTRATOR_CONSTRUCTION_DRYRUN":        "false",
		// deliberately NO GitHub App creds, NO construction repo owner/name,
		// NO ARCHISTRATOR_ARTIFACT_REPO_URL (must default — see next assertion).
		"ARCHISTRATOR_GITHUB_APP_ID":              "",
		"ARCHISTRATOR_GITHUB_APP_PRIVATE_KEY_PEM": "",
		"ARCHISTRATOR_CONSTRUCTION_REPO_OWNER":    "",
		"ARCHISTRATOR_CONSTRUCTION_REPO_NAME":     "",
		"ARCHISTRATOR_ARTIFACT_REPO_URL":          "",
	})
	cfg, err := loadResolvedConfig()
	if err != nil {
		t.Fatalf("expected a local, no-creds, DRYRUN=false server to boot, got: %v", err)
	}
	if cfg.ArtifactRepoURL != cfg.ProjectStateGitRepoURL {
		t.Fatalf("ArtifactRepoURL = %q, want it defaulted to ProjectStateGitRepoURL %q", cfg.ArtifactRepoURL, cfg.ProjectStateGitRepoURL)
	}
}

// TestLoadConfig_LocalRealConstruction_ExplicitArtifactRepoURL_NotOverridden — an
// operator-supplied ARCHISTRATOR_ARTIFACT_REPO_URL on the local profile is NOT
// clobbered by the Task 6 default.
func TestLoadConfig_LocalRealConstruction_ExplicitArtifactRepoURL_NotOverridden(t *testing.T) {
	setEnv(t, map[string]string{
		"ARCHISTRATOR_POSTGRES_URL":               "",
		"ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL":    "true",
		"ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL": "file:///tmp/proj.git",
		"ARCHISTRATOR_CONSTRUCTION_DRYRUN":        "false",
		"ARCHISTRATOR_ARTIFACT_REPO_URL":          "file:///tmp/other-artifacts.git",
	})
	cfg, err := loadResolvedConfig()
	if err != nil {
		t.Fatalf("expected boot to succeed, got: %v", err)
	}
	if cfg.ArtifactRepoURL != "file:///tmp/other-artifacts.git" {
		t.Fatalf("ArtifactRepoURL = %q, want the explicitly-set value preserved", cfg.ArtifactRepoURL)
	}
}

// TestLoadConfig_CloudRealConstruction_StillRequiresGitHubCreds — the cloud arm's
// creds requirement is UNCHANGED by Task 6 (only the local profile is relaxed).
func TestLoadConfig_CloudRealConstruction_StillRequiresGitHubCreds(t *testing.T) {
	setEnv(t, map[string]string{
		"ARCHISTRATOR_POSTGRES_URL":               "postgres://x",
		"ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL":    "false",
		"ARCHISTRATOR_CONSTRUCTION_DRYRUN":        "false",
		"ARCHISTRATOR_ARTIFACT_REPO_URL":          "https://example/artifacts.git",
		"ARCHISTRATOR_GITHUB_APP_ID":              "",
		"ARCHISTRATOR_GITHUB_APP_PRIVATE_KEY_PEM": "",
		"ARCHISTRATOR_CONSTRUCTION_REPO_OWNER":    "",
		"ARCHISTRATOR_CONSTRUCTION_REPO_NAME":     "",
	})
	_, err := loadResolvedConfig()
	if err == nil {
		t.Fatal("expected the cloud profile to still fail fast without GitHub App creds when DRYRUN=false")
	}
	if !strings.Contains(err.Error(), "ARCHISTRATOR_GITHUB_APP_ID") {
		t.Fatalf("expected error to name ARCHISTRATOR_GITHUB_APP_ID, got: %v", err)
	}
}
