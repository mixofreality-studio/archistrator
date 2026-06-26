package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	llminfra "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-llm/testinfra"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// This is the developer-owned regression harness (N-RTH) for C-WA. The generic
// typed worker surface (Generate / GenerateTypedData[T] / Cancel) is tested:
//   (a) offline via the contract-misuse guards, the idempotency replay path (a
//       pre-recorded result replays without any provider call), the cancel-replay
//       nil path, the GenerateTypedData unmarshal + Validate() shape hook, the
//       distinct *UnmarshalError on an unconstructable response, and an
//       unreachable endpoint (typed *fwra.Error bubbling verbatim);
//   (b) end-to-end against a real Ollama testcontainer that is SKIPPED under
//       -short (so `go test -short` is fast and container-free).
//
// 2026-05-29 re-cut (workerAccess.md §0b): the thin Dispatch/FileUpload transport
// is reverted; deserialize-to-T is mechanical and lives at this seam.

// ---- Generate (the raw-JSON interface method) -------------------------------

// TestGenerate_ContractMisuse — Generate rejects an empty key OR an empty prompt
// as fwra.ContractMisuse BEFORE any infrastructure call (no dial of the dead
// endpoint).
func TestGenerate_ContractMisuse(t *testing.T) {
	w := newDeadWorker(t)
	ctx := context.Background()

	_, err := w.Generate(rc(ctx, ""), GenerateSpec{WorkerClass: "planner", Prompt: "do x"})
	assertKind(t, err, fwra.ContractMisuse)

	_, err = w.Generate(rc(ctx, "k1"), GenerateSpec{WorkerClass: "planner", Prompt: "   "})
	assertKind(t, err, fwra.ContractMisuse)
}

// TestGenerate_Idempotency_ReplaysWithoutProviderCall — a pre-recorded
// json.RawMessage for a key replays on the next Generate with that key WITHOUT
// dialling the (here dead) endpoint.
func TestGenerate_Idempotency_ReplaysWithoutProviderCall(t *testing.T) {
	w := newDeadWorker(t)
	ctx := context.Background()
	const key = fwra.IdempotencyKey("wf:act-1")

	recorded := jsonRaw(`{"hello":"world"}`)
	w.record(key, recorded)

	got, err := w.Generate(rc(ctx, key), GenerateSpec{WorkerClass: "planner", Prompt: "draft it"})
	if err != nil {
		t.Fatalf("replay must not invoke the provider (got error %v)", err)
	}
	if string(got) != string(recorded) {
		t.Fatalf("replay returned different bytes: %s", string(got))
	}

	// A DISTINCT key has no recording, so it DOES dial the dead endpoint → error.
	if _, err := w.Generate(rc(ctx, "wf:act-2"), GenerateSpec{WorkerClass: "planner", Prompt: "draft it"}); err == nil {
		t.Fatal("a distinct (unrecorded) key must invoke the provider and error against the dead endpoint")
	}
}

// TestGenerate_AfterCancel_ReturnsNil — Cancel(key) then Generate(key) returns
// nil bytes with nil error (NOT a panic). The cancelledRun marker stored under
// the key must be observed via comma-ok type assertion, not an unchecked one.
func TestGenerate_AfterCancel_ReturnsNil(t *testing.T) {
	w := newDeadWorker(t)
	ctx := context.Background()
	const key = fwra.IdempotencyKey("wf-cancel:act-1")

	if err := w.Cancel(rc(ctx, key)); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	got, err := w.Generate(rc(ctx, key), GenerateSpec{WorkerClass: "planner", Prompt: "draft it"})
	if err != nil {
		t.Fatalf("expected nil error after cancel-then-generate, got: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil bytes after cancel, got: %s", string(got))
	}
}

// TestCancel_UnknownKey_Success — Cancel on a never-dispatched key is a no-op
// success (returns nil).
func TestCancel_UnknownKey_Success(t *testing.T) {
	w := newDeadWorker(t)
	if err := w.Cancel(rc(context.Background(), "never-dispatched")); err != nil {
		t.Fatalf("Cancel on an unknown key must succeed, got: %v", err)
	}
}

// ---- GenerateTypedData[T] (the package-level generic helper) -----------------

// sample is a representative caller-owned T with a Validate() shape hook.
type sample struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Validate is the optional mechanical shape hook GenerateTypedData runs after
// unmarshal: Name must be present.
func (s *sample) Validate() error {
	if s.Name == "" {
		return errors.New("sample: Name must not be empty")
	}
	return nil
}

// TestGenerateTypedData_UnmarshalsAndValidates — a recorded JSON response is
// unmarshalled into the caller's T and the Validate() hook passes.
func TestGenerateTypedData_UnmarshalsAndValidates(t *testing.T) {
	w := newDeadWorker(t)
	ctx := context.Background()
	const key = fwra.IdempotencyKey("typed:ok")
	w.record(key, jsonRaw(`{"name":"alpha","count":3}`))

	got, err := GenerateTypedData[sample](ctx, w, GenerateSpec{WorkerClass: "planner", Prompt: "draft"}, key)
	if err != nil {
		t.Fatalf("GenerateTypedData: %v", err)
	}
	if got.Name != "alpha" || got.Count != 3 {
		t.Fatalf("unexpected typed value: %+v", got)
	}
}

// TestGenerateTypedData_UnmarshalError_OnBadJSON — a response that is not
// decodable into T returns a DISTINCT *worker.UnmarshalError (not an *fwra.Error),
// carrying the raw bytes for the caller's redraft-vs-escalate decision.
func TestGenerateTypedData_UnmarshalError_OnBadJSON(t *testing.T) {
	w := newDeadWorker(t)
	ctx := context.Background()
	const key = fwra.IdempotencyKey("typed:badjson")
	w.record(key, jsonRaw(`not json at all`))

	_, err := GenerateTypedData[sample](ctx, w, GenerateSpec{WorkerClass: "planner", Prompt: "draft"}, key)
	assertUnmarshalError(t, err)
}

// TestGenerateTypedData_UnmarshalError_OnValidateFail — a response that decodes
// but fails T's Validate() shape hook is also a *worker.UnmarshalError
// (unconstructable), not a transport error.
func TestGenerateTypedData_UnmarshalError_OnValidateFail(t *testing.T) {
	w := newDeadWorker(t)
	ctx := context.Background()
	const key = fwra.IdempotencyKey("typed:validatefail")
	w.record(key, jsonRaw(`{"count":7}`)) // missing required Name

	_, err := GenerateTypedData[sample](ctx, w, GenerateSpec{WorkerClass: "planner", Prompt: "draft"}, key)
	assertUnmarshalError(t, err)
}

// TestGenerateTypedData_TransportError_BubblesAsFwra — a transport error from
// Generate (dead endpoint, unrecorded key) bubbles up VERBATIM as an *fwra.Error,
// NOT wrapped as an UnmarshalError.
func TestGenerateTypedData_TransportError_BubblesAsFwra(t *testing.T) {
	w := newDeadWorker(t)
	ctx := context.Background()

	_, err := GenerateTypedData[sample](ctx, w, GenerateSpec{WorkerClass: "planner", Prompt: "draft"}, "typed:dead")
	if err == nil {
		t.Fatal("expected a transport error against the dead endpoint")
	}
	var ue *UnmarshalError
	if errors.As(err, &ue) {
		t.Fatalf("transport error must NOT be an UnmarshalError, got %v", err)
	}
	var fe *fwra.Error
	if !errors.As(err, &fe) {
		t.Fatalf("expected *fwra.Error, got %T: %v", err, err)
	}
}

// TestGenerateTypedData_AfterCancel_ReturnsZeroT — a cancelled (nil) response
// returns the zero T with nil error (callers treat zero T as cancelled).
func TestGenerateTypedData_AfterCancel_ReturnsZeroT(t *testing.T) {
	w := newDeadWorker(t)
	ctx := context.Background()
	const key = fwra.IdempotencyKey("typed:cancel")
	if err := w.Cancel(rc(ctx, key)); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	got, err := GenerateTypedData[sample](ctx, w, GenerateSpec{WorkerClass: "planner", Prompt: "draft"}, key)
	if err != nil {
		t.Fatalf("cancel-replay must return nil error, got %v", err)
	}
	if got.Name != "" || got.Count != 0 {
		t.Fatalf("expected zero T after cancel, got %+v", got)
	}
}

// ---- end-to-end (real Ollama testcontainer; skipped under -short) -----------

// TestGenerate_RoundTrip exercises the full Generate path against a real Ollama
// testcontainer. LLM output is non-deterministic so the assertion is STRUCTURAL:
// the call returns without a transport error and non-empty JSON bytes. Skipped
// under -short.
func TestGenerate_RoundTrip(t *testing.T) {
	o := llminfra.StartOllama(t) // skips under -short
	w, err := NewOllamaWorker(o.BaseURL, o.Model, map[WorkerClass]string{"planner": o.Model})
	if err != nil {
		t.Fatalf("NewOllamaWorker: %v", err)
	}

	got, err := w.Generate(rc(context.Background(), "wf-e2e:act-1"), GenerateSpec{
		WorkerClass: "planner",
		Prompt:      `Respond with a JSON object {"word":"hello"} and nothing else.`,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected non-empty JSON bytes from a successful provider call")
	}
}

// ---- helpers ----------------------------------------------------------------

// jsonRaw returns a json.RawMessage (the concrete type Generate records under a
// key) so the dedup-map replay type assertion in Generate matches.
func jsonRaw(s string) json.RawMessage { return json.RawMessage(s) }

// newDeadWorker builds a worker pointed at a reserved/unbound port (no container).
func newDeadWorker(t *testing.T) *OllamaWorker {
	t.Helper()
	w, err := NewOllamaWorker("http://127.0.0.1:1", "qwen2.5:0.5b", nil)
	if err != nil {
		t.Fatalf("NewOllamaWorker: %v", err)
	}
	return w
}

func assertKind(t *testing.T, err error, want fwra.Kind) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error of kind %s, got nil", want)
	}
	var e *fwra.Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *fwra.Error, got %T: %v", err, err)
	}
	if e.Kind != want {
		t.Fatalf("expected kind %s, got %s (detail: %s)", want, e.Kind, e.Detail)
	}
}

func assertUnmarshalError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected *UnmarshalError, got nil")
	}
	var ue *UnmarshalError
	if !errors.As(err, &ue) {
		t.Fatalf("expected *UnmarshalError, got %T: %v", err, err)
	}
	// It must NOT be an fwra transport error.
	var fe *fwra.Error
	if errors.As(err, &fe) {
		t.Fatalf("UnmarshalError must be distinct from *fwra.Error, got %v", err)
	}
	if len(ue.Raw) == 0 {
		t.Fatal("UnmarshalError must carry the raw bytes for diagnostics")
	}
}

// TestNewOllamaWorker_RejectsEmptyConfig — constructor pre-condition guards.
func TestNewOllamaWorker_RejectsEmptyConfig(t *testing.T) {
	if _, err := NewOllamaWorker("", "m", nil); err == nil {
		t.Fatal("expected error for empty baseURL")
	}
	if _, err := NewOllamaWorker("http://x", "", nil); err == nil {
		t.Fatal("expected error for empty defaultModel")
	}
}
