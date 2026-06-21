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

// TestLoadConfig_Replay_RequiresDir — provider=replay requires a cassette dir.
func TestLoadConfig_Replay_RequiresDir(t *testing.T) {
	setEnv(t, map[string]string{
		"ARCHISTRATOR_POSTGRES_URL":      "postgres://x",
		"ARCHISTRATOR_WORKER_PROVIDER":   "replay",
		"ARCHISTRATOR_WORKER_REPLAY_DIR": "",
	})
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected error: replay requires ARCHISTRATOR_WORKER_REPLAY_DIR")
	}
}

// TestLoadConfig_Replay_Strict_OK — strict replay needs only the dir (no delegate).
func TestLoadConfig_Replay_Strict_OK(t *testing.T) {
	setEnv(t, map[string]string{
		"ARCHISTRATOR_POSTGRES_URL":        "postgres://x",
		"ARCHISTRATOR_WORKER_PROVIDER":     "replay",
		"ARCHISTRATOR_WORKER_REPLAY_DIR":   "/tmp/cassettes",
		"ARCHISTRATOR_WORKER_REPLAY_MODE":  "strict",
		"ARCHISTRATOR_CONSTRUCTION_DRYRUN": "true", // not testing construction; skip cred validation
	})
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("strict replay config: %v", err)
	}
	if cfg.ReplayMode != "strict" || cfg.ReplayDir != "/tmp/cassettes" {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
}

// TestLoadConfig_Replay_RecordOnMiss_Ollama_RequiresBaseURL — record_on_miss with
// the ollama delegate requires the Ollama base URL.
func TestLoadConfig_Replay_RecordOnMiss_Ollama_RequiresBaseURL(t *testing.T) {
	setEnv(t, map[string]string{
		"ARCHISTRATOR_POSTGRES_URL":           "postgres://x",
		"ARCHISTRATOR_WORKER_PROVIDER":        "replay",
		"ARCHISTRATOR_WORKER_REPLAY_DIR":      "/tmp/cassettes",
		"ARCHISTRATOR_WORKER_REPLAY_MODE":     "record_on_miss",
		"ARCHISTRATOR_WORKER_REPLAY_DELEGATE": "ollama",
		"ARCHISTRATOR_OLLAMA_BASEURL":         "",
	})
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected error: ollama delegate requires ARCHISTRATOR_OLLAMA_BASEURL")
	}
}

// TestLoadConfig_Replay_InvalidMode — an unrecognized ReplayMode is rejected with
// an actionable error (covers the mode-enum default arm).
func TestLoadConfig_Replay_InvalidMode(t *testing.T) {
	setEnv(t, map[string]string{
		"ARCHISTRATOR_POSTGRES_URL":       "postgres://x",
		"ARCHISTRATOR_WORKER_PROVIDER":    "replay",
		"ARCHISTRATOR_WORKER_REPLAY_DIR":  "/tmp/cassettes",
		"ARCHISTRATOR_WORKER_REPLAY_MODE": "replayy",
	})
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected error for an invalid ARCHISTRATOR_WORKER_REPLAY_MODE")
	}
}

// TestLoadConfig_RealConstruction_FailFast — DRYRUN=false requires all construction
// creds; missing any one must return an error at startup.
func TestLoadConfig_RealConstruction_FailFast(t *testing.T) {
	setEnv(t, map[string]string{
		"ARCHISTRATOR_POSTGRES_URL":               "postgres://x",
		"ARCHISTRATOR_ANTHROPIC_API_KEY":          "sk-test",
		"ARCHISTRATOR_CONSTRUCTION_DRYRUN":        "false",
		"ARCHISTRATOR_CONSTRUCTION_REPO_OWNER":    "mixofreality-studio",
		"ARCHISTRATOR_CONSTRUCTION_REPO_NAME":     "archistrator",
		"ARCHISTRATOR_CONSTRUCTION_WORKFLOW_FILE": "aiarch-construct.yml",
		"ARCHISTRATOR_CONSTRUCTION_REF":           "main",
		// App creds intentionally absent
	})
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected error when DRYRUN=false and app creds missing")
	}
}

// TestLoadConfig_RealConstruction_OK — all creds present → no error.
func TestLoadConfig_RealConstruction_OK(t *testing.T) {
	setEnv(t, map[string]string{
		"ARCHISTRATOR_POSTGRES_URL":               "postgres://x",
		"ARCHISTRATOR_ANTHROPIC_API_KEY":          "sk-test",
		"ARCHISTRATOR_CONSTRUCTION_DRYRUN":        "false",
		"ARCHISTRATOR_CONSTRUCTION_REPO_OWNER":    "mixofreality-studio",
		"ARCHISTRATOR_CONSTRUCTION_REPO_NAME":     "archistrator",
		"ARCHISTRATOR_CONSTRUCTION_WORKFLOW_FILE": "aiarch-construct.yml",
		"ARCHISTRATOR_CONSTRUCTION_REF":           "main",
		"ARCHISTRATOR_GITHUB_APP_ID":              "12345",
		"ARCHISTRATOR_GITHUB_APP_PRIVATE_KEY_PEM": "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAA==\n-----END RSA PRIVATE KEY-----",
		"ARCHISTRATOR_ARTIFACT_REPO_URL":          "https://github.com/mixofreality-studio/archistrator.git",
	})
	if _, err := loadConfig(); err != nil {
		t.Fatalf("expected no error with all real-construction creds: %v", err)
	}
}

// TestLoadConfig_RealConstruction_RequiresArtifactRepoURL — the real-path selection
// needs the git-forward artifact store (main.go: pipeline != nil && artifacts != nil),
// which is constructed only when ArtifactRepoURL is set. DRYRUN=false without it must
// fail fast and name the missing var, not silently skip construction registration.
func TestLoadConfig_RealConstruction_RequiresArtifactRepoURL(t *testing.T) {
	setEnv(t, map[string]string{
		"ARCHISTRATOR_POSTGRES_URL":               "postgres://x",
		"ARCHISTRATOR_ANTHROPIC_API_KEY":          "sk-test",
		"ARCHISTRATOR_CONSTRUCTION_DRYRUN":        "false",
		"ARCHISTRATOR_CONSTRUCTION_REPO_OWNER":    "mixofreality-studio",
		"ARCHISTRATOR_CONSTRUCTION_REPO_NAME":     "archistrator",
		"ARCHISTRATOR_CONSTRUCTION_WORKFLOW_FILE": "aiarch-construct.yml",
		"ARCHISTRATOR_CONSTRUCTION_REF":           "main",
		"ARCHISTRATOR_GITHUB_APP_ID":              "12345",
		"ARCHISTRATOR_GITHUB_APP_PRIVATE_KEY_PEM": "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAA==\n-----END RSA PRIVATE KEY-----",
		// ARCHISTRATOR_ARTIFACT_REPO_URL intentionally absent
	})
	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected error when DRYRUN=false and ARCHISTRATOR_ARTIFACT_REPO_URL missing")
	}
	if !strings.Contains(err.Error(), "ARCHISTRATOR_ARTIFACT_REPO_URL") {
		t.Fatalf("expected error to name ARCHISTRATOR_ARTIFACT_REPO_URL, got: %v", err)
	}
}
