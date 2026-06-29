package worker

import (
	"encoding/json"
	"strings"
	"sync"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// idemStore is the in-process idempotency replay table shared by every concrete
// WorkerAccess implementation (OllamaWorker, AnthropicWorker). A retry carrying
// the same idempotencyKey replays the recorded result without re-invoking the
// expensive, non-deterministic Worker Provider (workerAccess.md §2f idempotency).
// Worker Providers expose no native request-id idempotency facility, so the access
// layer maintains this table itself.
//
// NOTE: this map grows unbounded — one entry per idempotency key, no eviction.
// Acceptable for the current single-workflow-server scope; a TTL/capacity cap is
// the production follow-up.
type idemStore struct {
	mu    sync.Mutex
	dedup map[fwra.IdempotencyKey]any
}

// newIdemStore builds an empty replay table. Concrete workers embed *idemStore so
// replay / record / replayResult promote onto the worker's method set.
func newIdemStore() *idemStore {
	return &idemStore{dedup: make(map[fwra.IdempotencyKey]any)}
}

// replay returns a recorded value for the key (idempotent replay), so a retry does
// not re-invoke the expensive Worker.
func (s *idemStore) replay(key fwra.IdempotencyKey) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.dedup[key]
	return v, ok
}

// record stores a result (a json.RawMessage or the cancelled marker) for replay.
func (s *idemStore) record(key fwra.IdempotencyKey, result any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dedup[key] = result
}

// replayResult inspects the dedup table for a prior outcome under key, collapsing
// the three replay cases into one decision a Generate can act on directly:
//
//	done=false                  → no recording; the caller must invoke the provider.
//	done=true, raw=nil, err=nil → a Cancel(key) preceded this; treat as cancelled.
//	done=true, raw=bytes        → a recorded response; return it without a provider call.
//	done=true, err!=nil         → the key holds an incompatible value (ContractMisuse).
//
// Shared by OllamaWorker and AnthropicWorker so the idempotency contract has one
// implementation across providers.
func (s *idemStore) replayResult(key fwra.IdempotencyKey) (raw json.RawMessage, done bool, err error) {
	prior, ok := s.replay(key)
	if !ok {
		return nil, false, nil
	}
	if _, cancelled := prior.(cancelledRun); cancelled {
		return nil, true, nil
	}
	res, isType := prior.(json.RawMessage)
	if !isType {
		return nil, true, fwra.New(fwra.ContractMisuse, "workerAccess: idempotency key recorded under an incompatible value")
	}
	return res, true, nil
}

// replayTurn is replayResult's counterpart for GenerateToolTurn: it returns a
// recorded AssistantTurn for the key (idempotent replay of a single tool turn).
//
//	done=false                       → no recording; invoke the provider.
//	done=true, turn=zero, err=nil     → a Cancel(key) preceded this; treat as cancelled.
//	done=true, turn=recorded          → return it without a provider call.
//	done=true, err!=nil               → key holds an incompatible value (ContractMisuse).
func (s *idemStore) replayTurn(key fwra.IdempotencyKey) (turn AssistantTurn, done bool, err error) {
	prior, ok := s.replay(key)
	if !ok {
		return AssistantTurn{}, false, nil
	}
	if _, cancelled := prior.(cancelledRun); cancelled {
		return AssistantTurn{}, true, nil
	}
	t, isType := prior.(AssistantTurn)
	if !isType {
		return AssistantTurn{}, true, fwra.New(fwra.ContractMisuse, "workerAccess: idempotency key recorded under an incompatible value")
	}
	return t, true, nil
}

// cancelledRun is the dedup marker recorded by Cancel for a cancelled key. A later
// Generate replay observes it as a nil message (the cancelled-replay path).
type cancelledRun struct{}

// requireKey enforces the non-empty idempotencyKey pre-condition shared by every
// verb (workerAccess.md §2.6) as fwra.ContractMisuse, caught before any
// infrastructure call.
func requireKey(key fwra.IdempotencyKey) error {
	if strings.TrimSpace(string(key)) == "" {
		return fwra.New(fwra.ContractMisuse, "workerAccess: empty idempotencyKey")
	}
	return nil
}

// resolveModel maps a logical WorkerClass to the concrete provider model that
// serves it (the encapsulated Worker volatility, workerAccess.md §3). An unknown
// or unmapped class falls back to defaultModel. Shared by every concrete worker.
func resolveModel(classModels map[WorkerClass]string, defaultModel string, class WorkerClass) string {
	if m, ok := classModels[class]; ok && m != "" {
		return m
	}
	return defaultModel
}

// copyClassModels defensively copies a caller-supplied WorkerClass→model map (nil
// is fine — every class then resolves to defaultModel).
func copyClassModels(classModels map[WorkerClass]string) map[WorkerClass]string {
	cm := make(map[WorkerClass]string, len(classModels))
	for k, v := range classModels {
		cm[k] = v
	}
	return cm
}

// --- pointer/value bridges for the generated `omitempty` contract fields --------
//
// The generated contract types (contract.gen.go) render the source `,omitempty`
// JSON tags as OPTIONAL fields → Go POINTERS (*string, *bool, *json.RawMessage).
// The provider infra types (fwllm.*) take plain values, so these tiny helpers
// bridge the two directions inside the concrete workers. They are NOT contract
// surface — pure within-package conversion utilities.

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func strVal(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func boolVal(p *bool) bool { return p != nil && *p }

func rawPtr(r json.RawMessage) *json.RawMessage {
	if len(r) == 0 {
		return nil
	}
	return &r
}

func rawVal(p *json.RawMessage) json.RawMessage {
	if p == nil {
		return nil
	}
	return *p
}
