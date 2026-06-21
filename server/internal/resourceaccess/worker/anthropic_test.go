package worker

import (
	"context"
	"errors"
	"testing"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// Developer-owned regression harness for the production AnthropicWorker. The
// offline surface — constructor guards, the contract-misuse guards, the shared
// idempotency replay path, and the cancel-replay nil path — is asserted WITHOUT a
// live provider call (a recorded result replays without dialling Anthropic). The
// live Messages-API round-trip is covered by the manager integration suite, not
// here, so `go test` stays fast, offline, and free of API-key/billing needs.

// newOfflineAnthropicWorker builds a worker with a dummy key. Tests must never
// drive an unrecorded key through Generate (that would call the real API); they
// pre-record results to exercise the replay paths.
func newOfflineAnthropicWorker(t *testing.T) *AnthropicWorker {
	t.Helper()
	w, err := NewAnthropicWorker("test-key", "", "claude-opus-4-8", map[WorkerClass]string{
		"architect":      "claude-opus-4-8",
		"productManager": "claude-sonnet-4-6",
	})
	if err != nil {
		t.Fatalf("NewAnthropicWorker: %v", err)
	}
	return w
}

// TestNewAnthropicWorker_RejectsEmptyConfig — constructor pre-condition guards: an
// empty API key or empty default model is fwra.ContractMisuse.
func TestNewAnthropicWorker_RejectsEmptyConfig(t *testing.T) {
	if _, err := NewAnthropicWorker("", "", "claude-opus-4-8", nil); err == nil {
		t.Fatal("expected error for empty apiKey")
	}
	if _, err := NewAnthropicWorker("k", "", "", nil); err == nil {
		t.Fatal("expected error for empty defaultModel")
	}
}

// TestAnthropicGenerate_ContractMisuse — Generate rejects an empty key OR an empty
// prompt as fwra.ContractMisuse BEFORE any provider call.
func TestAnthropicGenerate_ContractMisuse(t *testing.T) {
	w := newOfflineAnthropicWorker(t)
	ctx := context.Background()

	if _, err := w.Generate(ctx, GenerateSpec{WorkerClass: "architect", Prompt: "do x"}, ""); !isKind(err, fwra.ContractMisuse) {
		t.Fatalf("empty key: expected ContractMisuse, got %v", err)
	}
	if _, err := w.Generate(ctx, GenerateSpec{WorkerClass: "architect", Prompt: "   "}, "k1"); !isKind(err, fwra.ContractMisuse) {
		t.Fatalf("empty prompt: expected ContractMisuse, got %v", err)
	}
}

// TestAnthropicGenerate_Idempotency_ReplaysWithoutProviderCall — a pre-recorded
// json.RawMessage replays on the next Generate with that key WITHOUT a provider
// call (the dummy key would otherwise fail against the real API).
func TestAnthropicGenerate_Idempotency_ReplaysWithoutProviderCall(t *testing.T) {
	w := newOfflineAnthropicWorker(t)
	ctx := context.Background()
	const key = fwra.IdempotencyKey("wf:act-1")

	recorded := jsonRaw(`{"hello":"world"}`)
	w.record(key, recorded)

	got, err := w.Generate(ctx, GenerateSpec{WorkerClass: "architect", Prompt: "draft it"}, key)
	if err != nil {
		t.Fatalf("replay must not invoke the provider (got error %v)", err)
	}
	if string(got) != string(recorded) {
		t.Fatalf("replay returned different bytes: %s", string(got))
	}
}

// TestAnthropicGenerate_AfterCancel_ReturnsNil — Cancel(key) then Generate(key)
// returns nil bytes with nil error (the cancelled-replay path).
func TestAnthropicGenerate_AfterCancel_ReturnsNil(t *testing.T) {
	w := newOfflineAnthropicWorker(t)
	ctx := context.Background()
	const key = fwra.IdempotencyKey("wf-cancel:act-1")

	if err := w.Cancel(ctx, key); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	got, err := w.Generate(ctx, GenerateSpec{WorkerClass: "architect", Prompt: "draft it"}, key)
	if err != nil {
		t.Fatalf("expected nil error after cancel-then-generate, got: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil bytes after cancel, got: %s", string(got))
	}
}

// TestAnthropicCancel_UnknownKey_Success — Cancel on a never-dispatched key is a
// no-op success.
func TestAnthropicCancel_UnknownKey_Success(t *testing.T) {
	w := newOfflineAnthropicWorker(t)
	if err := w.Cancel(context.Background(), "never-dispatched"); err != nil {
		t.Fatalf("Cancel on an unknown key must succeed, got: %v", err)
	}
}

// TestResolveModel_PerWorkerClass — the WorkerClass→model registry maps known
// classes and falls back to the default model for unknown/unmapped classes.
func TestResolveModel_PerWorkerClass(t *testing.T) {
	classModels := map[WorkerClass]string{
		"architect":      "claude-opus-4-8",
		"productManager": "claude-sonnet-4-6",
	}
	cases := []struct {
		class WorkerClass
		want  string
	}{
		{"architect", "claude-opus-4-8"},
		{"productManager", "claude-sonnet-4-6"},
		{"unmapped", "default-model"},
	}
	for _, c := range cases {
		if got := resolveModel(classModels, "default-model", c.class); got != c.want {
			t.Fatalf("resolveModel(%q): got %q, want %q", c.class, got, c.want)
		}
	}
}

// isKind reports whether err is an *fwra.Error of the given kind.
func isKind(err error, want fwra.Kind) bool {
	var e *fwra.Error
	if err == nil || !errors.As(err, &e) {
		return false
	}
	return e.Kind == want
}
