package main

import (
	"strings"
	"testing"
)

// QA 2026-07-19 (poll-404 wizard reset): the local stack used to target the
// WELL-KNOWN Temporal frontend port 7233 and silently ADOPT whatever was
// listening there. On the founder's machine the systemtests suite runs its own
// `temporal server start-dev` on that same port (persistent aiarch-test.db), so
// whenever the stack's own dev server died and the systemtests one held the
// port, the server transparently reconnected to a FOREIGN Temporal — every
// session lookup then answered "workflow not found", the API served an
// authoritative 404 "no active design session", and the SPA reset the wizard
// mid-use-case. The local stack now defaults to its OWN dedicated loopback
// port so the two tools never collide.
func TestDefaultTemporalHostport_DedicatedLocalPort(t *testing.T) {
	t.Setenv("ARCHISTRATOR_TEMPORAL_HOSTPORT", "")
	got := defaultTemporalHostport()
	if got == "127.0.0.1:7233" {
		t.Fatal("the local stack must NOT default to the shared well-known 7233 (systemtests' dev server lives there); want a dedicated port")
	}
	if !strings.HasPrefix(got, "127.0.0.1:") {
		t.Fatalf("default hostport must stay loopback, got %q", got)
	}
}

func TestDefaultTemporalHostport_ExplicitOverrideWins(t *testing.T) {
	t.Setenv("ARCHISTRATOR_TEMPORAL_HOSTPORT", "10.0.0.5:7233")
	if got := defaultTemporalHostport(); got != "10.0.0.5:7233" {
		t.Fatalf("explicit ARCHISTRATOR_TEMPORAL_HOSTPORT must win, got %q", got)
	}
}

// The spawned dev server must carry the stack's IDENTITY (its dedicated
// namespace, pre-created via --namespace) and PERSISTENCE (--db-filename, so a
// restart does not vaporize every in-flight design session the SPA is
// polling). The old spawn had neither: in-memory storage on the default
// namespace — indistinguishable from any other tool's dev server.
func TestTemporalStartDevArgs_CarriesNamespaceAndPersistentDB(t *testing.T) {
	args := temporalStartDevArgs("127.0.0.1", "7943", "/home/x/.archistrator/temporal.db")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"server start-dev",
		"--headless",
		"--ip 127.0.0.1",
		"--port 7943",
		"--namespace " + localTemporalNamespace,
		"--db-filename /home/x/.archistrator/temporal.db",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("start-dev args missing %q; got %q", want, joined)
		}
	}
}

// localTemporalNamespace is the stack's identity seam: adopting an
// already-running Temporal is only safe when it carries this namespace. It
// must never be "default" — every stray dev server has "default".
func TestLocalTemporalNamespace_NotDefault(t *testing.T) {
	if localTemporalNamespace == "default" || localTemporalNamespace == "" {
		t.Fatalf("localTemporalNamespace must be a dedicated identity namespace, got %q", localTemporalNamespace)
	}
}
