package main

import (
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
		"ARCHISTRATOR_POSTGRES_URL":       "postgres://x",
		"ARCHISTRATOR_WORKER_PROVIDER":    "replay",
		"ARCHISTRATOR_WORKER_REPLAY_DIR":  "/tmp/cassettes",
		"ARCHISTRATOR_WORKER_REPLAY_MODE": "strict",
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
