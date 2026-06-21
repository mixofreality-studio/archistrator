package harness

// workerEnv maps the systemtests drafting mode to the ARCHISTRATOR_WORKER_* env
// the server's config.go reads. The modes mirror the design's four-value knob:
//
//	replay        — cassette replay, strict (miss = loud error). Offline; no Ollama.
//	WHEN_REQUIRED — cassette replay, record-on-miss via the Ollama delegate.
//	live          — direct Ollama, no cassettes (the eval tier).
//
// "off" is the uitests-layer skip signal; it has no distinct systemtests mapping
// and falls through to the offline strict-replay default below.
func workerEnv(mode string, in Infra, cassetteDir string) []string {
	switch mode {
	case "live":
		return []string{
			"ARCHISTRATOR_WORKER_PROVIDER=ollama",
			"ARCHISTRATOR_OLLAMA_BASEURL=" + in.OllamaBaseURL,
			"ARCHISTRATOR_OLLAMA_MODEL=" + in.OllamaModel,
		}
	case "WHEN_REQUIRED":
		return []string{
			"ARCHISTRATOR_WORKER_PROVIDER=replay",
			"ARCHISTRATOR_WORKER_REPLAY_DIR=" + cassetteDir,
			"ARCHISTRATOR_WORKER_REPLAY_MODE=record_on_miss",
			"ARCHISTRATOR_WORKER_REPLAY_DELEGATE=ollama",
			"ARCHISTRATOR_OLLAMA_BASEURL=" + in.OllamaBaseURL,
			"ARCHISTRATOR_OLLAMA_MODEL=" + in.OllamaModel,
		}
	default: // "replay" (and any unrecognized value) → strict offline replay.
		return []string{
			"ARCHISTRATOR_WORKER_PROVIDER=replay",
			"ARCHISTRATOR_WORKER_REPLAY_DIR=" + cassetteDir,
			"ARCHISTRATOR_WORKER_REPLAY_MODE=strict",
		}
	}
}
