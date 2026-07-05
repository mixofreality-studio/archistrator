package main

import (
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

func TestParseArtifactKind(t *testing.T) {
	cases := map[string]projectstate.ArtifactKind{
		"Volatilities": projectstate.KindVolatilities, // String() form (what the dispatch stamps)
		"volatilities": projectstate.KindVolatilities, // wire name
		"System":       projectstate.KindSystem,
		"coreUseCases": projectstate.KindCoreUseCases,
	}
	for in, want := range cases {
		got, err := parseArtifactKind(in)
		if err != nil {
			t.Fatalf("parseArtifactKind(%q): %v", in, err)
		}
		if got != want {
			t.Fatalf("parseArtifactKind(%q) = %v, want %v", in, got, want)
		}
	}
	if _, err := parseArtifactKind("NotAKind"); err == nil {
		t.Fatalf("expected error for an unknown kind")
	}
}

// slotFor must cover every ArtifactKind (a new kind must not silently fall through).
func TestSlotForCoversAllKinds(t *testing.T) {
	p := &projectstate.Project{}
	for _, k := range projectstate.AllArtifactKinds() {
		if _, ok := slotFor(p, k); !ok {
			t.Fatalf("slotFor has no case for kind %s", k)
		}
	}
}

func TestNewSessionFromEnv(t *testing.T) {
	env := map[string]string{
		envArtifactKind: "Volatilities",
		envJobMode:      "critique",
		envTargetBranch: "aiarch-design/proj/3",
		envProjectID:    "proj",
	}
	getenv := func(k string) string { return env[k] }
	s, err := newSessionFromEnv(getenv, "/tmp/checkout")
	if err != nil {
		t.Fatalf("newSessionFromEnv: %v", err)
	}
	if s.Kind != projectstate.KindVolatilities {
		t.Fatalf("kind = %v", s.Kind)
	}
	if s.Mode != jobModeCritique {
		t.Fatalf("mode = %q", s.Mode)
	}
	if s.StateRoot != "/tmp/checkout" {
		t.Fatalf("state root = %q", s.StateRoot)
	}
	if s.ProjectID != projectstate.ProjectID("proj") {
		t.Fatalf("project id = %q", s.ProjectID)
	}
}

func TestNewSessionFromEnv_MissingKind(t *testing.T) {
	getenv := func(k string) string { return "" }
	if _, err := newSessionFromEnv(getenv, "/tmp/x"); err == nil {
		t.Fatalf("expected error when artifact kind is missing")
	}
}

func TestNewSessionFromEnv_BadMode(t *testing.T) {
	env := map[string]string{envArtifactKind: "Mission", envJobMode: "bogus"}
	if _, err := newSessionFromEnv(func(k string) string { return env[k] }, "/tmp/x"); err == nil {
		t.Fatalf("expected error for an unknown job mode")
	}
}

// Default mode is draft when unset.
func TestNewSessionFromEnv_DefaultModeDraft(t *testing.T) {
	env := map[string]string{envArtifactKind: "Mission"}
	s, err := newSessionFromEnv(func(k string) string { return env[k] }, "/tmp/x")
	if err != nil {
		t.Fatalf("newSessionFromEnv: %v", err)
	}
	if s.Mode != jobModeDraft {
		t.Fatalf("default mode = %q, want draft", s.Mode)
	}
}
