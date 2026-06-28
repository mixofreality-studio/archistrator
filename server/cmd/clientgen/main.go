// Command clientgen generates the archistrator server CLIENT layer
// (internal/client/{web,mcp}) 100% from project.json. It is the single
// reproducible driver behind `make gen-client`:
//
//   - reads the 5 web-exposed manager contracts from
//     .aiarch/state/project.json .serviceContracts (systemDesign, projectDesign,
//     project, construction, operations),
//   - runs the (fixed, standalone-green) framework-go-http-generator to emit per
//     manager REST handlers (internal/client/web/<pkg>/) plus the single
//     component-agnostic auth middleware + NewServer wiring (internal/client/web/),
//   - runs the framework-go-mcp-generator to emit per-manager MCP tools
//     (internal/client/mcp/<pkg>/), and
//   - merges every per-contract OpenAPI 3.1 document into the single
//     api/openapi.yaml (union of paths + components.schemas, conflicting shared
//     scalar defs are rejected).
//
// The generators emit TEXT and never compile the target — all manager/import
// wiring is parameterized here. Output is deterministic (contracts are visited in
// sorted order; yaml.v3 sorts map keys) so re-running is byte-idempotent.
//
// Usage:
//
//	cd server && make gen-client
//	cd server && go run ./cmd/clientgen ../.aiarch/state/project.json
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	httpcontract "github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator/contract"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator/httpgen"
	mcpcontract "github.com/mixofreality-studio/archistrator-platform/framework-go-mcp-generator/contract"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-mcp-generator/mcpgen"
)

// projectFile is the default path (relative to the server module root, where the
// gen targets run) to the head-state document that owns the contracts.
const projectFile = "../.aiarch/state/project.json"

// serverModule is the Go module path of the server, used to compose each
// manager's import path from its goPackage.
const serverModule = "github.com/mixofreality-studio/archistrator/server"

// Output roots, relative to the server module root.
const (
	webRoot = "internal/client/web"
	mcpRoot = "internal/client/mcp"
	oasPath = "api/openapi.yaml"
)

// exposedManagers is the set of web-wired manager contracts to generate the
// client layer for (the 5 managers mounted by the former hand-written web.go;
// settlement is intentionally excluded — it is not web-wired).
var exposedManagers = []string{
	"systemDesignManager",
	"projectDesignManager",
	"constructionManager",
	"operationsManager",
}

// contractEntry is the self-describing metadata on a .serviceContracts entry.
type contractEntry struct {
	Component string `json:"component"`
	Layer     string `json:"layer"`
	GoPackage string `json:"goPackage"`
}

func main() {
	path := projectFile
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		fatal("read %s: %v", path, err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		fatal("parse %s: %v", path, err)
	}
	var contracts map[string]json.RawMessage
	if err := json.Unmarshal(top["serviceContracts"], &contracts); err != nil {
		fatal("parse .serviceContracts in %s: %v", path, err)
	}

	// Deterministic order.
	keys := append([]string(nil), exposedManagers...)
	sort.Strings(keys)

	// Accumulate the per-contract OpenAPI docs for the final merge.
	oasDocs := make([]contractOAS, 0, len(keys))

	for _, key := range keys {
		entry, ok := contracts[key]
		if !ok {
			fatal("contract %q not found in .serviceContracts", key)
		}
		var meta contractEntry
		if err := json.Unmarshal(entry, &meta); err != nil {
			fatal("contract %q: parse metadata: %v", key, err)
		}
		if meta.GoPackage == "" {
			fatal("contract %q has no goPackage", key)
		}
		pkg := lastSegment(meta.GoPackage)
		managerImport := serverModule + "/" + meta.GoPackage

		// --- REST handlers + OpenAPI (http generator) ---
		hdoc, err := httpcontract.Parse(entry)
		if err != nil {
			fatal("contract %q: http parse: %v", key, err)
		}
		hres, err := httpgen.Generate(hdoc, httpgen.Options{
			Package:       pkg,
			ManagerImport: managerImport,
		})
		if err != nil {
			fatal("contract %q: httpgen: %v", key, err)
		}
		base := httpcontract.Kebab(hdoc.ManagerBase())
		webDir := filepath.Join(webRoot, pkg)
		mustMkdir(webDir)
		mustWrite(filepath.Join(webDir, base+"_handlers.gen.go"), hres.HandlersGo)

		var oasDoc map[string]any
		if err := yaml.Unmarshal(hres.OpenAPIYAML, &oasDoc); err != nil {
			fatal("contract %q: decode generated OpenAPI: %v", key, err)
		}
		oasDocs = append(oasDocs, contractOAS{prefix: hdoc.ManagerBase(), doc: oasDoc})

		// --- MCP tools (mcp generator) ---
		mdoc, err := mcpcontract.Parse(entry)
		if err != nil {
			fatal("contract %q: mcp parse: %v", key, err)
		}
		mres, err := mcpgen.Generate(mdoc, mcpgen.Options{
			Package:       pkg,
			ManagerImport: managerImport,
		})
		if err != nil {
			fatal("contract %q: mcpgen: %v", key, err)
		}
		mcpDir := filepath.Join(mcpRoot, pkg)
		mustMkdir(mcpDir)
		mustWrite(filepath.Join(mcpDir, base+"_tools.gen.go"), mres.ToolsGo)

		fmt.Printf("generated client layer for %s (pkg %s)\n", key, pkg)
	}

	// --- component-agnostic wiring (auth middleware + NewServer), once ---
	wres, err := httpgen.GenerateWiring(httpgen.WiringOptions{Package: "web"})
	if err != nil {
		fatal("httpgen wiring: %v", err)
	}
	mustWrite(filepath.Join(webRoot, "middleware.gen.go"), wres.MiddlewareGo)
	mustWrite(filepath.Join(webRoot, "server.gen.go"), wres.ServerGo)
	fmt.Printf("generated wiring layer (%s/{middleware,server}.gen.go)\n", webRoot)

	// --- merge every per-contract OpenAPI doc into the single api/openapi.yaml ---
	merged, err := mergeOpenAPI(oasDocs)
	if err != nil {
		fatal("merge openapi: %v", err)
	}
	out, err := yaml.Marshal(merged)
	if err != nil {
		fatal("marshal merged openapi: %v", err)
	}
	mustMkdir(filepath.Dir(oasPath))
	mustWrite(oasPath, out)
	fmt.Printf("merged OpenAPI -> %s\n", oasPath)
}

// contractOAS pairs a per-contract OpenAPI document with its manager-base prefix
// (e.g. "SystemDesign", "Project"), used to namespace that contract's schemas in
// the merged document.
type contractOAS struct {
	prefix string
	doc    map[string]any
}

// mergeOpenAPI unions the paths and components.schemas of every per-contract OAS
// 3.1 document into one.
//
// The 5 manager contracts are INDEPENDENT, self-contained documents: several
// define a same-named type with a genuinely different shape (e.g. SessionStage is
// an 8-value enum for systemDesign but a 9-value enum for projectDesign;
// ResearchSource uses camelCase JSON keys in systemDesign but PascalCase in
// project; ReviewFeedback carries comments in systemDesign but not in
// projectDesign). OAS components.schemas is a single flat namespace, so a naive
// union would silently collapse these distinct types. Because each contract's
// schema graph is self-contained (no $ref crosses a contract boundary), the sound
// merge namespaces every schema by its owning manager (e.g. SystemDesignSessionStage,
// ProjectDesignSessionStage) and rewrites that contract's $refs to match — the
// document then carries every component's true contract with zero collisions.
func mergeOpenAPI(docs []contractOAS) (map[string]any, error) {
	paths := map[string]any{}
	schemas := map[string]any{}

	for _, c := range docs {
		// Namespace this contract's schema graph in place: prefix every schema
		// name and rewrite every #/components/schemas/<name> $ref to the prefixed
		// name across the whole document (paths + schemas).
		prefixRefs(c.doc, c.prefix)

		if p, ok := c.doc["paths"].(map[string]any); ok {
			for route, ops := range p {
				if _, ok := paths[route]; ok {
					return nil, fmt.Errorf("duplicate route %q across contracts", route)
				}
				paths[route] = ops
			}
		}
		comps, ok := c.doc["components"].(map[string]any)
		if !ok {
			continue
		}
		sch, ok := comps["schemas"].(map[string]any)
		if !ok {
			continue
		}
		for name, def := range sch {
			if existing, ok := schemas[name]; ok {
				// With per-contract prefixing this cannot collide unless two
				// contracts share a prefix; guard anyway.
				if !reflect.DeepEqual(existing, def) {
					return nil, fmt.Errorf("conflicting definitions for namespaced schema %q", name)
				}
				continue
			}
			schemas[name] = def
		}
	}

	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   "archistrator API",
			"version": "1.0.0",
		},
		"paths": paths,
		"components": map[string]any{
			"schemas": schemas,
		},
	}, nil
}

// schemaRefPrefix is the JSON-pointer prefix every component schema $ref carries.
const schemaRefPrefix = "#/components/schemas/"

// prefixRefs namespaces a single contract's OpenAPI document in place: it renames
// every components.schemas key to prefix+name and rewrites every
// "#/components/schemas/<name>" $ref (anywhere in the document) to
// "#/components/schemas/<prefix><name>". Safe because a contract's schema graph
// never $refs outside itself.
func prefixRefs(doc map[string]any, prefix string) {
	rewriteRefStrings(doc, prefix)
	comps, ok := doc["components"].(map[string]any)
	if !ok {
		return
	}
	sch, ok := comps["schemas"].(map[string]any)
	if !ok {
		return
	}
	renamed := make(map[string]any, len(sch))
	for name, def := range sch {
		renamed[prefix+name] = def
	}
	comps["schemas"] = renamed
}

// rewriteRefStrings walks an arbitrary decoded YAML/JSON value and rewrites every
// {"$ref": "#/components/schemas/X"} to point at prefix+X.
func rewriteRefStrings(node any, prefix string) {
	switch v := node.(type) {
	case map[string]any:
		for k, val := range v {
			if k == "$ref" {
				if s, ok := val.(string); ok && strings.HasPrefix(s, schemaRefPrefix) {
					v[k] = schemaRefPrefix + prefix + strings.TrimPrefix(s, schemaRefPrefix)
					continue
				}
			}
			rewriteRefStrings(val, prefix)
		}
	case []any:
		for _, item := range v {
			rewriteRefStrings(item, prefix)
		}
	}
}

func lastSegment(goPackage string) string {
	i := strings.LastIndex(goPackage, "/")
	return goPackage[i+1:]
}

func mustMkdir(dir string) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fatal("mkdir %s: %v", dir, err)
	}
}

func mustWrite(path string, data []byte) {
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fatal("write %s: %v", path, err)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "clientgen: "+format+"\n", args...)
	os.Exit(1)
}
