# aiarch server — developer make targets.

.PHONY: test test-short fmt vet lint method-check

# Everything, including the model-gated integration tests (need a live, capable
# Ollama-backed worker — see internal/manager/systemdesign/integration_test.go).
# These hang without a real model, so do NOT run this in CI.
test:
	go test ./...

# CI quality gate. `-short` makes the model-gated integration tests' TestMain
# skip (they require a live Ollama worker); everything else — including the
# fake-backed workflow gating tests — still runs.
test-short:
	go test -short ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

lint:
	golangci-lint run ./...

# Run the Method conformance gate (design rules + layer rules + alignment).
# Reads .aiarch/state/project.json from the repo root (one level up).
method-check:
	GOWORK=off go test ./internal/... -run TestMethod -v
