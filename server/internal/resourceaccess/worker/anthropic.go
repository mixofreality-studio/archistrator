package worker

import (
	"encoding/json"
	"strings"

	fwllm "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-llm"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// anthropicWorker is the PRODUCTION WorkerAccess implementation, backed by the
// Anthropic Messages API (official anthropic-sdk-go, wrapped by the sanctioned llm
// infrastructure module's AnthropicClient). It is UNEXPORTED — the package's public
// surface is the generated WorkerAccess interface + models + the generated
// NewAnthropicWorkerAccess constructor (option-1 generated-DI). It implements the
// generic typed worker surface (Generate + Cancel) of workerAccess.md §2f, exactly
// as ollamaWorker does — the two are interchangeable concrete realizations of the
// same port. The composition root wires this worker in production; ollamaWorker is
// the test-only provider (testcontainers / docker-compose).
//
// Infrastructure-opacity (workerAccess.md §3f): the Anthropic API key, model name,
// max-tokens cap and token meters live ENTIRELY inside this struct (and the opaque
// client). None appear on the WorkerAccess surface. The Ollama→Anthropic swap is
// precisely the Worker-volatility move the design anticipates — it changes only
// the wired concrete worker; the contract, every Manager, Engine and Client are
// unchanged (volatilities.md, Worker volatility).
//
// Layer rules (workerAccess.md §5f, [[the-method-layers]]):
//   - imports NO Temporal — idempotencyKey is an ordinary parameter.
//   - imports NO Method-model types — no projectstate, no artifact.
//   - RA-never-calls-RA — calls NO sibling ResourceAccess.
type anthropicWorker struct {
	client *fwllm.AnthropicClient

	// classModels maps a logical WorkerClass to the concrete Claude model that
	// serves it (the encapsulated Worker volatility, workerAccess.md §3). An unknown
	// class falls back to defaultModel (via resolveModel). Per-class mapping lets a
	// drafting class (e.g. "architect") run a more capable model than a critique
	// class (e.g. "productManager").
	classModels  map[WorkerClass]string
	defaultModel string

	// idemStore is the shared idempotency replay table (idempotency.go), identical
	// in behaviour to OllamaWorker's — a retry carrying the same idempotencyKey
	// replays the recorded response without a second (billed) provider call.
	*idemStore
}

// compile-time proof the concrete impl satisfies the port.
var _ WorkerAccess = (*anthropicWorker)(nil)

// jsonOnlySystem is the provider-mechanical instruction that constrains the
// response to a bare JSON value — the Anthropic analog of Ollama's Format:"json"
// flag, so GenerateTypedData can unmarshal the bytes. It is NOT Method doctrine
// (the per-step prompt corpus is the caller's, workerAccess.md §3f); it is the
// JSON-output mechanism for this provider and nothing more.
const jsonOnlySystem = "Respond with exactly one valid JSON value and nothing else. " +
	"Do not add prose, explanation, or Markdown code fences."

// newAnthropicWorkerAccess is the hand-written, unexported builder behind the
// generated NewAnthropicWorkerAccess constructor (option-1 delegated DI). It builds
// the impl over a framework *fwllm.AnthropicClient (the composition root owns the
// key+endpoint+max-tokens choice when it constructs the client) and returns the
// WorkerAccess interface so the concrete struct + its *idemStore stay unexported.
// defaultModel is the fallback Claude model id; classModels may override it per
// WorkerClass (nil is fine — every class then uses defaultModel). The contract never
// sees the model choice.
func newAnthropicWorkerAccess(client *fwllm.AnthropicClient, defaultModel string, classModels map[WorkerClass]string) (WorkerAccess, error) {
	if client == nil {
		return nil, fwra.New(fwra.ContractMisuse, "anthropic worker: nil client")
	}
	if strings.TrimSpace(defaultModel) == "" {
		return nil, fwra.New(fwra.ContractMisuse, "anthropic worker: empty defaultModel")
	}
	return &anthropicWorker{
		client:       client,
		classModels:  copyClassModels(classModels),
		defaultModel: defaultModel,
		idemStore:    newIdemStore(),
	}, nil
}

// Cancel abandons the in-flight run identified by idempotencyKey. The Anthropic
// Messages API exposes no durable re-attachable job to abort; cancellation here
// records a terminal cancelled marker against the key so a later Generate replay
// observes it as a nil message. An unknown / already-terminal run (fwra.NotFound
// semantics) is SUCCESS — the desired post-condition already holds, which makes
// cancel safe to retry (workerAccess.md §2f.3).
func (w *anthropicWorker) Cancel(rc fwra.Context) error {
	if err := requireKey(rc.IdempotencyKey); err != nil {
		return err
	}
	w.record(rc.IdempotencyKey, cancelledRun{})
	return nil
}

// Generate is the generic typed worker surface (workerAccess.md §2f.1). It forwards
// the CALLER-assembled spec.Prompt to Anthropic (model chosen per spec.WorkerClass
// inside the seam), instructs the provider to emit a bare JSON value, records the
// raw response under the idempotencyKey, and returns it. The worker does NOT
// unmarshal — that is GenerateTypedData's job.
//
// Idempotency: a retry carrying the same key replays the recorded bytes without
// re-invoking (and re-billing) the provider. A Cancel(key) followed by
// Generate(key) returns nil bytes with nil error (treated as cancelled).
func (w *anthropicWorker) Generate(rc fwra.Context, spec GenerateSpec) (json.RawMessage, error) {
	if err := requireKey(rc.IdempotencyKey); err != nil {
		return nil, err
	}
	if strings.TrimSpace(spec.Prompt) == "" {
		return nil, fwra.New(fwra.ContractMisuse, "Generate: empty spec.Prompt")
	}
	if raw, done, err := w.replayResult(rc.IdempotencyKey); done || err != nil {
		// Recorded result (bytes), cancelled run (nil), or corrupt entry (err) —
		// return without re-invoking the provider.
		return raw, err
	}

	resp, err := w.client.Generate(rc, fwllm.AnthropicGenerateRequest{
		Model:  resolveModel(w.classModels, w.defaultModel, spec.WorkerClass),
		System: jsonOnlySystem,
		Prompt: spec.Prompt,
	})
	if err != nil {
		return nil, err
	}

	result := json.RawMessage([]byte(resp.Text))
	w.record(rc.IdempotencyKey, result)
	return result, nil
}

// GenerateToolTurn runs one tool-calling Messages-API turn (workerAccess.md §2f
// tool surface). It maps the caller's generic ToolTurnSpec to the provider-shaped
// fwllm.AnthropicToolRequest, forwards it, and maps the assistant response back to
// a generic AssistantTurn. The worker holds no loop state and never interprets the
// tool schemas/inputs — the Manager drives the loop and owns the tool semantics.
//
// Idempotency mirrors Generate: a retry on the same key replays the recorded
// AssistantTurn; a Cancel(key) first replays as a zero AssistantTurn (cancelled).
func (w *anthropicWorker) GenerateToolTurn(rc fwra.Context, spec ToolTurnSpec) (AssistantTurn, error) {
	if err := requireKey(rc.IdempotencyKey); err != nil {
		return AssistantTurn{}, err
	}
	if turn, done, err := w.replayTurn(rc.IdempotencyKey); done || err != nil {
		return turn, err
	}

	resp, err := w.client.GenerateWithTools(rc, fwllm.AnthropicToolRequest{
		Model:    resolveModel(w.classModels, w.defaultModel, spec.WorkerClass),
		System:   spec.System,
		Messages: toAnthropicMessages(spec.Messages),
		Tools:    toAnthropicTools(spec.Tools),
	})
	if err != nil {
		return AssistantTurn{}, err
	}

	turn := AssistantTurn{Text: strPtr(resp.Text), StopReason: resp.StopReason}
	for _, tu := range resp.ToolUses {
		turn.ToolCalls = append(turn.ToolCalls, ToolCall{Id: tu.ID, Name: tu.Name, Input: rawPtr(tu.Input)})
	}
	w.record(rc.IdempotencyKey, turn)
	return turn, nil
}

// toAnthropicTools / toAnthropicMessages map the generic worker tool envelopes to
// the provider-shaped infra types. InputSchema and tool inputs pass through as
// opaque bytes — the worker never interprets them.
func toAnthropicTools(tools []ToolSpec) []fwllm.AnthropicTool {
	out := make([]fwllm.AnthropicTool, 0, len(tools))
	for _, t := range tools {
		out = append(out, fwllm.AnthropicTool{Name: t.Name, Description: t.Description, InputSchema: rawVal(t.InputSchema), Strict: boolVal(t.Strict)})
	}
	return out
}

func toAnthropicMessages(msgs []Message) []fwllm.AnthropicMessage {
	out := make([]fwllm.AnthropicMessage, 0, len(msgs))
	for _, m := range msgs {
		am := fwllm.AnthropicMessage{Role: m.Role, Text: strVal(m.Text)}
		for _, tc := range m.ToolCalls {
			am.ToolUses = append(am.ToolUses, fwllm.AnthropicToolUse{ID: tc.Id, Name: tc.Name, Input: rawVal(tc.Input)})
		}
		for _, tr := range m.ToolResults {
			am.ToolResults = append(am.ToolResults, fwllm.AnthropicToolResult{ToolUseID: tr.ToolCallId, Content: tr.Content, IsError: boolVal(tr.IsError)})
		}
		out = append(out, am)
	}
	return out
}
