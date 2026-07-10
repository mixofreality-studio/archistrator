// Command server is the archistrator server composition root (designs/aiarch
// codename; BUILD-LOCATION.md). The ordered boot walk — signal context →
// telemetry → Temporal client → Postgres pool → per-binding ResourceAccess →
// Engines → security Utility → Managers + embedded Temporal Workers → the
// generated web/MCP transports + HTTP server + graceful shutdown — is GENERATED
// into main.gen.go (framework-go-app-generator/composegen, from project.json's
// deployment model + service contracts). This file only loads + resolves config,
// builds the hand POLICY seam (hooks.go), and calls RunGenerated.
//
// This file is OUTSIDE internal/, so it is not scanned by the Method arch checker
// (TestMethodLayering) and may freely import Temporal + both concrete RA packages
// — it is the composition root. The Client (internal/client/web) imports no
// Temporal; the Managers own it.
package main

import (
	"log/slog"
	"os"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := loadResolvedConfig()
	if err != nil {
		logger.Error("server exited with error", "err", err)
		os.Exit(1)
	}
	if cfg.AuthDevMode {
		logger.Warn("AUTH DEV MODE ENABLED — a dev principal is injected on every request and the access token is NOT validated. MUST be off in any IdP-fronted deployment.")
	}

	hooks, err := newAppHooks(cfg, logger)
	if err != nil {
		logger.Error("server exited with error", "err", err)
		os.Exit(1)
	}

	if err := RunGenerated(cfg, hooks, logger); err != nil {
		logger.Error("server exited with error", "err", err)
		os.Exit(1)
	}
}
