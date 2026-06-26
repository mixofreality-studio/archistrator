package worker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// writeCassetteFile writes a minimal envelope so the read path can find it.
func writeCassetteFile(t *testing.T, dir, key, responseJSON string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	env := map[string]any{
		"workerClass":   "architect",
		"prompt":        "p",
		"tools":         []any{},
		"contextSha256": "",
		"response":      json.RawMessage(responseJSON),
	}
	b, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, key+".json"), b, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// TestNewReplayWorker_Guards — empty dir and unknown mode are ContractMisuse;
// record_on_miss requires a non-nil delegate.
func TestNewReplayWorker_Guards(t *testing.T) {
	if _, err := NewReplayWorker("", ReplayStrict, nil); err == nil {
		t.Fatal("expected error for empty dir")
	}
	if _, err := NewReplayWorker(t.TempDir(), "bogus", nil); err == nil {
		t.Fatal("expected error for unknown mode")
	}
	if _, err := NewReplayWorker(t.TempDir(), ReplayRecordOnMiss, nil); err == nil {
		t.Fatal("expected error: record_on_miss requires a delegate")
	}
	if _, err := NewReplayWorker(t.TempDir(), ReplayStrict, nil); err != nil {
		t.Fatalf("strict mode with nil delegate is valid: %v", err)
	}
}

// TestReplay_Hit_ReturnsCassetteBytes — a present cassette replays without any
// delegate (strict, delegate nil).
func TestReplay_Hit_ReturnsCassetteBytes(t *testing.T) {
	dir := t.TempDir()
	w, err := NewReplayWorker(dir, ReplayStrict, nil)
	if err != nil {
		t.Fatalf("NewReplayWorker: %v", err)
	}
	spec := GenerateSpec{WorkerClass: "architect", Prompt: "draft the glossary"}
	key := cassetteKey(spec)
	writeCassetteFile(t, dir, key, `{"term":"aggregate"}`)

	got, err := w.Generate(rc(context.Background(), "wf:act-1"), spec)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if string(got) != `{"term":"aggregate"}` {
		t.Fatalf("unexpected bytes: %s", string(got))
	}
}

// TestReplay_StrictMiss_IsLoudError — a missing cassette in strict mode is an
// *fwra.Error (ContractMisuse) that names the key and dir.
func TestReplay_StrictMiss_IsLoudError(t *testing.T) {
	dir := t.TempDir()
	w, err := NewReplayWorker(dir, ReplayStrict, nil)
	if err != nil {
		t.Fatalf("NewReplayWorker: %v", err)
	}
	_, err = w.Generate(rc(context.Background(), "wf:act-1"), GenerateSpec{WorkerClass: "architect", Prompt: "no cassette"})
	var fe *fwra.Error
	if err == nil {
		t.Fatal("expected a loud error on strict miss")
	}
	if !errors.As(err, &fe) || fe.Kind != fwra.ContractMisuse {
		t.Fatalf("expected ContractMisuse, got %v", err)
	}
}

// TestReplay_ContractMisuse_GuardsFirst — empty key / empty prompt are rejected
// before any disk lookup.
func TestReplay_ContractMisuse_GuardsFirst(t *testing.T) {
	w, _ := NewReplayWorker(t.TempDir(), ReplayStrict, nil)
	ctx := context.Background()
	if _, err := w.Generate(rc(ctx, ""), GenerateSpec{WorkerClass: "architect", Prompt: "x"}); err == nil {
		t.Fatal("empty key must be ContractMisuse")
	}
	if _, err := w.Generate(rc(ctx, "k"), GenerateSpec{WorkerClass: "architect", Prompt: "  "}); err == nil {
		t.Fatal("empty prompt must be ContractMisuse")
	}
}

// TestCassetteKey_StableAndDistinct — identical specs hash equal; a changed prompt
// hashes differently; Tools/Context participate.
func TestCassetteKey_StableAndDistinct(t *testing.T) {
	a := GenerateSpec{WorkerClass: "architect", Prompt: "draft", Tools: []ToolSpec{{Name: "t", Description: "d"}}, Context: []byte("ctx")}
	b := GenerateSpec{WorkerClass: "architect", Prompt: "draft", Tools: []ToolSpec{{Name: "t", Description: "d"}}, Context: []byte("ctx")}
	if cassetteKey(a) != cassetteKey(b) {
		t.Fatal("identical specs must hash equal")
	}
	c := a
	c.Prompt = "draft v2"
	if cassetteKey(a) == cassetteKey(c) {
		t.Fatal("a changed prompt must hash differently")
	}
	d := a
	d.Context = []byte("other")
	if cassetteKey(a) == cassetteKey(d) {
		t.Fatal("a changed context must hash differently")
	}
}

// TestCassetteKey_NilEqualsEmpty — nil and empty Tools/Context must key identically
// (semantically "none"), so a cassette recorded one way replays the other.
func TestCassetteKey_NilEqualsEmpty(t *testing.T) {
	nilSpec := GenerateSpec{WorkerClass: "architect", Prompt: "draft"}
	emptySpec := GenerateSpec{WorkerClass: "architect", Prompt: "draft", Tools: []ToolSpec{}, Context: []byte{}}
	if cassetteKey(nilSpec) != cassetteKey(emptySpec) {
		t.Fatal("nil and empty Tools/Context must produce the same cassette key")
	}
}

// fakeDelegate is an in-memory WorkerAccess that returns a canned response and
// counts Generate calls, so a test can prove the SECOND Generate replays from
// disk without re-invoking the delegate.
type fakeDelegate struct {
	resp  json.RawMessage
	calls int
}

func (f *fakeDelegate) Generate(_ fwra.Context, _ GenerateSpec) (json.RawMessage, error) {
	f.calls++
	return f.resp, nil
}
func (f *fakeDelegate) Cancel(_ fwra.Context) error { return nil }
func (f *fakeDelegate) GenerateToolTurn(_ fwra.Context, _ ToolTurnSpec) (AssistantTurn, error) {
	f.calls++
	return AssistantTurn{StopReason: "end_turn"}, nil
}

// TestRecordOnMiss_GeneratesThenReplaysFromDisk — first Generate misses → calls
// the delegate and writes a cassette; a FRESH worker over the same dir then
// replays from disk with zero further delegate calls.
func TestRecordOnMiss_GeneratesThenReplaysFromDisk(t *testing.T) {
	dir := t.TempDir()
	del := &fakeDelegate{resp: json.RawMessage(`{"drafted":true}`)}
	w, err := NewReplayWorker(dir, ReplayRecordOnMiss, del)
	if err != nil {
		t.Fatalf("NewReplayWorker: %v", err)
	}
	spec := GenerateSpec{WorkerClass: "architect", Prompt: "draft the mission"}

	got, err := w.Generate(rc(context.Background(), "wf:act-1"), spec)
	if err != nil {
		t.Fatalf("Generate (miss): %v", err)
	}
	if string(got) != `{"drafted":true}` {
		t.Fatalf("unexpected bytes: %s", string(got))
	}
	if del.calls != 1 {
		t.Fatalf("expected exactly 1 delegate call on a miss, got %d", del.calls)
	}
	// The cassette must exist on disk.
	if _, statErr := os.Stat(filepath.Join(dir, cassetteKey(spec)+".json")); statErr != nil {
		t.Fatalf("cassette not written: %v", statErr)
	}

	// A FRESH strict worker over the same dir replays without any delegate.
	w2, err := NewReplayWorker(dir, ReplayStrict, nil)
	if err != nil {
		t.Fatalf("NewReplayWorker (replay): %v", err)
	}
	got2, err := w2.Generate(rc(context.Background(), "wf2:act-1"), spec)
	if err != nil {
		t.Fatalf("Generate (replay): %v", err)
	}
	if string(got2) != `{"drafted":true}` {
		t.Fatalf("replay returned different bytes: %s", string(got2))
	}
}

// TestRecordOnMiss_WithinRunRetry_DoesNotReinvoke — a Temporal retry carrying the
// SAME key replays via idemStore (delegate called once total).
func TestRecordOnMiss_WithinRunRetry_DoesNotReinvoke(t *testing.T) {
	dir := t.TempDir()
	del := &fakeDelegate{resp: json.RawMessage(`{"x":1}`)}
	w, _ := NewReplayWorker(dir, ReplayRecordOnMiss, del)
	spec := GenerateSpec{WorkerClass: "architect", Prompt: "draft"}
	const key = fwra.IdempotencyKey("wf:act-1")

	if _, err := w.Generate(rc(context.Background(), key), spec); err != nil {
		t.Fatalf("first Generate: %v", err)
	}
	if _, err := w.Generate(rc(context.Background(), key), spec); err != nil {
		t.Fatalf("retry Generate: %v", err)
	}
	if del.calls != 1 {
		t.Fatalf("within-run retry must replay (1 delegate call), got %d", del.calls)
	}
}

// TestRecordOnMiss_CancelThenGenerate_ReturnsNil — Cancel(key) then Generate(key)
// returns nil bytes / nil error and does NOT call the delegate.
func TestRecordOnMiss_CancelThenGenerate_ReturnsNil(t *testing.T) {
	dir := t.TempDir()
	del := &fakeDelegate{resp: json.RawMessage(`{"x":1}`)}
	w, _ := NewReplayWorker(dir, ReplayRecordOnMiss, del)
	const key = fwra.IdempotencyKey("wf-cancel:act-1")
	if err := w.Cancel(rc(context.Background(), key)); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	got, err := w.Generate(rc(context.Background(), key), GenerateSpec{WorkerClass: "architect", Prompt: "draft"})
	if err != nil {
		t.Fatalf("expected nil error after cancel, got %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil bytes after cancel, got %s", string(got))
	}
	if del.calls != 0 {
		t.Fatalf("cancel-replay must not call the delegate, got %d calls", del.calls)
	}
}
