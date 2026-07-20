package main

import "testing"

// lookupEnv returns the value for key in an env slice ("KEY=value" entries)
// produced by serverChildConfig.env(), and whether key was present at all —
// distinguishing "not set" from "set to empty string".
func lookupEnv(env []string, key string) (value string, present bool) {
	for _, kv := range env {
		k, v, ok := splitEnv(kv)
		if ok && k == key {
			return v, true
		}
	}
	return "", false
}

// TestServerChildConfigEnv_DoesNotForceConstructionDryRun is I2: the child
// env composer must NOT force ARCHISTRATOR_CONSTRUCTION_DRYRUN=true anymore —
// cmd/server's own config.gen.go default (getenvBool(..., "false")) then
// takes over when the parent leaves it unset, so an unconfigured `archistrator
// mcp` boot now defaults to the REAL local construction executor (Task 6,
// sandbox-gated fail-closed), not a forced dry run.
func TestServerChildConfigEnv_DoesNotForceConstructionDryRun(t *testing.T) {
	cfg := serverChildConfig{
		Bin:              "archistrator-server",
		RepoDir:          "/tmp/repo",
		ListenAddr:       "127.0.0.1:8877",
		TemporalHostport: "127.0.0.1:7233",
	}

	env := cfg.env()

	if v, present := lookupEnv(env, "ARCHISTRATOR_CONSTRUCTION_DRYRUN"); present {
		t.Fatalf("ARCHISTRATOR_CONSTRUCTION_DRYRUN forced to %q; want ABSENT so cmd/server's own default (false) governs", v)
	}
}

// TestServerChildConfigEnv_RespectsExplicitParentDryRunOverride is the other
// half of I2: an operator who explicitly sets ARCHISTRATOR_CONSTRUCTION_DRYRUN
// in the parent (archistrator mcp) process's own environment before Claude
// Code auto-spawns it must have that value passed through to the child
// unchanged — neither forced to true (the old behavior) nor silently dropped.
func TestServerChildConfigEnv_RespectsExplicitParentDryRunOverride(t *testing.T) {
	t.Setenv("ARCHISTRATOR_CONSTRUCTION_DRYRUN", "true")

	cfg := serverChildConfig{
		Bin:              "archistrator-server",
		RepoDir:          "/tmp/repo",
		ListenAddr:       "127.0.0.1:8877",
		TemporalHostport: "127.0.0.1:7233",
	}

	env := cfg.env()

	v, present := lookupEnv(env, "ARCHISTRATOR_CONSTRUCTION_DRYRUN")
	if !present {
		t.Fatal("ARCHISTRATOR_CONSTRUCTION_DRYRUN missing from child env; want explicit parent override passed through")
	}
	if v != "true" {
		t.Fatalf("ARCHISTRATOR_CONSTRUCTION_DRYRUN = %q, want %q (parent's explicit override)", v, "true")
	}
}

// TestServerChildConfigEnv_StillForcesLocalProfileSettings guards against
// over-correcting I2: the OTHER local-profile settings this package is
// responsible for (git-local substrate, listen addr, temporal hostport,
// loopback-only dev auth) must still be forced regardless of any conflicting
// parent env — only ARCHISTRATOR_CONSTRUCTION_DRYRUN's forcing was removed.
func TestServerChildConfigEnv_StillForcesLocalProfileSettings(t *testing.T) {
	t.Setenv("ARCHISTRATOR_LISTEN_ADDR", "should-be-overridden:0")
	t.Setenv("ARCHISTRATOR_AUTH_DEV_MODE", "false")

	cfg := serverChildConfig{
		Bin:              "archistrator-server",
		RepoDir:          "/tmp/repo",
		ListenAddr:       "127.0.0.1:8877",
		TemporalHostport: "127.0.0.1:7233",
	}

	env := cfg.env()

	if v, _ := lookupEnv(env, "ARCHISTRATOR_LISTEN_ADDR"); v != "127.0.0.1:8877" {
		t.Fatalf("ARCHISTRATOR_LISTEN_ADDR = %q, want forced %q", v, "127.0.0.1:8877")
	}
	if v, _ := lookupEnv(env, "ARCHISTRATOR_AUTH_DEV_MODE"); v != "true" {
		t.Fatalf("ARCHISTRATOR_AUTH_DEV_MODE = %q, want forced %q", v, "true")
	}
	if v, present := lookupEnv(env, "ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL"); !present || v != "true" {
		t.Fatalf("ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL = %q (present=%v), want forced %q", v, present, "true")
	}
}

// QA 2026-07-19 (poll-404 wizard reset): the child must run in the stack's
// DEDICATED Temporal namespace, not "default" — the namespace is the identity
// seam that turns a wrong/foreign Temporal backend into a typed
// NamespaceNotFound (an Infrastructure fault the SPA tolerates) instead of a
// destructive, authoritative "no active design session" 404. Forced over any
// conflicting parent env, like the other local-profile settings.
func TestServerChildConfigEnv_ForcesDedicatedTemporalNamespace(t *testing.T) {
	t.Setenv("ARCHISTRATOR_TEMPORAL_NAMESPACE", "default")

	cfg := serverChildConfig{
		Bin:               "archistrator-server",
		RepoDir:           "/tmp/repo",
		ListenAddr:        "127.0.0.1:8877",
		TemporalHostport:  "127.0.0.1:7943",
		TemporalNamespace: localTemporalNamespace,
	}

	env := cfg.env()

	v, present := lookupEnv(env, "ARCHISTRATOR_TEMPORAL_NAMESPACE")
	if !present || v != localTemporalNamespace {
		t.Fatalf("ARCHISTRATOR_TEMPORAL_NAMESPACE = %q (present=%v), want forced %q", v, present, localTemporalNamespace)
	}
}
