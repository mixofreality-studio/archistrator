package harness

import (
	"strings"
	"testing"
)

// envContains asserts a "KEY=VALUE" pair is present in the slice.
func envContains(t *testing.T, env []string, want string) {
	t.Helper()
	for _, e := range env {
		if e == want {
			return
		}
	}
	t.Fatalf("expected env to contain %q, got:\n%s", want, strings.Join(env, "\n"))
}

// envHasKey reports whether any "KEY=..." entry has the given key.
func envHasKey(env []string, key string) bool {
	for _, e := range env {
		if strings.HasPrefix(e, key+"=") {
			return true
		}
	}
	return false
}

func TestWorkerEnv_Replay_Strict(t *testing.T) {
	in := Infra{OllamaBaseURL: "http://localhost:11434", OllamaModel: "qwen2.5:3b"}
	env := workerEnv("replay", in, "/repo/testdata/cassettes")
	envContains(t, env, "ARCHISTRATOR_WORKER_PROVIDER=replay")
	envContains(t, env, "ARCHISTRATOR_WORKER_REPLAY_MODE=strict")
	envContains(t, env, "ARCHISTRATOR_WORKER_REPLAY_DIR=/repo/testdata/cassettes")
	// Strict replay must be Ollama-free — this is the invariant InfraFromEnv's
	// relaxed Ollama requirement depends on.
	for _, key := range []string{
		"ARCHISTRATOR_OLLAMA_BASEURL",
		"ARCHISTRATOR_OLLAMA_MODEL",
		"ARCHISTRATOR_WORKER_REPLAY_DELEGATE",
	} {
		if envHasKey(env, key) {
			t.Errorf("strict replay must be Ollama-free, but emitted %s", key)
		}
	}
}

// TestWorkerEnv_UnknownMode_SafeDefault — an unrecognized mode degrades to the
// same strict, Ollama-free offline default as "replay".
func TestWorkerEnv_UnknownMode_SafeDefault(t *testing.T) {
	in := Infra{OllamaBaseURL: "http://localhost:11434", OllamaModel: "qwen2.5:3b"}
	env := workerEnv("bogus", in, "/repo/testdata/cassettes")
	envContains(t, env, "ARCHISTRATOR_WORKER_PROVIDER=replay")
	envContains(t, env, "ARCHISTRATOR_WORKER_REPLAY_MODE=strict")
	if envHasKey(env, "ARCHISTRATOR_OLLAMA_BASEURL") {
		t.Error("unknown mode must degrade to Ollama-free strict replay")
	}
}

func TestWorkerEnv_WhenRequired_RecordsViaOllama(t *testing.T) {
	in := Infra{OllamaBaseURL: "http://localhost:11434", OllamaModel: "qwen2.5:3b"}
	env := workerEnv("WHEN_REQUIRED", in, "/repo/testdata/cassettes")
	envContains(t, env, "ARCHISTRATOR_WORKER_PROVIDER=replay")
	envContains(t, env, "ARCHISTRATOR_WORKER_REPLAY_MODE=record_on_miss")
	envContains(t, env, "ARCHISTRATOR_WORKER_REPLAY_DELEGATE=ollama")
	envContains(t, env, "ARCHISTRATOR_OLLAMA_BASEURL=http://localhost:11434")
}

func TestWorkerEnv_Live_DirectOllama(t *testing.T) {
	in := Infra{OllamaBaseURL: "http://localhost:11434", OllamaModel: "qwen2.5:3b"}
	env := workerEnv("live", in, "/repo/testdata/cassettes")
	envContains(t, env, "ARCHISTRATOR_WORKER_PROVIDER=ollama")
	envContains(t, env, "ARCHISTRATOR_OLLAMA_BASEURL=http://localhost:11434")
}
