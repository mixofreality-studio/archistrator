package main

import (
	"strings"
	"testing"
	"time"
)

// allConfigEnv is every env var loadConfig (via the generated loader + the hand
// adapter) reads. The parity test clears them all before each case so ambient
// process env cannot leak in — the whole point of the IDENTICAL-ENV gate.
var allConfigEnv = []string{
	"ARCHISTRATOR_GITHUB_APP_ID",
	"ARCHISTRATOR_GITHUB_APP_PRIVATE_KEY_PEM",
	"ARCHISTRATOR_GITHUB_APP_PRIVATE_KEY_PEM_FILE",
	"ARCHISTRATOR_GITHUB_ACCOUNT",
	"ARCHISTRATOR_GITHUB_APP_SLUG",
	"ARCHISTRATOR_GITHUB_INSTALLATION_ID",
	"ARCHISTRATOR_GITHUB_API_BASE_URL",
	"ARCHISTRATOR_KEYCLOAK_JWKS_URL",
	"ARCHISTRATOR_KEYCLOAK_ISSUER",
	"ARCHISTRATOR_POSTGRES_URL",
	"ARCHISTRATOR_LISTEN_ADDR",
	"ARCHISTRATOR_SHUTDOWN_TIMEOUT",
	"ARCHISTRATOR_TEMPORAL_HOSTPORT",
	"ARCHISTRATOR_TEMPORAL_NAMESPACE",
	"ARCHISTRATOR_CONSTRUCTION_ESCALATION_TIMEOUT",
	"ARCHISTRATOR_CONSTRUCTION_INTERVENTION_MODE",
	"ARCHISTRATOR_CONSTRUCTION_REPO_OWNER",
	"ARCHISTRATOR_CONSTRUCTION_REPO_NAME",
	"ARCHISTRATOR_CONSTRUCTION_WORKFLOW_FILE",
	"ARCHISTRATOR_CONSTRUCTION_REF",
	"ARCHISTRATOR_CONSTRUCTION_TASK_QUEUE",
	"ARCHISTRATOR_CONSTRUCTION_DRYRUN",
	"ARCHISTRATOR_OPERATIONS_DRYRUN",
	"ARCHISTRATOR_OPERATED_RUNTIME_GITOPS_REPO_URL",
	"ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL",
	"ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL",
	"ARCHISTRATOR_ARTIFACT_REPO_URL",
	"ARCHISTRATOR_ARTIFACT_REPO_OWNER",
	"ARCHISTRATOR_ARTIFACT_REPO_LOCAL",
	"ARCHISTRATOR_AUTH_DEV_MODE",
	"ARCHISTRATOR_DEV_SUBJECT",
	"ARCHISTRATOR_DEV_ROLES",
}

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, k := range allConfigEnv {
		t.Setenv(k, "")
	}
}

const testPEM = "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAA==\n-----END RSA PRIVATE KEY-----"

// TestLoadConfigParity pins the historical loadConfig contract: for identical env
// input the required-var errors and effective config values must match the
// pre-configgen hand loader. Each case clears ALL config env first, then applies
// its overrides.
func TestLoadConfigParity(t *testing.T) {
	// devLocalRig is the systemtests / local-dogfood boot env: dry-run construction,
	// on-disk git project state, dev auth injected.
	t.Run("dev-local-rig", func(t *testing.T) {
		clearConfigEnv(t)
		setEnv(t, map[string]string{
			"ARCHISTRATOR_POSTGRES_URL":               "postgres://archistrator@localhost:5432/db",
			"ARCHISTRATOR_TEMPORAL_HOSTPORT":          "localhost:7233",
			"ARCHISTRATOR_TEMPORAL_NAMESPACE":         "st-1",
			"ARCHISTRATOR_AUTH_DEV_MODE":              "true",
			"ARCHISTRATOR_CONSTRUCTION_DRYRUN":        "true",
			"ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL":    "true",
			"ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL": "file:///tmp/proj.git",
		})
		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		wantStr := map[string]string{
			"PostgresURL":            cfg.PostgresURL,
			"TemporalHostPort":       cfg.TemporalHostPort,
			"TemporalNamespace":      cfg.TemporalNamespace,
			"ProjectStateGitRepoURL": cfg.ProjectStateGitRepoURL,
			"ListenAddr":             cfg.ListenAddr,
		}
		if wantStr["PostgresURL"] != "postgres://archistrator@localhost:5432/db" {
			t.Errorf("PostgresURL = %q", cfg.PostgresURL)
		}
		if cfg.TemporalHostPort != "localhost:7233" || cfg.TemporalNamespace != "st-1" {
			t.Errorf("temporal = %q / %q", cfg.TemporalHostPort, cfg.TemporalNamespace)
		}
		if cfg.ListenAddr != ":8080" { // default preserved
			t.Errorf("ListenAddr default = %q, want :8080", cfg.ListenAddr)
		}
		if cfg.ShutdownTimeout != 20*time.Second {
			t.Errorf("ShutdownTimeout default = %v, want 20s", cfg.ShutdownTimeout)
		}
		if !cfg.ConstructionDryRun {
			t.Error("ConstructionDryRun = false, want true")
		}
		if !cfg.ProjectStateGitLocal || cfg.ProjectStateGitRepoURL != "file:///tmp/proj.git" {
			t.Errorf("project-state-git local=%v url=%q", cfg.ProjectStateGitLocal, cfg.ProjectStateGitRepoURL)
		}
		if !cfg.Dev.Enabled {
			t.Error("Dev.Enabled = false, want true")
		}
		if cfg.Dev.Principal.Subject != "dev-architect" {
			t.Errorf("dev subject = %q, want dev-architect (default)", cfg.Dev.Principal.Subject)
		}
		if got := cfg.Dev.Principal.Roles; len(got) != 2 || got[0] != "drive-phase" || got[1] != "approve-artifact" {
			t.Errorf("dev roles = %v, want [drive-phase approve-artifact]", got)
		}
		if cfg.ConstructionWorkflowFile != "aiarch-construct.yml" {
			t.Errorf("ConstructionWorkflowFile default = %q", cfg.ConstructionWorkflowFile)
		}
		if cfg.ConstructionEscalationTimeout != 30*time.Minute {
			t.Errorf("ConstructionEscalationTimeout default = %v, want 30m", cfg.ConstructionEscalationTimeout)
		}
		if cfg.ConstructionInterventionMode != "tiered" {
			t.Errorf("ConstructionInterventionMode default = %q, want tiered", cfg.ConstructionInterventionMode)
		}
		_ = wantStr
	})

	// cloudish exercises the real construction path: all creds present, PEM inline,
	// installation-id int64 coercion, GitHubAccount chained default off
	// CONSTRUCTION_REPO_OWNER, keycloak configured.
	t.Run("cloudish", func(t *testing.T) {
		clearConfigEnv(t)
		setEnv(t, map[string]string{
			"ARCHISTRATOR_POSTGRES_URL":               "postgres://cloud",
			"ARCHISTRATOR_CONSTRUCTION_DRYRUN":        "false",
			"ARCHISTRATOR_CONSTRUCTION_REPO_OWNER":    "acme",
			"ARCHISTRATOR_CONSTRUCTION_REPO_NAME":     "widget",
			"ARCHISTRATOR_GITHUB_APP_ID":              "999",
			"ARCHISTRATOR_GITHUB_APP_PRIVATE_KEY_PEM": testPEM,
			"ARCHISTRATOR_GITHUB_INSTALLATION_ID":     "42",
			"ARCHISTRATOR_ARTIFACT_REPO_URL":          "https://github.com/acme/widget.git",
			"ARCHISTRATOR_KEYCLOAK_JWKS_URL":          "https://kc/realms/x/certs",
			"ARCHISTRATOR_KEYCLOAK_ISSUER":            "https://kc/realms/x",
		})
		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.ConstructionDryRun {
			t.Error("ConstructionDryRun = true, want false")
		}
		if cfg.GitHubInstallationID != 42 { // int64 coercion
			t.Errorf("GitHubInstallationID = %d, want 42", cfg.GitHubInstallationID)
		}
		if cfg.GitHubAccount != "acme" { // chained default off CONSTRUCTION_REPO_OWNER
			t.Errorf("GitHubAccount = %q, want acme (chained default)", cfg.GitHubAccount)
		}
		if cfg.GitHubAppPrivateKeyPEM != testPEM {
			t.Errorf("GitHubAppPrivateKeyPEM = %q, want inline PEM verbatim", cfg.GitHubAppPrivateKeyPEM)
		}
		if cfg.KeycloakJWKSURL == "" || cfg.KeycloakIssuer == "" {
			t.Errorf("keycloak = %q / %q", cfg.KeycloakJWKSURL, cfg.KeycloakIssuer)
		}
	})

	// account-explicit: an explicit ARCHISTRATOR_GITHUB_ACCOUNT wins over the
	// chained CONSTRUCTION_REPO_OWNER default; unparseable installation id ⇒ 0.
	t.Run("account-explicit-and-bad-installation-id", func(t *testing.T) {
		clearConfigEnv(t)
		setEnv(t, map[string]string{
			"ARCHISTRATOR_POSTGRES_URL":            "postgres://x",
			"ARCHISTRATOR_CONSTRUCTION_DRYRUN":     "true",
			"ARCHISTRATOR_GITHUB_ACCOUNT":          "explicit-org",
			"ARCHISTRATOR_CONSTRUCTION_REPO_OWNER": "fallback-owner",
			"ARCHISTRATOR_GITHUB_INSTALLATION_ID":  "not-a-number",
		})
		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.GitHubAccount != "explicit-org" {
			t.Errorf("GitHubAccount = %q, want explicit-org", cfg.GitHubAccount)
		}
		if cfg.GitHubInstallationID != 0 {
			t.Errorf("GitHubInstallationID = %d, want 0 (unparseable)", cfg.GitHubInstallationID)
		}
	})

	// empty env ⇒ the unconditional PostgresURL requirement fires first.
	t.Run("empty-env-requires-postgres", func(t *testing.T) {
		clearConfigEnv(t)
		_, err := loadConfig()
		if err == nil {
			t.Fatal("expected error on empty env")
		}
		if got := err.Error(); got != "ARCHISTRATOR_POSTGRES_URL is required" {
			t.Fatalf("error = %q, want exact ARCHISTRATOR_POSTGRES_URL is required", got)
		}
	})

	// postgres set, DRYRUN=false, creds missing ⇒ the construction-creds error set,
	// naming every missing var.
	t.Run("dryrun-false-missing-creds", func(t *testing.T) {
		clearConfigEnv(t)
		setEnv(t, map[string]string{
			"ARCHISTRATOR_POSTGRES_URL":        "postgres://x",
			"ARCHISTRATOR_CONSTRUCTION_DRYRUN": "false",
		})
		_, err := loadConfig()
		if err == nil {
			t.Fatal("expected construction-creds error")
		}
		for _, want := range []string{
			"ARCHISTRATOR_GITHUB_APP_ID",
			"ARCHISTRATOR_GITHUB_APP_PRIVATE_KEY_PEM",
			"ARCHISTRATOR_CONSTRUCTION_REPO_OWNER",
			"ARCHISTRATOR_CONSTRUCTION_REPO_NAME",
			"ARCHISTRATOR_ARTIFACT_REPO_URL",
		} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q missing %q", err.Error(), want)
			}
		}
	})
}
