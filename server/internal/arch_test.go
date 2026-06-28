package internal_test

import (
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/arch"
)

// modulePrefix is the import-path prefix for this module's internal packages.
const modulePrefix = "github.com/mixofreality-studio/archistrator/server/internal/"

// TestMethodLayering enforces The Method's layer model on the archistrator server's
// internal packages: strictly closed downward imports, Temporal only in the
// manager layer, Engine/ResourceAccess ports named *Engine/*Access with error-
// returning methods, and the operator-curated dependency allowlist.
//
// Layer layout (top→bottom):
//   - manager/systemdesign                       — Manager façade; OWNS Temporal.
//     The Manager's SEQUENCE owns the Phase-1 prompt corpus (2026-05-29 re-cut);
//     drafting AND PM-critique are the GENERIC workerAccess.GenerateTypedData[T]
//     wrapped in a Manager-owned Activity, so the worker I/O stays out of replayed
//     workflow code.
//   - engine/{estimation,operationestimation,settlement}
//     — Engine ports. The estimate Engines are pure deterministic Phase-2 computation.
//     (The former systemdesign Engine was WITHDRAWN by the 2026-05-29 re-cut —
//     drafting a Method artifact via an LLM is the Manager's activity, not an
//     Engine. Server-side rendering was likewise removed — the client renders
//     typed models. The artifactValidation Engine was REMOVED 2026-06-16 — the
//     Method gate moved to framework-go/methodcheck, the seated `go test` in the
//     user repo; the Managers retained only the small Finding value type, relocated
//     locally to systemdesign/projectdesign.)
//   - resourceaccess/{projectstate,artifact,worker}
//     — ResourceAccess components, each fronting a single Resource. NO RA→RA
//     imports: worker is the GENERIC typed LLM worker (Generate raw JSON +
//     package-level GenerateTypedData[T] + Cancel) with NO Method-model knowledge —
//     it imports neither projectstate nor artifact; the typed Method models live
//     in projectstate and the construction value types live in artifact; only
//     Engines and the Manager import those.
//
// The Method has NO "domain" layer — only Clients/Managers/Engines/
// ResourceAccess/Resources/Utilities. Shared typed Method models are owned by
// the RA that fronts them (projectstate); downstream Engines and the Manager
// import the owning RA's package as a normal downward edge.
func TestMethodLayering(t *testing.T) {
	spec := arch.MethodSpec(
		"..", // module root relative to internal/
		modulePrefix,
	)
	// There is no sideways escape hatch to configure. The typed-models rework
	// eliminated every sideways RA→RA import: worker is the generic typed LLM
	// worker that imports neither projectstate nor artifact; the Engines'
	// projectstate import is a legal Engine→RA downward edge. MethodSpec
	// makes every layer NoSideways and requires every internal package to
	// classify into client/manager/engine/resourceaccess, so an out-of-band
	// package (e.g. a "domain" dir) fails the build outright.

	// Dependency allowlist — the FIXED, operator-curated infrastructure menu for
	// any system built with archistrator (the CustomerAppInfrastructure volatility made
	// executable). Production code may import only:
	//   - the Go standard library (auto-allowed; not listed),
	//   - the mixofreality-studio archistrator-platform framework family + this app's own module,
	//   - the sanctioned infrastructure drivers carried by the framework-go
	//     infrastructure modules: Postgres (pgx), Git/Gitea (go-git), and the
	//     durable-execution substrate (Temporal).
	// An unsanctioned dependency (e.g. a MongoDB driver) fails this test. Only an
	// archistrator operator may widen the menu by adding a prefix here; that is the one
	// and only place the menu is defined (no parallel doc to drift out of sync).
	// Test-only deps (testcontainers, the Gitea SDK) are NOT scanned — Check loads
	// with Tests:false — so they need no entry.
	// Temporal-isolation exemption — the single architecturally-sanctioned
	// exception to "Temporal lives only in the Manager layer". durableExecutionAccess
	// is the ResourceAccess whose fronted Resource IS the durable-execution substrate
	// (Temporal) itself — "the architecturally hardest case in the corpus"
	// (durableExecutionAccess.md §1). Its concrete adapter (temporal.go) MUST speak
	// the Temporal control-plane SDK, exactly as projectstate's adapter speaks pgx
	// and artifact's speaks go-git. The exemption relaxes ONLY the Temporal-isolation
	// rule for this one package; it remains subject to classification, downward-only
	// imports, no-sideways, the Access port + error returns, and the dependency
	// allowlist. The CONTRACT surface (the DurableExecutionAccess port + value types
	// in durableexecution.go) stays Temporal-free by the component's own design and
	// review — the Temporal SDK is confined to temporal.go. This list is the one and
	// only place the exception is granted; any OTHER RA/Engine/Utility importing
	// Temporal still fails the build.
	spec.TemporalExemptPackages = []string{"resourceaccess/durableexecution"}

	spec.AllowedImportPrefixes = []string{
		"github.com/mixofreality-studio/", // archistrator-platform framework family + this app's own module
		"github.com/google/uuid",          // sanctioned identity type (projectstate.ProjectID = uuid.UUID)
		"github.com/invopop/jsonschema",   // typed-output JSON Schema derivation for LLM prompts (manager/systemdesign/schema.go)
		"github.com/jackc/pgx",            // sanctioned Postgres driver
		"github.com/go-git/",              // sanctioned Git/Gitea client (go-git + go-billy)
		"go.temporal.io/",                 // sanctioned durable-execution substrate
		"github.com/modelcontextprotocol/go-sdk", // sanctioned MCP substrate for the generated mcpClient tool surface (internal/client/mcp/*, framework-go-mcp-generator output)
	}
	arch.Check(t, spec)
}
