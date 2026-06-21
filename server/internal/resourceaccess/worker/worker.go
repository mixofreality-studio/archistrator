// Package worker is the workerAccess component of the aiarch server's
// ResourceAccess layer — the Temporal-free port over the external Worker Provider
// (an LLM API: Anthropic in production, Ollama in tests). It is the only component
// permitted to call workerProvider (workerAccess.md §1f). The seam encapsulates
// Worker volatility: the WorkerClass→(provider, model) registry lives behind this
// surface, and the concrete provider (AnthropicWorker / OllamaWorker) is a wiring
// choice the contract never sees.
//
// Per The Method's layer model ([[the-method-layers]]): ResourceAccess components
// import NO Temporal. Each calling Manager wraps a verb in a Manager-owned
// Activity (long StartToClose, small retry budget); idempotencyKey is a plain
// caller-supplied parameter and this package never reads Temporal context. This
// package also calls NO sibling ResourceAccess (RA-never-calls-RA) and imports NO
// Method-model types (no projectstate, no artifact).
//
// Surface (2026-05-29 GENERIC TYPED worker re-cut — workerAccess.md §0b, §1f–§9f):
//   - Generate(ctx, spec, key) → json.RawMessage — the encapsulated, mockable LLM
//     round-trip (the interface method; the only one that crosses the provider seam).
//   - GenerateTypedData[T](ctx, w, spec, key) → T — the package-level generic helper
//     that unmarshals the raw bytes into the caller's type T and runs T's optional
//     Validate() shape hook. An unconstructable response is a distinct *UnmarshalError.
//   - Cancel(ctx, key) → error — abandon an in-flight run (idempotent).
//
// This REVERTS the 2026-05-27 thin Dispatch/FileUpload transport: Dispatch, the
// FileUpload payload type, and DispatchSpec are GONE. The worker no longer returns
// raw bytes for an Engine to parse — deserialize-to-T is mechanical and lives at
// this seam (GenerateTypedData). The per-step prompt corpus + tool choice are the
// CALLER's (a Manager's sequence); workerAccess holds none of them.
package worker

import (
	"context"
	"encoding/json"
	"fmt"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// WorkerAccess is the Temporal-free port over the Worker Provider (workerAccess.md
// §1f/§2f). One logical operation — generateTypedData[T] — realized in Go as the
// non-generic Generate interface method (raw JSON round-trip) plus the
// package-level GenerateTypedData[T] helper (mechanical unmarshal), since Go
// forbids generic methods on an interface. Plus Cancel for abandoning a run.
//
// BLOCKING generate-and-collect: the Worker Provider owns no durable
// re-attachable job, so there is no async observe op. Each calling Manager wraps
// a verb in a Manager-owned Activity.
type WorkerAccess interface {
	// Generate sends spec.Prompt to the provider (model chosen per
	// spec.WorkerClass inside the seam), requests a JSON-format response, and
	// returns the raw response bytes. BLOCKING; idempotent on idempotencyKey (same
	// key replays the recorded bytes without re-invoking the provider).
	//
	// The worker does NOT unmarshal — that is GenerateTypedData's job. A
	// Cancel(key) followed by Generate(key) returns nil bytes and nil error
	// (the cancelled-replay path; the caller treats nil as cancelled).
	//
	// Transport/auth/quota errors are *fwra.Error. A response that arrives but
	// cannot be treated as a valid T is shape-checked by GenerateTypedData, not
	// here — this method returns whatever bytes the provider produced.
	//
	// Imports no Temporal; the calling Manager wraps in an Activity.
	Generate(ctx context.Context, spec GenerateSpec, idempotencyKey fwra.IdempotencyKey) (json.RawMessage, error)

	// GenerateToolTurn runs ONE turn of a tool-calling conversation and returns the
	// model's next assistant turn (free text + the tool calls it wants executed +
	// the stop reason). It is the multi-turn, tool-native counterpart of Generate:
	// the CALLER drives the loop — it executes each returned ToolCall, appends a
	// matching ToolResult as a user Message, and calls GenerateToolTurn again with
	// the grown Messages slice, until StopReason is no longer ToolUse (or a `finish`
	// tool fires). The worker stays schema-agnostic: ToolSpec.InputSchema and the
	// ToolCall.Input bytes are opaque to it (no Method types). BLOCKING; idempotent
	// on idempotencyKey (a Temporal retry of the same turn replays the recorded
	// AssistantTurn). A Cancel(key) preceding the turn replays as a zero AssistantTurn
	// (StopReason == "") which the caller treats as cancelled.
	//
	// Imports no Temporal; the calling Manager wraps each turn in an Activity.
	GenerateToolTurn(ctx context.Context, spec ToolTurnSpec, idempotencyKey fwra.IdempotencyKey) (AssistantTurn, error)

	// Cancel abandons the in-flight run identified by idempotencyKey. An unknown
	// / already-terminal key (fwra.NotFound semantics) is treated as SUCCESS
	// (returns nil) — the desired post-condition ("this run consumes no further
	// Worker resource") already holds, which makes cancel safe to retry.
	Cancel(ctx context.Context, idempotencyKey fwra.IdempotencyKey) error
}

// GenerateSpec is the caller-owned generation request for the generic typed
// worker surface (Generate / GenerateTypedData). The Prompt + Tools are the
// CALLER's (a Manager's sequence); workerAccess holds no Method-specific corpus
// and no vendor lexeme (no model name, temperature, or token cap) appears here.
type GenerateSpec struct {
	// WorkerClass is the LOGICAL class chosen upstream (by handOffEngine where
	// relevant); it maps to a (provider, model) INSIDE the access layer — never a
	// vendor id on the surface.
	WorkerClass WorkerClass
	// Prompt is the fully-assembled prompt the caller owns (system + few-shot +
	// instruction + serialized context). The worker forwards it verbatim.
	Prompt string
	// Tools is the optional set of logical tools the worker may call this run; the
	// caller drives the round-trips. Surface note: no tool framework is asserted on
	// the contract; the wiring is a code-stage concern (workerAccess.md §9f OQ-3).
	Tools []ToolSpec
	// Context is an optional caller-owned context payload (e.g. serialized prior
	// typed models). Opaque bytes to the worker.
	Context []byte
}

// ToolSpec is a logical tool the caller permits the worker to call this run
// (workerAccess.md §3f / §9f OQ-3, resolved at the code stage). InputSchema is the
// caller's JSON Schema for the tool's `input` — OPAQUE bytes to the worker (it is
// the reflected Method-draft schema, but the worker never interprets it). Strict
// asks the provider to constrain decoding to that schema (guaranteed schema-valid
// tool input) where the provider supports it.
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
	Strict      bool            `json:"strict,omitempty"`
}

// ToolTurnSpec is the caller-owned request for ONE turn of a tool-calling
// conversation (GenerateToolTurn). The worker forwards it verbatim and holds no
// loop state: Messages is the FULL running conversation the caller grows each turn.
type ToolTurnSpec struct {
	// WorkerClass is the LOGICAL class chosen upstream; it maps to a (provider,
	// model) INSIDE the seam — never a vendor id on the surface.
	WorkerClass WorkerClass
	// System is an optional provider-mechanical / caller instruction applied to the
	// whole conversation (e.g. "draft every use case, one submit_use_case call each").
	System string
	// Messages is the running conversation: alternating user/assistant turns. A user
	// turn carries Text and/or ToolResults; an assistant turn carries Text and/or the
	// ToolCalls the model previously emitted (echoed back so the provider sees history).
	Messages []Message
	// Tools is the set of tools the model may call this turn.
	Tools []ToolSpec
}

// Message is one turn of a tool-calling conversation. Opaque envelope: the worker
// maps it to the provider's wire shape and back, never interpreting tool inputs.
type Message struct {
	Role        string       `json:"role"` // "user" | "assistant"
	Text        string       `json:"text,omitempty"`
	ToolCalls   []ToolCall   `json:"toolCalls,omitempty"`   // assistant turn: the model's tool calls
	ToolResults []ToolResult `json:"toolResults,omitempty"` // user turn: answers to prior tool calls
}

// ToolCall is one model-emitted tool invocation. ID correlates the later
// ToolResult (provider-assigned for Anthropic; synthesized for providers without
// native ids). Input is the raw tool arguments (schema-constrained when Strict).
type ToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input,omitempty"`
}

// ToolResult is the caller's answer to a prior ToolCall, carried on a user
// Message. Content is the result text — an ok acknowledgement, or (IsError) the
// actionable failure the model should self-correct from on the next turn.
type ToolResult struct {
	ToolCallID string `json:"toolCallId"`
	Content    string `json:"content"`
	IsError    bool   `json:"isError,omitempty"`
}

// AssistantTurn is one model response in a tool loop: any free text, the tool
// calls to execute, and the stop reason ("tool_use" → execute and continue;
// "end_turn" → the model is done). A zero AssistantTurn (StopReason == "") is the
// cancelled-replay sentinel.
type AssistantTurn struct {
	Text       string     `json:"text,omitempty"`
	ToolCalls  []ToolCall `json:"toolCalls,omitempty"`
	StopReason string     `json:"stopReason"`
}

// WorkerClass is a LOGICAL worker class — NOT a model name. It maps to a
// (provider, model) INSIDE this seam ("planner" | "coder" | "reviewer" | ...).
type WorkerClass string

// Error is the shared ResourceAccess error model (framework-go), aliased so this
// component's contract reads in its own terms while every RA component shares one
// fixed enum. Construct with fwra.New / fwra.Wrap. Kinds relevant to workerAccess:
// fwra.Transient, fwra.RateLimited, fwra.Auth, fwra.QuotaExhausted, fwra.NotFound,
// fwra.Infrastructure, fwra.ContractMisuse.
type Error = fwra.Error

// UnmarshalError is the DISTINCT, non-fwra error GenerateTypedData returns when
// the provider's response arrives WITHOUT a transport error BUT cannot be
// unmarshalled into a valid T (or fails T's optional Validate() shape hook). It
// signals "the worker ran but produced something that is not a T" — the caller
// routes it through intervention. It carries the raw bytes so the caller can
// decide redraft-vs-escalate without re-invoking the provider.
//
// This is deliberately NOT an fwra.Error: an unconstructable response is
// categorically above the transport layer. Transport/auth/quota errors from
// Generate bubble up verbatim as *fwra.Error and never reach UnmarshalError
// (workerAccess.md §2f.2 / §3f Error model).
type UnmarshalError struct {
	// Raw is the verbatim provider response bytes that could not be constructed
	// into a valid T.
	Raw []byte
	// Err is the unmarshal or Validate() failure.
	Err error
}

func (e *UnmarshalError) Error() string {
	return fmt.Sprintf("worker: response could not be unmarshalled into the requested type: %v", e.Err)
}

func (e *UnmarshalError) Unwrap() error { return e.Err }

// GenerateTypedData asks the worker for a JSON value of type T and returns it —
// the one logical generateTypedData[T] operation (workerAccess.md §2f.2). It
// calls w.Generate, json-unmarshals the response into T, and — if *T (or T)
// implements interface{ Validate() error } — runs that mechanical shape check
// (NOT semantic validation, which remains artifactValidationEngine's job).
//
// A response that cannot be unmarshalled or fails Validate() is returned as a
// *UnmarshalError (carrying the raw bytes), distinct from a transport error.
// Transport/auth/quota errors from w.Generate bubble up verbatim (*fwra.Error).
//
// A nil (cancelled) response from Generate (when Cancel preceded the key) returns
// the zero value of T with nil error — callers treat zero T as cancelled.
func GenerateTypedData[T any](ctx context.Context, w WorkerAccess, spec GenerateSpec, idempotencyKey fwra.IdempotencyKey) (T, error) {
	var zero T
	raw, err := w.Generate(ctx, spec, idempotencyKey)
	if err != nil {
		return zero, err
	}
	if raw == nil {
		// Cancel-then-Generate path: replays as a nil message with nil error.
		return zero, nil
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		return zero, &UnmarshalError{Raw: raw, Err: err}
	}
	// Optional shape hook: if *T implements Validate() error, run it.
	if v, ok := any(&out).(interface{ Validate() error }); ok {
		if vErr := v.Validate(); vErr != nil {
			return zero, &UnmarshalError{Raw: raw, Err: vErr}
		}
	}
	return out, nil
}
