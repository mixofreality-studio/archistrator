package harness

import "os"

// Infra is the set of externally-provisioned backing services the server needs.
type Infra struct {
	PostgresURL       string
	TemporalHostPort  string
	TemporalNamespace string
	OllamaBaseURL     string
	OllamaModel       string
	// Drafting is the four-value knob (off|replay|WHEN_REQUIRED|live). Default
	// "replay": fast, offline cassette replay needing no Ollama.
	Drafting string
}

// needsOllama reports whether the drafting mode requires a live Ollama delegate.
func (in Infra) needsOllama() bool {
	return in.Drafting == "WHEN_REQUIRED" || in.Drafting == "live"
}

// InfraFromEnv reads the backing-service endpoints. ok is false when a REQUIRED
// endpoint is absent. Postgres + Temporal are always required; Ollama is required
// only for the WHEN_REQUIRED / live drafting modes.
func InfraFromEnv() (Infra, bool) {
	in := Infra{
		PostgresURL:       os.Getenv("ARCHISTRATOR_POSTGRES_URL"),
		TemporalHostPort:  os.Getenv("ARCHISTRATOR_TEMPORAL_HOSTPORT"),
		TemporalNamespace: getenvDefault("ARCHISTRATOR_TEMPORAL_NAMESPACE", "aiarch-test"),
		OllamaBaseURL:     os.Getenv("ARCHISTRATOR_OLLAMA_BASEURL"),
		OllamaModel:       getenvDefault("ARCHISTRATOR_OLLAMA_MODEL", "qwen2.5:3b"),
		Drafting:          getenvDefault("ARCHISTRATOR_SYSTEMTESTS_DRAFTING", "replay"),
	}
	if in.PostgresURL == "" || in.TemporalHostPort == "" {
		return Infra{}, false
	}
	if in.needsOllama() && in.OllamaBaseURL == "" {
		return Infra{}, false
	}
	return in, true
}

func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
