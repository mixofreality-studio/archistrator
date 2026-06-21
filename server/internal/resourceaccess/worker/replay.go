// Package worker — ReplayWorker is a WorkerAccess DECORATOR backed by on-disk
// cassettes keyed by a content hash of the GenerateSpec. It exists so the
// archistrator system/UI tests run fast, offline, and deterministically: the
// expensive, non-deterministic Worker Provider is replaced by byte-replay of a
// previously recorded response.
//
// Modes (replay.md / design 2026-06-05):
//   - ReplayStrict        — a cassette miss is a loud *fwra.Error (CI default).
//   - ReplayRecordOnMiss  — a miss calls the wrapped delegate, writes the
//     cassette, and returns it (VCR new_episodes).
//
// Like the other concrete workers it embeds *idemStore for within-run
// idempotencyKey replay (a Temporal retry returns the same bytes). The on-disk
// cassette provides CROSS-run determinism; the idemStore provides within-run.
package worker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// ReplayMode selects miss behaviour for a ReplayWorker.
type ReplayMode string

const (
	// ReplayStrict returns a loud error on a cassette miss (offline, deterministic).
	ReplayStrict ReplayMode = "strict"
	// ReplayRecordOnMiss generates a missing cassette via the delegate and writes it.
	ReplayRecordOnMiss ReplayMode = "record_on_miss"
)

// ReplayWorker decorates an optional real WorkerAccess delegate with on-disk
// cassette replay. delegate is nil in strict mode.
type ReplayWorker struct {
	dir      string
	mode     ReplayMode
	delegate WorkerAccess
	writeMu  sync.Mutex
	*idemStore
}

// compile-time proof the decorator satisfies the port.
var _ WorkerAccess = (*ReplayWorker)(nil)

// NewReplayWorker builds a ReplayWorker over a cassette directory. In strict mode
// delegate may be nil; in record_on_miss mode delegate is required (it serves
// misses). An unknown mode or empty dir is ContractMisuse.
func NewReplayWorker(dir string, mode ReplayMode, delegate WorkerAccess) (*ReplayWorker, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fwra.New(fwra.ContractMisuse, "replay worker: empty cassette dir")
	}
	switch mode {
	case ReplayStrict:
	case ReplayRecordOnMiss:
		if delegate == nil {
			return nil, fwra.New(fwra.ContractMisuse, "replay worker: record_on_miss requires a non-nil delegate")
		}
	default:
		return nil, fwra.New(fwra.ContractMisuse, fmt.Sprintf("replay worker: unknown mode %q", mode))
	}
	return &ReplayWorker{dir: dir, mode: mode, delegate: delegate, idemStore: newIdemStore()}, nil
}

// Cancel records the cancelled marker for within-run replay (no durable provider
// job to abort), matching OllamaWorker.Cancel. Idempotent.
func (w *ReplayWorker) Cancel(_ context.Context, idempotencyKey fwra.IdempotencyKey) error {
	if err := requireKey(idempotencyKey); err != nil {
		return err
	}
	w.record(idempotencyKey, cancelledRun{})
	return nil
}

// Generate replays a cassette keyed by the content hash of spec. On a miss it
// either errors (strict) or generates-and-records via the delegate
// (record_on_miss). Same contract as the other workers: empty key/prompt are
// ContractMisuse; a within-run retry replays via idemStore.
func (w *ReplayWorker) Generate(ctx context.Context, spec GenerateSpec, idempotencyKey fwra.IdempotencyKey) (json.RawMessage, error) {
	if err := requireKey(idempotencyKey); err != nil {
		return nil, err
	}
	if strings.TrimSpace(spec.Prompt) == "" {
		return nil, fwra.New(fwra.ContractMisuse, "Generate: empty spec.Prompt")
	}
	// Within-run replay (Temporal retry of the same activity).
	if raw, done, err := w.replayResult(idempotencyKey); done || err != nil {
		return raw, err
	}

	key := cassetteKey(spec)
	if raw, ok, err := w.readCassette(key); err != nil {
		return nil, err
	} else if ok {
		w.record(idempotencyKey, raw)
		return raw, nil
	}

	if w.mode == ReplayStrict {
		return nil, fwra.New(fwra.ContractMisuse, fmt.Sprintf(
			"replay worker: no cassette for key %s in %s (strict mode); re-record with the WHEN_REQUIRED drafting mode", key, w.dir))
	}
	// record_on_miss: serve via the delegate and persist (Task 2 wires writeCassette).
	return w.generateAndRecord(ctx, key, spec, idempotencyKey)
}

// GenerateToolTurn replays a tool-turn cassette keyed by the content hash of the
// turn spec. On a miss it errors (strict) or generates-and-records via the delegate
// (record_on_miss) — same VCR contract as Generate. Each turn of a tool loop hashes
// a distinct Messages history, so it gets its own cassette; the model's
// self-correction turn (a re-submit after an error tool_result) records as just
// another cassette, replayed deterministically.
func (w *ReplayWorker) GenerateToolTurn(ctx context.Context, spec ToolTurnSpec, idempotencyKey fwra.IdempotencyKey) (AssistantTurn, error) {
	if err := requireKey(idempotencyKey); err != nil {
		return AssistantTurn{}, err
	}
	if turn, done, err := w.replayTurn(idempotencyKey); done || err != nil {
		return turn, err
	}

	key := toolTurnCassetteKey(spec)
	if turn, ok, err := w.readTurnCassette(key); err != nil {
		return AssistantTurn{}, err
	} else if ok {
		w.record(idempotencyKey, turn)
		return turn, nil
	}

	if w.mode == ReplayStrict {
		return AssistantTurn{}, fwra.New(fwra.ContractMisuse, fmt.Sprintf(
			"replay worker: no tool-turn cassette for key %s in %s (strict mode); re-record with the WHEN_REQUIRED drafting mode", key, w.dir))
	}

	turn, err := w.delegate.GenerateToolTurn(ctx, spec, idempotencyKey)
	if err != nil {
		return AssistantTurn{}, err
	}
	if err := w.writeTurnCassette(key, spec, turn); err != nil {
		return AssistantTurn{}, err
	}
	w.record(idempotencyKey, turn)
	return turn, nil
}

// cassetteKey is the SHA-256 hex of a stable JSON encoding of the WHOLE spec
// (WorkerClass, Prompt, Tools in declared order, Context). Tools/Context are
// empty today but participate so the key stays correct when they are populated.
func cassetteKey(spec GenerateSpec) string {
	// Normalize nil to empty so semantically-equal specs ("none") key identically:
	// json.Marshal encodes nil Tools as null but []ToolSpec{} as [], and nil
	// Context as null but []byte{} as "". A cassette recorded one way must replay
	// the other.
	tools := spec.Tools
	if tools == nil {
		tools = []ToolSpec{}
	}
	contextBytes := spec.Context
	if contextBytes == nil {
		contextBytes = []byte{}
	}
	keyInput := struct {
		WorkerClass WorkerClass `json:"workerClass"`
		Prompt      string      `json:"prompt"`
		Tools       []ToolSpec  `json:"tools"`
		Context     []byte      `json:"context"`
	}{spec.WorkerClass, spec.Prompt, tools, contextBytes}
	b, err := json.Marshal(keyInput)
	if err != nil {
		// Unreachable for these field types; fall back to a deterministic seed so
		// the key never panics.
		b = []byte(string(spec.WorkerClass) + "\x00" + spec.Prompt)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// toolTurnCassetteKey is the SHA-256 hex (turn- prefixed so it never collides with
// a Generate cassette) of a stable JSON encoding of the turn spec. Provider-assigned
// tool-call ids are ZEROED before hashing: those ids are non-deterministic across
// recordings (Anthropic assigns a fresh toolu_… each run), so a later turn whose
// request echoes an earlier assistant tool_use would otherwise key differently on
// every re-record. The turn's STRUCTURE (tool names, inputs, result contents, order)
// is what identifies it.
func toolTurnCassetteKey(spec ToolTurnSpec) string {
	msgs := make([]Message, len(spec.Messages))
	for i, m := range spec.Messages {
		cm := Message{Role: m.Role, Text: m.Text}
		for _, tc := range m.ToolCalls {
			cm.ToolCalls = append(cm.ToolCalls, ToolCall{Name: tc.Name, Input: tc.Input})
		}
		for _, tr := range m.ToolResults {
			cm.ToolResults = append(cm.ToolResults, ToolResult{Content: tr.Content, IsError: tr.IsError})
		}
		msgs[i] = cm
	}
	tools := spec.Tools
	if tools == nil {
		tools = []ToolSpec{}
	}
	keyInput := struct {
		WorkerClass WorkerClass `json:"workerClass"`
		System      string      `json:"system"`
		Messages    []Message   `json:"messages"`
		Tools       []ToolSpec  `json:"tools"`
	}{spec.WorkerClass, spec.System, msgs, tools}
	b, err := json.Marshal(keyInput)
	if err != nil {
		b = []byte(string(spec.WorkerClass) + "\x00" + spec.System)
	}
	sum := sha256.Sum256(b)
	return "turn-" + hex.EncodeToString(sum[:])
}

// toolTurnEnvelope is the on-disk, human-reviewable tool-turn record: the request
// shape for diff debuggability plus the recorded assistant Response.
type toolTurnEnvelope struct {
	WorkerClass string        `json:"workerClass"`
	System      string        `json:"system,omitempty"`
	Messages    []Message     `json:"messages"`
	Tools       []ToolSpec    `json:"tools"`
	Response    AssistantTurn `json:"response"`
}

// readTurnCassette returns (turn, true, nil) on a hit, (zero, false, nil) on a
// miss, and a wrapped *fwra.Error on a corrupt/unreadable file.
func (w *ReplayWorker) readTurnCassette(key string) (AssistantTurn, bool, error) {
	path := filepath.Join(w.dir, key+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return AssistantTurn{}, false, nil
		}
		return AssistantTurn{}, false, fwra.Wrap(fwra.Infrastructure, err, "replay worker: read tool-turn cassette")
	}
	var env toolTurnEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return AssistantTurn{}, false, fwra.Wrap(fwra.Infrastructure, err, "replay worker: corrupt tool-turn cassette "+path)
	}
	return env.Response, true, nil
}

// writeTurnCassette persists the tool-turn envelope atomically (temp file + rename)
// under the shared write mutex.
func (w *ReplayWorker) writeTurnCassette(key string, spec ToolTurnSpec, turn AssistantTurn) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()

	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		return fwra.Wrap(fwra.Infrastructure, err, "replay worker: mkdir cassette dir")
	}
	env := toolTurnEnvelope{
		WorkerClass: string(spec.WorkerClass),
		System:      spec.System,
		Messages:    spec.Messages,
		Tools:       spec.Tools,
		Response:    turn,
	}
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fwra.Wrap(fwra.Infrastructure, err, "replay worker: marshal tool-turn cassette")
	}
	path := filepath.Join(w.dir, key+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fwra.Wrap(fwra.Infrastructure, err, "replay worker: write tool-turn cassette")
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fwra.Wrap(fwra.Infrastructure, err, "replay worker: commit tool-turn cassette")
	}
	return nil
}

// cassetteEnvelope is the on-disk, human-reviewable record. Only Response is
// returned to the caller; the rest is for PR-diff debuggability. The raw Context
// feeds cassetteKey; only its SHA (ContextSHA256) is persisted here, for diff
// readability.
type cassetteEnvelope struct {
	WorkerClass   string          `json:"workerClass"`
	Prompt        string          `json:"prompt"`
	Tools         []ToolSpec      `json:"tools"`
	ContextSHA256 string          `json:"contextSha256"`
	Response      json.RawMessage `json:"response"`
}

// readCassette returns (response, true, nil) on a hit, (nil, false, nil) on a
// miss, and a wrapped *fwra.Error on a corrupt/unreadable file.
func (w *ReplayWorker) readCassette(key string) (json.RawMessage, bool, error) {
	path := filepath.Join(w.dir, key+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fwra.Wrap(fwra.Infrastructure, err, "replay worker: read cassette")
	}
	var env cassetteEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, false, fwra.Wrap(fwra.Infrastructure, err, "replay worker: corrupt cassette "+path)
	}
	// The envelope is stored pretty-printed (MarshalIndent re-indents the embedded
	// response), so compact the response to its canonical form before returning —
	// callers see the same bytes the provider produced, not the on-disk layout.
	var compact bytes.Buffer
	if err := json.Compact(&compact, env.Response); err != nil {
		return nil, false, fwra.Wrap(fwra.Infrastructure, err, "replay worker: corrupt cassette response "+path)
	}
	return json.RawMessage(compact.Bytes()), true, nil
}

// generateAndRecord serves a cassette miss in record_on_miss mode: it invokes the
// delegate, persists the response as a cassette, records it for within-run replay,
// and returns it. A nil (cancelled) delegate response is passed through without a
// disk write.
func (w *ReplayWorker) generateAndRecord(ctx context.Context, key string, spec GenerateSpec, idempotencyKey fwra.IdempotencyKey) (json.RawMessage, error) {
	raw, err := w.delegate.Generate(ctx, spec, idempotencyKey)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		// Delegate cancelled-replay: nothing durable to persist.
		return nil, nil
	}
	if err := w.writeCassette(key, spec, raw); err != nil {
		return nil, err
	}
	w.record(idempotencyKey, raw)
	return raw, nil
}

// writeCassette persists the envelope atomically (temp file + rename) under a
// mutex so parallel activities never observe a partial file.
func (w *ReplayWorker) writeCassette(key string, spec GenerateSpec, raw json.RawMessage) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()

	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		return fwra.Wrap(fwra.Infrastructure, err, "replay worker: mkdir cassette dir")
	}
	ctxHash := ""
	if len(spec.Context) > 0 {
		sum := sha256.Sum256(spec.Context)
		ctxHash = hex.EncodeToString(sum[:])
	}
	env := cassetteEnvelope{
		WorkerClass:   string(spec.WorkerClass),
		Prompt:        spec.Prompt,
		Tools:         spec.Tools,
		ContextSHA256: ctxHash,
		Response:      raw,
	}
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fwra.Wrap(fwra.Infrastructure, err, "replay worker: marshal cassette")
	}
	path := filepath.Join(w.dir, key+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fwra.Wrap(fwra.Infrastructure, err, "replay worker: write cassette")
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp) // best-effort: don't leave an orphaned .tmp behind
		return fwra.Wrap(fwra.Infrastructure, err, "replay worker: commit cassette")
	}
	return nil
}
