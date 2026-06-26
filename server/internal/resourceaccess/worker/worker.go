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

// The WorkerAccess port interface and its I/O value types (GenerateSpec,
// ToolSpec, ToolTurnSpec, Message, ToolCall, ToolResult, AssistantTurn, and the
// named scalar WorkerClass) are now GENERATED from contract.schema.json into
// contract.gen.go (schema-first; edit the schema and run `make gen`). Each method
// takes the ResourceAccess call Context `rc fwra.Context` first — it embeds
// context.Context and carries the Principal + IdempotencyKey, so the cross-cutting
// ctx/idempotencyKey that the hand-written surface passed explicitly now ride the
// context. The design rationale not captured by the generated signatures:
//
//   - Generate forwards spec.Prompt to the provider (model chosen per
//     spec.WorkerClass inside the seam), requests a JSON-format response, and
//     returns the raw bytes (the worker does NOT unmarshal — that is
//     GenerateTypedData's job). BLOCKING; idempotent on rc.IdempotencyKey (same key
//     replays the recorded bytes). A Cancel followed by Generate on the same key
//     returns nil bytes + nil error (the cancelled-replay sentinel).
//   - GenerateToolTurn runs ONE turn of a tool-calling conversation. The CALLER
//     drives the loop; the worker stays schema-agnostic (ToolSpec.InputSchema and
//     ToolCall.Input are opaque bytes). BLOCKING; idempotent on rc.IdempotencyKey.
//   - Cancel abandons the in-flight run for rc.IdempotencyKey. An unknown /
//     already-terminal key (fwra.NotFound semantics) is SUCCESS — the desired
//     post-condition already holds, which makes cancel safe to retry.
//
// BLOCKING generate-and-collect: the Worker Provider owns no durable
// re-attachable job, so there is no async observe op. Each calling Manager wraps
// a verb in a Manager-owned Activity (long StartToClose, small retry budget).

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
	raw, err := w.Generate(fwra.Context{Context: ctx, IdempotencyKey: idempotencyKey}, spec)
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
