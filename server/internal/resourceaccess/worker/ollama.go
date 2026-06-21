package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	fwllm "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-llm"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// OllamaWorker is the concrete WorkerAccess implementation backed by an Ollama
// HTTP endpoint — a real, locally-runnable LLM Worker Provider. It implements
// the generic typed worker surface (Generate + Cancel) of workerAccess.md §2f.
//
// The Ollama HTTP transport itself lives in the sanctioned llm infrastructure
// module (fwllm.Client); this struct owns the workerAccess DOMAIN behaviour on
// top of it — idempotent replay and the WorkerClass→model registry. Prompt
// assembly is NOT here — the fully-assembled prompt is the CALLER's (a Manager's
// sequence); the worker forwards it verbatim. Response deserialize is NOT here
// either — it is the mechanical GenerateTypedData[T] helper at the seam.
//
// Infrastructure-opacity (workerAccess.md §3f): the Ollama endpoint, model name,
// temperature/seed and token meters live ENTIRELY inside this struct (and the
// opaque client). None appear on the WorkerAccess surface. A vendor/model swap
// changes only this file; the contract is unchanged.
//
// Layer rules (workerAccess.md §5f, [[the-method-layers]]):
//   - imports NO Temporal — idempotencyKey is an ordinary parameter.
//   - imports NO Method-model types — no projectstate, no artifact.
//   - RA-never-calls-RA — calls NO sibling ResourceAccess.
type OllamaWorker struct {
	client *fwllm.Client

	// classModels maps a logical WorkerClass to the concrete provider model that
	// serves it. This IS the encapsulated Worker volatility (workerAccess.md §3):
	// the model name never crosses the contract surface. An unknown class falls
	// back to defaultModel (via resolveModel).
	classModels  map[WorkerClass]string
	defaultModel string

	// idemStore is the shared idempotency replay table (idempotency.go): a retry
	// carrying the same idempotencyKey replays the recorded response without
	// re-invoking the expensive, non-deterministic Worker (workerAccess.md §2f).
	*idemStore
}

// compile-time proof the concrete impl satisfies the port.
var _ WorkerAccess = (*OllamaWorker)(nil)

// NewOllamaWorker builds an OllamaWorker against an Ollama HTTP endpoint serving
// the given default model. classModels may override the model per WorkerClass (nil
// is fine — every class then uses defaultModel). The caller (production wiring /
// tests) owns the endpoint+model choice; the contract never sees it.
func NewOllamaWorker(baseURL, defaultModel string, classModels map[WorkerClass]string) (*OllamaWorker, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, fwra.New(fwra.ContractMisuse, "ollama worker: empty baseURL")
	}
	if strings.TrimSpace(defaultModel) == "" {
		return nil, fwra.New(fwra.ContractMisuse, "ollama worker: empty defaultModel")
	}
	return &OllamaWorker{
		// A generous client timeout; the Manager's Activity owns the real
		// StartToClose. Worker runs are slow.
		client:       fwllm.NewClient(baseURL, 5*time.Minute),
		classModels:  copyClassModels(classModels),
		defaultModel: defaultModel,
		idemStore:    newIdemStore(),
	}, nil
}

// Cancel abandons the in-flight run identified by idempotencyKey. Ollama
// exposes no durable re-attachable job to abort; cancellation here records a
// terminal cancelled marker against the key so a later Generate replay observes
// the cancellation as a nil message. An unknown / already-terminal run
// (fwra.NotFound semantics) is SUCCESS — the desired post-condition ("this run
// consumes no further Worker resource") already holds, which makes cancel safe
// to retry (workerAccess.md §2f.3).
func (w *OllamaWorker) Cancel(_ context.Context, idempotencyKey fwra.IdempotencyKey) error {
	if err := requireKey(idempotencyKey); err != nil {
		return err
	}
	w.record(idempotencyKey, cancelledRun{})
	return nil
}

// Generate is the generic typed worker surface (workerAccess.md §2f.1). It
// forwards the CALLER-assembled spec.Prompt to the provider (model chosen per
// spec.WorkerClass inside the seam), instructs the provider to emit JSON-format
// output (Format:"json"), records the raw response under the idempotencyKey, and
// returns it. The worker does NOT unmarshal — that is GenerateTypedData's job.
//
// Idempotency: a retry carrying the same key replays the recorded bytes without
// re-invoking the provider. A Cancel(key) followed by Generate(key) returns nil
// bytes with nil error (treated as cancelled by the caller).
func (w *OllamaWorker) Generate(ctx context.Context, spec GenerateSpec, idempotencyKey fwra.IdempotencyKey) (json.RawMessage, error) {
	if err := requireKey(idempotencyKey); err != nil {
		return nil, err
	}
	if strings.TrimSpace(spec.Prompt) == "" {
		return nil, fwra.New(fwra.ContractMisuse, "Generate: empty spec.Prompt")
	}
	if raw, done, err := w.replayResult(idempotencyKey); done || err != nil {
		// Recorded result (bytes), cancelled run (nil), or corrupt entry (err) —
		// return without re-invoking the provider.
		return raw, err
	}

	resp, err := w.generateJSON(ctx, spec.WorkerClass, spec.Prompt)
	if err != nil {
		return nil, err
	}

	result := json.RawMessage([]byte(resp.Response))
	w.record(idempotencyKey, result)
	return result, nil
}

// GenerateToolTurn runs one tool-calling /api/chat turn. It maps the generic
// ToolTurnSpec to the provider-shaped fwllm.ChatRequest and back. Ollama has no
// native tool-call ids, so this synthesizes positional ids (call_0, call_1, …) for
// the returned ToolCalls; Ollama correlates tool RESULTS by message order (each
// ToolResult becomes a role:"tool" message), so the synthesized id is caller-side
// bookkeeping only. Ollama returns no stop reason, so it is derived: tool calls
// present → "tool_use", otherwise "end_turn". Tool support varies by model; an
// unsupported model returns no tool calls (treated as end_turn).
//
// Idempotency mirrors Generate.
func (w *OllamaWorker) GenerateToolTurn(ctx context.Context, spec ToolTurnSpec, idempotencyKey fwra.IdempotencyKey) (AssistantTurn, error) {
	if err := requireKey(idempotencyKey); err != nil {
		return AssistantTurn{}, err
	}
	if turn, done, err := w.replayTurn(idempotencyKey); done || err != nil {
		return turn, err
	}

	resp, err := w.client.Chat(ctx, fwllm.ChatRequest{
		Model:    resolveModel(w.classModels, w.defaultModel, spec.WorkerClass),
		Messages: toOllamaChatMessages(spec.System, spec.Messages),
		Tools:    toOllamaChatTools(spec.Tools),
		Options:  fwllm.GenerateOptions{Temperature: 0, Seed: 42, NumPredict: 16000},
	})
	if err != nil {
		return AssistantTurn{}, err
	}

	turn := AssistantTurn{Text: resp.Content}
	for i, tc := range resp.ToolCalls {
		turn.ToolCalls = append(turn.ToolCalls, ToolCall{ID: fmt.Sprintf("call_%d", i), Name: tc.Name, Input: tc.Arguments})
	}
	if len(turn.ToolCalls) > 0 {
		turn.StopReason = "tool_use"
	} else {
		turn.StopReason = "end_turn"
	}
	w.record(idempotencyKey, turn)
	return turn, nil
}

// toOllamaChatTools / toOllamaChatMessages map the generic envelopes to the
// /api/chat wire shape. A System prompt becomes a leading role:"system" message;
// each ToolResult becomes a role:"tool" message (Ollama correlates by order).
func toOllamaChatTools(tools []ToolSpec) []fwllm.ChatTool {
	out := make([]fwllm.ChatTool, 0, len(tools))
	for _, t := range tools {
		out = append(out, fwllm.ChatTool{Name: t.Name, Description: t.Description, Parameters: t.InputSchema})
	}
	return out
}

func toOllamaChatMessages(system string, msgs []Message) []fwllm.ChatMessage {
	out := make([]fwllm.ChatMessage, 0, len(msgs)+1)
	if strings.TrimSpace(system) != "" {
		out = append(out, fwllm.ChatMessage{Role: "system", Content: system})
	}
	for _, m := range msgs {
		// A user turn carrying tool results maps to one role:"tool" message per
		// result (Ollama's tool-result shape), preceded by any free text.
		if len(m.ToolResults) > 0 {
			if m.Text != "" {
				out = append(out, fwllm.ChatMessage{Role: m.Role, Content: m.Text})
			}
			for _, tr := range m.ToolResults {
				out = append(out, fwllm.ChatMessage{Role: "tool", Content: tr.Content})
			}
			continue
		}
		cm := fwllm.ChatMessage{Role: m.Role, Content: m.Text}
		for _, tc := range m.ToolCalls {
			cm.ToolCalls = append(cm.ToolCalls, fwllm.ChatToolCall{Name: tc.Name, Arguments: tc.Input})
		}
		out = append(out, cm)
	}
	return out
}

// generateJSON resolves the WorkerClass to a concrete model and performs the
// blocking generate-and-collect call in JSON mode, used by the Generate verb. It
// sets Format:"json" on the request to instruct Ollama to constrain its output to
// valid JSON. The provider error is already a typed fwra.Error from the llm infra
// module (workerAccess.md §3f).
func (w *OllamaWorker) generateJSON(ctx context.Context, class WorkerClass, prompt string) (fwllm.GenerateResponse, error) {
	return w.client.Generate(ctx, fwllm.GenerateRequest{
		Model:  resolveModel(w.classModels, w.defaultModel, class),
		Prompt: prompt,
		Stream: false,
		Format: "json",
		Options: fwllm.GenerateOptions{
			Temperature: 0,
			Seed:        42,
			// Output-token ceiling. 1024 was too low: the larger Method artifacts
			// (volatilities, system, operationalConcepts) serialize to well over a
			// thousand JSON tokens and were truncated mid-object → "unexpected end of
			// JSON input" → a terminal WorkerRefused. Match the Anthropic worker's
			// 16000 ceiling so every artifact kind has room to complete.
			NumPredict: 16000,
		},
	})
}
