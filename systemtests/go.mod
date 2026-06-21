// The archistrator SYSTEM-TEST HARNESS — a SEPARATE Go module, sibling to
// ../server, never under it. Because it is outside the server's package tree,
// Go's internal/ rule compiler-seals github.com/mixofreality-studio/archistrator/
// server/internal/... against import here: any such import is a `go build`
// error, not a lint. The harness drives the running server ONLY over its
// published Client surfaces (HTTP today, MCP once mcpClient is built) and links
// ZERO server code. See [[the-method-testing]] §7 (the test-authoring
// constitution) and designs/aiarch/system/operational-concepts.md §17.
//
// Dependencies are deliberately EMPTY: stdlib only. No google/uuid (we format a
// v4 UUID from crypto/rand), no testinfra (the stack is provisioned externally —
// docker-compose.yaml / CI services — and passed via ARCHISTRATOR_* env). The
// smaller the import graph, the smaller the cheating surface — enforced by the
// depguard allowlist (.golangci.yml) and the constitution test (constitution/).
module github.com/mixofreality-studio/archistrator/systemtests

go 1.25.4
