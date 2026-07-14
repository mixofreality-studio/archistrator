# MCP Apps Pilot Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make archistrator's MCP server serve the System-Design screen as an MCP App (spec: `docs/superpowers/specs/2026-07-13-mcp-apps-design.md`), building the whole generic seam (OpsClient, lint DAG, ui:// stub resource, mcpemit `_meta.ui`) with one pilot view wired end-to-end.

**Architecture:** Components go prop-driven-pure; a generated transport-agnostic `OpsClient` (REST impl for SPA, `callServerTool` impl for MCP) feeds shared TanStack hooks; the Go server serves a constant HTML stub as `ui://archistrator/shell.html` whose assets load from the webApp origin; `mcpemit` stamps `_meta.ui.resourceUri` on ops annotated in project.json.

**Tech Stack:** Go (go-sdk v1.6.1, already vendored), React 19 + Vite 7 + TanStack Query, `@modelcontextprotocol/ext-apps` (new dep, webApp only), eslint-plugin-boundaries (vendored platform config).

## Global Constraints

- Resource mimetype: `text/html;profile=mcp-app` (exact).
- Tool meta key: nested `_meta.ui.resourceUri` (NOT deprecated `"ui/resourceUri"`).
- CSP: `_meta.ui.csp.resourceDomains = [<webApp origin>]` only; `connectDomains` stays absent/empty.
- Shell resource URI: `ui://archistrator/shell.html`. Pilot view id: `system-design-session`.
- Tool naming (existing): `<manager><PascalOp>`, e.g. `systemDesignGetSessionState`. REST paths (existing): `/api/v1/<kebab-manager>/<kebab-op>/...`.
- UI annotation goes on state-read ops only. Pilot: `systemDesignGetSessionState` only.
- No `go:embed` of web assets; the stub is a config-templated constant string. Server env: `GOWORK=off` for all go commands.
- MCP Vite entry emits FIXED names `mcp-app.js` / `mcp-app.css` (no content hashes) as a **classic IIFE** (not ESM); the stub references them with plain `<script src>` / `<link>` tags (no `type="module"`, no `crossorigin`) — this keeps asset delivery CORS-exempt, and NO CORS config exists in production anywhere.
- Never edit `*.gen.go` / `*.gen.ts` by hand; change the generator and re-run.
- No `dangerouslySetInnerHTML`, no `rehype-raw` — lint-enforced from Task 6 on.
- In MCP context there is no background polling (`refetchInterval` off) and no `/api/userinfo` probe.
- Commit after every task; run the repo gates before each commit (`server`: `GOWORK=off go test ./...`; `webApp`: `npm run lint && npm run build`).

---

### Task 1: `ui` annotation on service-contract operations

**Files:**
- Modify: `.aiarch/state/project.json` (systemDesignManager → interface.operations → GetSessionState)
- Modify: the Go struct(s) that decode `serviceContracts` operations (located in step 1)

**Interfaces:**
- Produces: op entries may carry `"ui": {"view": "<view-id>"}`; Go side exposes `Op.UI *OpUI` with `OpUI{View string}` to Task 2.

- [ ] **Step 1: Locate every decoder of serviceContracts operations**

Run: `cd server && grep -rn "Operations \[\]" cmd/ internal/ --include="*.go" | grep -iv test`
Expected: the clientgen contract structs (under `cmd/clientgen/`) and possibly the projectstate model. For each hit, check whether decoding uses `DisallowUnknownFields` (`grep -rn "DisallowUnknownFields" <those dirs>`). If nothing strict-decodes, only clientgen's struct needs the new field; if the canonical projectstate model round-trips serviceContracts, add the field there too (same shape) so `aiarch-state-mcp validate`/`reconcile` don't drop or reject it.

- [ ] **Step 2: Add the optional field to the op struct(s)**

```go
// alongside the existing operation struct fields (Name, Params, Result, Error):

// UI optionally marks this operation as rendering an MCP App view.
// Ops without it stay plain tools. See docs/superpowers/specs/2026-07-13-mcp-apps-design.md §3.5.
UI *OpUI `json:"ui,omitempty"`
```
```go
// OpUI is the MCP-Apps view annotation on a service-contract operation.
type OpUI struct {
	// View is the webApp view-registry id rendered when this op's tool fires.
	View string `json:"view"`
}
```

- [ ] **Step 3: Annotate the pilot op in project.json**

In `.aiarch/state/project.json`, find `"name": "GetSessionState"` inside `serviceContracts.systemDesignManager.interface.operations` and add the sibling field:

```json
"ui": { "view": "system-design-session" }
```

- [ ] **Step 4: Verify decode + validate**

Run: `cd server && GOWORK=off go build ./... && GOWORK=off go run ./cmd/aiarch-state-mcp validate`
Expected: build OK; validate passes (or, if validate rejects the unknown field, extend the canonical model per step 1 and re-run until green).

- [ ] **Step 5: Commit**

```bash
git add .aiarch/state/project.json server/
git commit -m "feat(contracts): optional ui view annotation on service-contract ops (pilot: systemDesignGetSessionState)"
```

---

### Task 2: mcpemit stamps `_meta.ui.resourceUri`

**Files:**
- Modify: `server/cmd/clientgen/internal/mcpemit/mcpemit.go` (AddTool emission, ~line 187)
- Regenerate: `server/internal/client/mcp/systemdesign/system-design_tools.gen.go` (via `make gen-client`)

**Interfaces:**
- Consumes: `Op.UI *OpUI` from Task 1.
- Produces: generated `mcp.AddTool(..., &mcp.Tool{..., Meta: mcp.Meta{"ui": map[string]any{"resourceUri": shellResourceURI}}}, ...)` for annotated ops; emits `const shellResourceURI = "ui://archistrator/shell.html"` once per generated file that needs it.

- [ ] **Step 1: Extend the emitter**

In `mcpemit.go`, where the AddTool line is emitted (the `fmt.Fprintf(&b, "\tmcp.AddTool(...` at ~187), branch on the op's annotation:

```go
if op.UI != nil {
	fmt.Fprintf(&b, "\tmcp.AddTool(srv, &mcp.Tool{Name: %q, Description: %q, InputSchema: %sInputSchema(), OutputSchema: %sOutputSchema(), Meta: mcp.Meta{\"ui\": map[string]any{\"resourceUri\": shellResourceURI}}}, h.handle%s)\n",
		toolName, desc, lcName, lcName, op.Name)
} else {
	// existing emission line unchanged
}
```

And, in the file-header emission of any package containing ≥1 annotated op, emit once:

```go
// shellResourceURI is the single MCP-Apps UI resource all view-bearing tools reference.
const shellResourceURI = "ui://archistrator/shell.html"
```

- [ ] **Step 2: Regenerate and inspect**

Run: `cd server && make gen-client && grep -n "Meta: mcp.Meta" internal/client/mcp/systemdesign/system-design_tools.gen.go`
Expected: exactly one hit, on the `systemDesignGetSessionState` AddTool line; `git diff --stat` shows only systemdesign regenerated (other managers unchanged — no annotated ops).

- [ ] **Step 3: Gates + commit**

Run: `cd server && GOWORK=off go test ./...`
Expected: PASS (arch checker still sees SDK imports only in generated files).

```bash
git add server/
git commit -m "feat(mcpemit): stamp _meta.ui.resourceUri on ui-annotated ops"
```

---

### Task 3: `ui://` shell resource + webApp-origin config

**Files:**
- Create: `server/cmd/server/mcp_apps.go`
- Modify: `server/cmd/server/mcp_mount.go` (register resource in `newMCPServer`)
- Modify: `server/cmd/server/hooks.go` (thread the two new config values)
- Test: `server/cmd/server/mcp_apps_test.go`

**Interfaces:**
- Produces: `registerShellResource(srv *mcp.Server, webAppOrigin, assetVersion string)`; config keys `WEBAPP_ORIGIN` (e.g. `https://app.example.com`; dev default `http://localhost:5173`) and `WEBAPP_ASSET_VERSION` (optional cache-buster, default `dev`).

- [ ] **Step 1: Find the config mechanism**

Run: `cd server && grep -n "ExtraMounts\|os.Getenv\|Config" cmd/server/hooks.go | head -20`
Expected: shows how `newMCPHandler` receives its inputs today and whether hooks read env directly or a generated config struct. Follow that exact pattern for the two new values (if configgen owns config, add the fields there; if hooks read env, read env).

- [ ] **Step 2: Write the failing test**

```go
package main

import (
	"strings"
	"testing"
)

func TestShellStubHTML(t *testing.T) {
	got := shellStubHTML("https://app.example.com", "42")
	if strings.Contains(got, "module") || strings.Contains(got, "crossorigin") {
		t.Error("stub must use classic CORS-exempt tags (no module/crossorigin)")
	}
	for _, want := range []string{
		`<!DOCTYPE html>`,
		// CLASSIC tags — no type="module", no crossorigin: CORS-exempt (spec §3.4)
		`<script src="https://app.example.com/mcp-app.js?v=42"></script>`,
		`<link rel="stylesheet" href="https://app.example.com/mcp-app.css?v=42">`,
		`<div id="root"></div>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("stub missing %q\n---\n%s", want, got)
		}
	}
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `cd server && GOWORK=off go test ./cmd/server/ -run TestShellStubHTML`
Expected: FAIL — `undefined: shellStubHTML`

- [ ] **Step 4: Implement `mcp_apps.go`**

```go
// MCP Apps support: serves the single ui:// shell resource. Composition-root
// glue like mcp_mount.go — lives outside internal/, may import the SDK.
// The stub is a constant, config-templated string: assets live on the webApp
// origin (nginx); this server does no asset fetching or caching (spec §3.4/§3.5).
package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	shellResourceURI = "ui://archistrator/shell.html"
	shellMIMEType    = "text/html;profile=mcp-app"
)

func shellStubHTML(webAppOrigin, assetVersion string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>archistrator</title>
<link rel="stylesheet" href="%[1]s/mcp-app.css?v=%[2]s">
</head>
<body>
<div id="root"></div>
<script src="%[1]s/mcp-app.js?v=%[2]s"></script>
</body>
</html>`, webAppOrigin, assetVersion)
}

func registerShellResource(srv *mcp.Server, webAppOrigin, assetVersion string) {
	srv.AddResource(&mcp.Resource{
		URI:      shellResourceURI,
		Name:     "archistrator-shell",
		MIMEType: shellMIMEType,
		Meta: mcp.Meta{"ui": map[string]any{
			"csp":           map[string]any{"resourceDomains": []string{webAppOrigin}},
			"prefersBorder": true,
		}},
	}, func(_ context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
			URI:      shellResourceURI,
			MIMEType: shellMIMEType,
			Text:     shellStubHTML(webAppOrigin, assetVersion),
		}}}, nil
	})
}
```

(If `mcp.ResourceContents` field names differ in v1.6.1, check `$(go env GOMODCACHE)/github.com/modelcontextprotocol/go-sdk@v1.6.1/mcp/protocol.go` for the exact struct and adjust — `URI`/`MIMEType`/`Text` are the expected names.)

- [ ] **Step 5: Register it in `newMCPServer` and thread config**

In `mcp_mount.go`, add params `webAppOrigin, assetVersion string` to `newMCPServer` and `newMCPHandler`, call `registerShellResource(srv, webAppOrigin, assetVersion)` after the four Handler registrations, and pass the values from `hooks.go` per the step-1 pattern.

- [ ] **Step 6: Tests pass + commit**

Run: `cd server && GOWORK=off go test ./...`
Expected: PASS.

```bash
git add server/
git commit -m "feat(server): serve ui://archistrator/shell.html MCP-Apps stub resource"
```

---

### Task 4: dev-profile CORS on `/mcp`

**Files:**
- Modify: `server/cmd/server/mcp_mount.go` (wrap handler when dev mode enabled)
- Test: `server/cmd/server/mcp_apps_test.go` (append)

**Interfaces:**
- Consumes: `web.DevConfig.Enabled` (already a `newMCPHandler` param).

- [ ] **Step 1: Failing test**

```go
func TestDevCORSPreflight(t *testing.T) {
	h := devCORS(true, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	req := httptest.NewRequest("OPTIONS", "/mcp", nil)
	req.Header.Set("Origin", "http://localhost:8080")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("missing ACAO header: %v", rr.Header())
	}
	// prod profile: no header
	h = devCORS(false, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("prod must not set CORS headers")
	}
}
```

Run: `GOWORK=off go test ./cmd/server/ -run TestDevCORSPreflight` — Expected: FAIL `undefined: devCORS`.

- [ ] **Step 2: Implement**

```go
// devCORS permits browser-based MCP hosts (the ext-apps basic-host pilot
// harness) to call /mcp cross-origin. DEV ONLY: production hosts call
// server-to-server; enabled strictly rides the dev-mode auth flag.
func devCORS(enabled bool, next http.Handler) http.Handler {
	if !enabled {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Mcp-Session-Id, Mcp-Protocol-Version")
		w.Header().Set("Access-Control-Expose-Headers", "Mcp-Session-Id")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

Wrap in `newMCPHandler`: `return devCORS(dev.Enabled, web.AuthMiddleware(dev, validator)(transport))`.

- [ ] **Step 3: Test + commit**

Run: `cd server && GOWORK=off go test ./...` — Expected: PASS.

```bash
git add server/ && git commit -m "feat(server): dev-profile CORS on /mcp for browser-based pilot hosts"
```

---

### Task 5: generated OpsClient (interface + REST + MCP impls)

**Files:**
- Create: `webApp/scripts/gen-ops.mjs`
- Create (generated): `webApp/src/api/ops.gen.ts`
- Create: `webApp/src/api/opsContext.tsx`
- Modify: `webApp/package.json` (script `gen:ops`, dep `@modelcontextprotocol/ext-apps`)
- Test: `webApp/src/api/ops.test.ts`

**Interfaces:**
- Produces (consumed by Tasks 7/9):
  - `interface OpsClient { call<P, R>(op: OpId, params: P): Promise<R> }` with `type OpId = 'systemDesignGetSessionState' | ...` (full union generated from openapi paths);
  - `restOpsClient(apiClient): OpsClient`, `mcpOpsClient(app: App): OpsClient`;
  - `OpsClientProvider` / `useOpsClient(): { ops: OpsClient; transport: 'rest' | 'mcp' }` from `opsContext.tsx`.

- [ ] **Step 1: Locate the OpenAPI source `gen:api` reads**

Run: `sed -n '1,30p' webApp/scripts/gen-api.mjs`
Expected: the openapi file path (likely `../server/api/...` or similar) and codegen style to mirror. Use the same source in `gen-ops.mjs`.

- [ ] **Step 2: Write the failing test**

```ts
import { describe, expect, it, vi } from 'vitest';
import { OP_BINDINGS, restOpsClient, mcpOpsClient } from './ops.gen';

describe('generated ops', () => {
  it('binds systemDesignGetSessionState to REST path and tool name', () => {
    const b = OP_BINDINGS.systemDesignGetSessionState;
    expect(b.method).toBe('GET');
    expect(b.path).toBe('/api/v1/system-design/get-session-state/{projectID}');
    expect(b.tool).toBe('systemDesignGetSessionState');
  });

  it('mcp impl routes through callServerTool and unwraps structuredContent', async () => {
    const app = { callServerTool: vi.fn().mockResolvedValue({ structuredContent: { stage: 'drafting' }, content: [] }) };
    const ops = mcpOpsClient(app as never);
    const out = await ops.call('systemDesignGetSessionState', { path: { projectID: 'p1' }, query: { kind: 1 } });
    expect(app.callServerTool).toHaveBeenCalledWith({ name: 'systemDesignGetSessionState', arguments: { projectID: 'p1', kind: 1 } });
    expect(out).toEqual({ stage: 'drafting' });
  });
});
```

Run: `cd webApp && npx vitest run src/api/ops.test.ts` — Expected: FAIL (module not found).

- [ ] **Step 3: Write `gen-ops.mjs`**

Mechanics (mirroring gen-api.mjs style): parse the OpenAPI YAML paths; for each `/api/v1/<mgr>/<op>[/...]`, derive `opId = camel(mgr) + Pascal(op)`, record `{method, path, tool: opId}`; emit `src/api/ops.gen.ts` containing `OP_BINDINGS`, the `OpId` union, and two factory functions:

```js
// core of the emitted file (template inside gen-ops.mjs):
const emitted = `// Code generated by scripts/gen-ops.mjs. DO NOT EDIT.
import type createClient from 'openapi-fetch';
import type { paths } from '../contracts/schema';
import type { App } from '@modelcontextprotocol/ext-apps';

export const OP_BINDINGS = ${JSON.stringify(bindings, null, 2)} as const;
export type OpId = keyof typeof OP_BINDINGS;

export interface OpsClient {
  call(op: OpId, params: { path?: Record<string, string>; query?: Record<string, unknown>; body?: unknown }): Promise<unknown>;
}

export function restOpsClient(client: ReturnType<typeof createClient<paths>>): OpsClient {
  return {
    async call(op, params) {
      const b = OP_BINDINGS[op];
      const fn = (client as never)[b.method] as (p: string, o: unknown) => Promise<{ data?: unknown; error?: unknown; response: Response }>;
      const { data, error, response } = await fn(b.path, { params: { path: params.path, query: params.query }, body: params.body });
      if (error !== undefined) throw Object.assign(new Error('api error'), { status: response.status, body: error });
      return data;
    },
  };
}

export function mcpOpsClient(app: App): OpsClient {
  return {
    async call(op, params) {
      const b = OP_BINDINGS[op];
      const args = { ...(params.path ?? {}), ...(params.query ?? {}), ...((params.body as object) ?? {}) };
      const result = await app.callServerTool({ name: b.tool, arguments: args });
      if (result.isError) throw Object.assign(new Error('tool error'), { status: 500, body: result.content });
      return result.structuredContent;
    },
  };
}
`;
```

(Error objects: reuse `toApiError`/`ApiError` from `src/contracts/errors` instead of bare `Object.assign` if its constructor fits — check `src/contracts/errors.ts` and prefer it so hooks' `retry`/404 logic keeps working across both transports. MCP-side 404-equivalent: tool errors carrying a not-found marker in content — map to `ApiError(404)` when detected.)

- [ ] **Step 4: Provider context (`src/api/opsContext.tsx`)**

```tsx
import { createContext, useContext, type ReactNode } from 'react';
import type { OpsClient } from './ops.gen';

interface OpsCtx { ops: OpsClient; transport: 'rest' | 'mcp' }
const Ctx = createContext<OpsCtx | null>(null);

export function OpsClientProvider({ value, children }: { value: OpsCtx; children: ReactNode }) {
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useOpsClient(): OpsCtx {
  const v = useContext(Ctx);
  if (!v) throw new Error('OpsClientProvider missing');
  return v;
}
```

- [ ] **Step 5: Wire scripts + install dep**

```bash
cd webApp && npm install @modelcontextprotocol/ext-apps
```
`package.json`: `"gen:ops": "node scripts/gen-ops.mjs"`, and chain it in `prebuild` after `gen:api`.

- [ ] **Step 6: Generate, test, commit**

Run: `cd webApp && npm run gen:ops && npx vitest run src/api/ops.test.ts && npm run lint && npx tsc -b`
Expected: PASS.

```bash
git add webApp/ && git commit -m "feat(webapp): generated OpsClient — REST + MCP tool-call transports over one interface"
```

---

### Task 6: lint DAG — `containers` element, pure `components`, XSS bans

**Files:**
- Modify: `webApp/eslint.platform.config.js` (vendored platform config — mirror the change into the platform package repo as a follow-up earmark)
- Create: `webApp/src/containers/` (empty `.gitkeep` or first container in Task 8)

**Interfaces:**
- Produces: lint DAG `routes → containers → (components | hooks) → api`; `components` may import only `components|contracts|utilities`; `mcpShell` element allowed to import `containers|api|contracts|utilities`.

- [ ] **Step 1: Extend BOUNDARY_RULES (config lines ~43–50)**

Add element types `containers` (pattern `src/containers/**`) and `mcpShell` (pattern `src/mcpShell/**`) and rewrite the allow-matrix:

```js
// routes:    ['routes', 'containers', 'components', 'hooks', 'contracts', 'utilities']  // transitional: components/hooks until migration completes
// containers:['containers', 'components', 'hooks', 'contracts', 'utilities']
// mcpShell:  ['mcpShell', 'containers', 'api', 'contracts', 'utilities']
// components:['components', 'contracts', 'utilities']            // ← hooks/api REMOVED
// hooks:     ['hooks', 'api', 'contracts', 'utilities']
// api:       ['api', 'contracts', 'utilities']
```

- [ ] **Step 2: XSS + raw-fetch bans**

In the same config, add to the shared rules:

```js
'no-restricted-properties': ['error', {
  object: undefined, property: 'dangerouslySetInnerHTML',
  message: 'Raw HTML rendering is banned (MCP Apps XSS control — spec §8.2).',
}],
'no-restricted-imports': ['error', { paths: [{ name: 'rehype-raw', message: 'Raw HTML in markdown is banned (spec §8.2).' }] }],
'no-restricted-globals': ['error', { name: 'fetch', message: 'Network IO only via the api layer (OpsClient).' }],
```

(`no-restricted-properties` needs the JSX-attribute variant: if it doesn't catch the JSX prop, use `react/no-danger` from eslint-plugin-react instead — check which react plugin the config already loads and use its rule.)

- [ ] **Step 3: Lint must still pass on the un-migrated tree**

Run: `cd webApp && npm run lint`
Expected: PASS — because existing `components/**` that still import hooks would now fail, keep the *old* components allow-list as a `boundaries` **override for a temporary legacy list** of files (explicit array of currently-violating component files, generated by running lint once and capturing failures). New/migrated files get the strict rule. The legacy list can only shrink (each screen migration removes entries).

- [ ] **Step 4: Commit**

```bash
git add webApp/ && git commit -m "feat(lint): containers element + pure-components DAG + XSS bans (legacy allowlist to burn down)"
```

---

### Task 7: migrate pilot hooks onto OpsClient

**Files:**
- Modify: `webApp/src/hooks/useSessionState.ts`
- Modify: `webApp/src/hooks/useDesignMutations.ts`
- Modify: SPA root (where `QueryClientProvider` lives, `webApp/src/main.tsx`) to mount `OpsClientProvider` with `restOpsClient(apiClient)`

**Interfaces:**
- Consumes: `useOpsClient()`, `OpsClient.call`, `sessionStateKey` (unchanged export).
- Produces: hooks that behave byte-identically in the SPA and transport-blind in MCP; polling disabled when `transport === 'mcp'`.

- [ ] **Step 1: Rewire `useSessionState`**

Replace the `apiClient.GET` call and polling gate:

```ts
const { ops, transport } = useOpsClient();
// inside useQuery:
queryFn: async () => {
  const data = await ops.call('systemDesignGetSessionState', {
    path: { projectID: projectId },
    query: { kind: artifactKindToOrdinal(kind) },
  });
  return mapSessionState(data as never);
},
// polling: MCP context never background-polls (spec §3.4); SPA keeps the 2s live poll.
refetchInterval: transport === 'mcp' ? false : /* existing live-stage 2s logic unchanged */,
```

Keep the 404/`ApiError` retry logic — it works because both OpsClient impls throw `ApiError` (Task 5 step 3 note).

- [ ] **Step 2: Rewire `useDesignMutations` the same way** — each `apiClient.POST('/api/v1/system-design/<op>/...')` becomes `ops.call('systemDesign<PascalOp>', {...})`; invalidations unchanged.

- [ ] **Step 3: Mount the provider in the SPA root**

```tsx
<QueryClientProvider client={queryClient}>
  <OpsClientProvider value={{ ops: restOpsClient(apiClient), transport: 'rest' }}>
    <App />
  </OpsClientProvider>
</QueryClientProvider>
```

- [ ] **Step 4: Verify SPA unchanged**

Run: `cd webApp && npm run lint && npx tsc -b && npx vitest run` then boot locally per the run-app-locally loop (server dev mode + `npm run dev`) and click through the system-design screen: session probe, draft, approve path.
Expected: identical behavior, network tab still shows the same REST calls.

- [ ] **Step 5: Commit**

```bash
git add webApp/ && git commit -m "refactor(webapp): pilot hooks ride OpsClient; polling disabled under MCP transport"
```

---

### Task 8: purify the System-Design screen + SPA container

**Files:**
- Create: `webApp/src/containers/SystemDesignContainer.tsx`
- Modify: `webApp/src/routes/DesignExperience.tsx` (or the file where `SystemDesignBody` composes — verify with `grep -rn "SystemDesignBody" webApp/src/routes/`)
- Modify: `webApp/src/components/design/*` only as needed to make chat/comment props optional

**Interfaces:**
- Produces: `SystemDesignView` (pure) with props:

```ts
interface SystemDesignViewProps {
  project: ProjectHead;
  session: SessionStateResponse | undefined;   // undefined → loading
  spine: SpineStep[];
  displayMode?: 'inline' | 'fullscreen' | 'pip';
  onSubmitReview: (d: ReviewDecision) => void;
  onRequestDraft: (feedback?: string) => void;
  onRetry: () => void;
  // SPA-only optional surfaces:
  chat?: ReactNode;                       // ChatRail slot; omitted in MCP
  commentSurface?: CommentSurfaceProps;   // CommentProvider-backed overlays; omitted in MCP
  onSubmitSelectionComment?: (anchor: CommentAnchor, text: string) => void; // MCP: two-call pattern
}
```

- [ ] **Step 1: Extract `SystemDesignView`** — move the `ExperienceChrome`+`SlimSpine`+`StepBody` composition out of the route file into `src/components/design/SystemDesignView.tsx`, all data/handlers via the props above; `ExperienceChrome`'s `chat`/`chatOpen`/`onOpenChat` become optional (render no chat affordance when absent); `StepBody`'s comment context usage goes behind `commentSurface`/`onSubmitSelectionComment` optional props (SelectionPopover submit calls whichever is provided).
- [ ] **Step 2: Create `SystemDesignContainer`** — calls `useProject`/`useSessionState`/`useDesignMutations` and maps to props; the route file becomes `CommentProvider → SystemDesignContainer(chat=<ChatRail/>, commentSurface=fromContext)`.
- [ ] **Step 3: Remove the migrated files from the Task-6 legacy lint allowlist.**
- [ ] **Step 4: Verify** — `npm run lint && npx tsc -b && npx vitest run`, then the local click-through again (SPA identical), plus the existing uitests if present: `grep -rn "uitests" webApp/package.json` and run what's there.
- [ ] **Step 5: Commit** — `git commit -m "refactor(webapp): SystemDesignView pure screen + SPA container (chat/comments optional props)"`

---

### Task 9: MCP shell entry, registry, comment two-call

**Files:**
- Create: `webApp/mcp-app.html`, `webApp/vite.mcp.config.ts`, `webApp/src/mcpShell/main.tsx`, `webApp/src/mcpShell/registry.ts`, `webApp/src/mcpShell/McpSystemDesignContainer.tsx`
- Modify: `webApp/package.json` (`build:mcp` script; `build` chains it)

**Interfaces:**
- Consumes: `SystemDesignView`, `mcpOpsClient`, `OpsClientProvider`, `app.hostContext.toolInfo.tool.name`, `ui/notifications/tool-result` via `app.ontoolresult`.

- [ ] **Step 1: `vite.mcp.config.ts`** — fixed names, no hashes:

```ts
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',            // same dist PUSH-APP.sh publishes
    emptyOutDir: false,        // don't clobber the SPA build
    rollupOptions: {
      input: 'mcp-app.html',
      output: {
        // CLASSIC IIFE, single chunk: keeps the stub's <script> tag CORS-exempt (spec §3.4)
        format: 'iife',
        inlineDynamicImports: true,
        entryFileNames: 'mcp-app.js',
        assetFileNames: (a) => (a.name?.endsWith('.css') ? 'mcp-app.css' : 'mcp-assets/[name][extname]'),
      },
    },
  },
});
```

`package.json`: `"build:mcp": "vite build -c vite.mcp.config.ts"`, and `"build": "tsc -b && vite build && npm run build:mcp"`.

- [ ] **Step 2: `mcp-app.html`** — minimal: `<div id="root"></div><script type="module" src="/src/mcpShell/main.tsx"></script>` (the *served* stub comes from the Go server; this file exists so Vite has an entry and local dev can load it directly).

- [ ] **Step 3: `registry.ts`**

```ts
import type { ComponentType } from 'react';
import { McpSystemDesignContainer } from './McpSystemDesignContainer';

/** toolName → view container. Keys must match mcpemit-stamped tools (project.json ui annotations). */
export const VIEW_REGISTRY: Record<string, ComponentType<{ toolArgs: Record<string, unknown> }>> = {
  systemDesignGetSessionState: McpSystemDesignContainer,
};
```

- [ ] **Step 4: `main.tsx`** — boot, handshake, mount:

```tsx
import { App } from '@modelcontextprotocol/ext-apps';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { OpsClientProvider } from '../api/opsContext';
import { mcpOpsClient } from '../api/ops.gen';
import { VIEW_REGISTRY } from './registry';

const app = new App({ appInfo: { name: 'archistrator', version: '0.1.0' } });
const queryClient = new QueryClient();

let toolArgs: Record<string, unknown> = {};
app.ontoolinput = (p) => { toolArgs = (p.arguments ?? {}) as Record<string, unknown>; };
app.ontoolresult = (r) => {
  // Seed the cache so the screen renders instantly from the pushed result;
  // per-view containers own the queryKey mapping (McpSystemDesignContainer).
  window.dispatchEvent(new CustomEvent('mcp-tool-result', { detail: r }));
};

await app.connect();
const toolName = app.hostContext?.toolInfo?.tool?.name ?? '';
const View = VIEW_REGISTRY[toolName];
const theme = app.hostContext?.theme ?? 'light';

createRoot(document.getElementById('root')!).render(
  <QueryClientProvider client={queryClient}>
    <OpsClientProvider value={{ ops: mcpOpsClient(app), transport: 'mcp' }}>
      {/* wrap with the SAME ThemeProvider/AppTheme pair the SPA uses (grep src/App.tsx), mapped from `theme` */}
      {View ? <View toolArgs={toolArgs} /> : <p>No view registered for {toolName || 'this tool'}.</p>}
    </OpsClientProvider>
  </QueryClientProvider>,
);
export { app }; // containers import the singleton for updateModelContext/sendMessage
```

(If `App` constructor options differ — check `node_modules/@modelcontextprotocol/ext-apps` README — adjust; register `ontoolinput`/`ontoolresult` BEFORE `connect()` per the create-mcp-app skill.)

- [ ] **Step 5: `McpSystemDesignContainer.tsx`** — same shape as the SPA container but: derives `projectId`/`kind` from `toolArgs`; seeds the query cache from the `mcp-tool-result` event (mapSessionState → `queryClient.setQueryData(sessionStateKey(...))`); omits `chat`/`commentSurface`; passes `displayMode` from `app.hostContext`; provides `onSubmitSelectionComment`:

```ts
async function submitSelectionComment(anchor: CommentAnchor, text: string) {
  await app.updateModelContext({ content: [{ type: 'text', text: ambientDescription(anchor, text) }] });
  const res = await app.sendMessage({ role: 'user', content: [{ type: 'text', text: 'File my comment on the selected element.' }] });
  if (res.isError) setSendFallbackHint(true); // "shared with conversation — ask the agent to file it"
}
```

Plus ambient view-state sync: a `useEffect` on [selection, session?.stage] calling `app.updateModelContext` with the current view/selection description (overwrite semantics are the point).

- [ ] **Step 6: Build + lint + commit**

Run: `cd webApp && npm run build && ls dist/mcp-app.js dist/mcp-app.css`
Expected: both files exist with fixed names; `npm run lint` PASS.

```bash
git add webApp/ && git commit -m "feat(webapp): MCP shell — view registry, host handshake, comment two-call, ambient context"
```

---

### Task 10: asset publish + nginx headers

**Files:**
- Modify: `webApp/PUSH-APP.sh` (confirm it ships the whole `dist/` — likely no change)
- Modify: the nginx config wherever it lives — Run: `grep -rn "nginx\|location /" webApp/ infra/ deploy/ 2>/dev/null | grep -v node_modules | head` to locate

- [ ] **Step 1: nginx location block for the two fixed-name assets:**

```nginx
# NO CORS headers anywhere — classic-script delivery is CORS-exempt (spec §3.4)
location ~ ^/(mcp-app\.js|mcp-app\.css)$ {
    add_header Cache-Control "public, max-age=300";   # short TTL: fixed names, version query busts
}
```

- [ ] **Step 2: Deploy dist per the usual PUSH-APP flow; verify:**

Run: `curl -sI https://<webapp-origin>/mcp-app.js | grep -i 'cache-control\|access-control'`
Expected: `cache-control` present, NO `access-control-*` headers anywhere (grep count for access-control → 0 on this path and on `/api/userinfo`).

- [ ] **Step 3: Commit** any repo-tracked config; note out-of-repo infra changes in the PR description.

---

### Task 11: end-to-end pilot verification

**Files:**
- Create: `docs/superpowers/plans/2026-07-14-mcp-apps-pilot-verification.md` (running log of what passed)

- [ ] **Step 1: basic-host locally**

```bash
git clone https://github.com/modelcontextprotocol/ext-apps.git /tmp/ext-apps && cd /tmp/ext-apps/examples/basic-host && npm install
# archistrator server running locally in dev mode on :8888 (dev CORS active)
SERVERS='["http://localhost:8888/mcp"]' npm start
```

At `http://localhost:8080`: call `systemDesignGetSessionState` for a real local project. Expected: iframe renders the System-Design screen from the stub + localhost:5173 assets (run `npm run dev` or serve `dist/`); pending states on in-widget actions work (Loop A); `submitReviewDecision` from the GatePanel round-trips.

- [ ] **Step 2: Claude via tunnel** — `npx cloudflared tunnel --url http://localhost:8888`, add as custom connector (throwaway project state ONLY — spec §8.2 A07). Verify: widget renders in chat; comment two-call produces a user message and the agent files the comment; a later `getSessionState` renders the revision widget (Loop B). Log which of `updateModelContext`/`sendMessage`/`ui/open-link`/display modes Claude honors — these are the pilot's host-variance answers.

- [ ] **Step 3: Record results + commit the verification log.** Anything failing becomes a follow-up task list at the bottom of the log, not silent scope drift.

---

### Task 12: docs + earmarks wrap-up

- [ ] **Step 1:** Update the spec's §6 risks with pilot findings; move verified items out of risk status.
- [ ] **Step 2:** Record earmarks where they're tracked: platform eslint-config-web needs the same DAG change released (vendored copy edited here); UserContext fetch-ban exemption removal (relocate session probe: src/api helper → src/hooks hook → container-mounted provider; see eslint.platform.config.js burn-down comment); production OAuth on `/mcp` is a release-blocker before real state meets a connector; screen-migration checklist (remaining screens × {purify, container, registry, annotation, inline-compact variant}); `visibility:["app"]` unused for now.
- [ ] **Step 3:** Final gates on both repos, commit, and stop — founder decides merge/PR per finishing-a-development-branch.

## Self-review notes

- Spec coverage: §3.1–3.3 → Tasks 5–8; §3.4 → Tasks 3, 9; §3.5 → Tasks 1–3; §4 → Tasks 4, 11; §5 steps 1–2 → all; §8 controls → Tasks 6 (XSS bans), 10 (CORS scoping), 11 (A07 discipline). Migration of remaining screens and app-generator replication are explicitly out of this plan (spec §5 steps 3–4).
- Known in-task recon points (each has a grep step): config threading pattern in hooks.go (T3), strict-decode surfaces for project.json (T1), gen-api.mjs source path (T5), react lint plugin for the JSX danger rule (T6), uitests harness presence (T8).
