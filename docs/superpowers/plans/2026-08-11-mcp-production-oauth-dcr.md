# Production MCP OAuth for `/mcp` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `https://archistrator.capture-gtd.com/mcp` addable as a custom connector on claude.ai, authenticated by Keycloak via Dynamic Client Registration with an audience-bound access token.

**Architecture:** One declaration in the committed deployment model (`mcpResourceUri`) drives everything: the operations Manager folds it into `RuntimeDesiredState.McpSurface`, the `operatedruntime` renderer emits two extra gateway routes plus the server env var (derived from the app Host, so the advertised resource URI and the route that serves it cannot drift), and the Go composition root mounts RFC 9728 metadata, a `WWW-Authenticate` challenge, and an audience-checking validator wrapped around the `/mcp` mount only. Keycloak realm configuration ships as a runbook.

**Tech Stack:** Go 1.26, `modelcontextprotocol/go-sdk` v1.6.1, Envoy Gateway HTTPRoute/SecurityPolicy, Keycloak 26.4.6, ArgoCD GitOps.

**Spec:** `docs/superpowers/specs/2026-08-11-mcp-production-oauth-dcr-design.md`

## Global Constraints

- **Every Go command runs `GOWORK=off`.** The server builds against published platform tags, not the workspace.
- **Never hand-edit a `*.gen.go` file.** `contract.gen.go`, `config.gen.go`, `fake.gen.go`, `toolcatalog.gen.go` and the whole of `internal/client/` are generated; the drift gates (`make gen-models-check`, `gen-config-check`, `gen-client-check`) fail CI on any diff. Change `project.json` and regenerate.
- **Never weaken a gate.** Lint is 7+ linters plus `revive`/`gocritic`/`gocyclo` (limit 15). If a new function trips gocyclo, split it — do not raise the limit or add a nolint.
- **Composition-root-only code lives outside `internal/`** (`server/cmd/server/*.go`). Only files there may import the MCP SDK transport or wire mounts; the Method arch checker does not scan them.
- **No platform (`framework-go`) change is in scope.** If a task appears to need one, stop and report rather than editing the platform repo.
- Working directory for all `make`/`go` commands is `server/` unless a step says otherwise.
- Canonical values used throughout, copied verbatim from the spec:
  - resource URI: `https://archistrator.capture-gtd.com/mcp`
  - metadata URL: `https://archistrator.capture-gtd.com/.well-known/oauth-protected-resource/mcp`
  - scope: `archistrator-mcp`
  - issuer: `https://keycloak.capture-gtd.com/realms/archistrator`
  - Anthropic egress: `160.79.104.0/21`

---

### Task 1: Declare `mcpResourceUri` in the deployment model

The single declaration the rest of the plan keys off. Its **presence** in the committed deployment model is what marks archistrator as serving an MCP surface (Task 2); its **value** is supplied at runtime by the renderer (Task 4), so the default stays empty and the URL is written down in exactly one place.

**Files:**
- Modify: `.aiarch/state/project.json` — slot `6` → `model.deployment.settings` (array; sibling entries are `listenAddr`, `shutdownTimeout`, `constructionEscalationTimeout`)
- Regenerated (do not hand-edit): `server/cmd/server/config.gen.go`, `systemtests/internal/harness/envnames.gen.go`
- Test: `server/cmd/server/config_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `Config.McpResourceUri string`, read from env `ARCHISTRATOR_MCP_RESOURCE_URI`, default `""`. Later tasks read `cfg.McpResourceUri`.

- [ ] **Step 1: Write the failing test**

Append to `server/cmd/server/config_test.go`:

```go
func TestConfig_McpResourceUriFromEnv(t *testing.T) {
	t.Setenv("ARCHISTRATOR_MCP_RESOURCE_URI", "https://archistrator.capture-gtd.com/mcp")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.McpResourceUri != "https://archistrator.capture-gtd.com/mcp" {
		t.Fatalf("McpResourceUri = %q, want the configured URI", cfg.McpResourceUri)
	}
}

func TestConfig_McpResourceUriDefaultsEmpty(t *testing.T) {
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.McpResourceUri != "" {
		t.Fatalf("McpResourceUri = %q, want empty by default", cfg.McpResourceUri)
	}
}
```

`LoadConfig()` (`config.gen.go:63`) is the generated plain-env read and is the right loader here. The existing tests in this file use `loadResolvedConfig()` (`config_adapter.go:59`) instead because they exercise the fail-fast *validation* paths, which require a full credential env — this test needs neither.

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd server && GOWORK=off go test ./cmd/server/ -run TestConfig_McpResourceUri -v
```

Expected: compile failure — `cfg.McpResourceUri undefined`.

- [ ] **Step 3: Add the setting to the deployment model**

In `.aiarch/state/project.json`, inside slot `6`'s `model.deployment.settings` array, add:

```json
{
  "name": "mcpResourceUri",
  "type": "string",
  "default": "",
  "env": "ARCHISTRATOR_MCP_RESOURCE_URI",
  "description": "Canonical RFC 8707 resource URI of this app's MCP surface (e.g. https://archistrator.capture-gtd.com/mcp). Empty disables the OAuth protected-resource metadata, the WWW-Authenticate challenge, and the /mcp audience check. Its PRESENCE as a declared setting is what marks this app as serving an MCP surface; its VALUE is supplied at deploy time by the operatedruntime renderer, derived from the app Host."
}
```

The file is large — edit the `settings` array in place; do not reformat surrounding JSON. This is a committed-state amendment, the same shape as commit `d91c481` ("operationalConcepts: deployment gains infrastructure/bindings/settings").

- [ ] **Step 4: Regenerate config**

```bash
cd server && make gen-config
git status --short -- cmd/server/config.gen.go ../systemtests/internal/harness/envnames.gen.go
```

Expected: both files modified, `config.gen.go` now carrying a `McpResourceUri string` field.

- [ ] **Step 5: Run the test to verify it passes**

```bash
cd server && GOWORK=off go test ./cmd/server/ -run TestConfig_McpResourceUri -v
```

Expected: PASS.

- [ ] **Step 6: Verify the drift gate is clean**

```bash
cd server && make gen-config-check
```

Expected: exits 0 with no diff.

- [ ] **Step 7: Commit**

```bash
git add .aiarch/state/project.json server/cmd/server/config.gen.go \
        systemtests/internal/harness/envnames.gen.go server/cmd/server/config_test.go
git commit -m "feat(config): declare mcpResourceUri in the deployment model"
```

---

### Task 2: `McpSurface` on `RuntimeDesiredState`, derived from the declaration

**Files:**
- Modify: `.aiarch/state/project.json` — `.serviceContracts` entry for `operatedruntime`, `RuntimeDesiredState` shape (add `McpSurface`, a `bool`, alongside the existing `SelfManaged`)
- Regenerated: `server/internal/resourceaccess/operatedruntime/contract.gen.go`, `server/internal/resourceaccess/projectstate/toolcatalog.gen.go`
- Modify: `server/internal/manager/operations/deploy.go` (`assembleDesiredState`, ~line 333)
- Test: `server/internal/manager/operations/manager_test.go`

**Interfaces:**
- Consumes: the `mcpResourceUri` setting name from Task 1.
- Produces: `RuntimeDesiredState.McpSurface bool`; helper `func declaresMcpSurface(settings []projectstate.DeploymentSettingSpec) bool` in `deploy.go`.

- [ ] **Step 1: Write the failing test**

Append to `server/internal/manager/operations/manager_test.go`:

```go
func TestDeclaresMcpSurface(t *testing.T) {
	str := func(s string) *string { return &s }
	cases := []struct {
		name     string
		settings []projectstate.DeploymentSettingSpec
		want     bool
	}{
		{"declared with empty default", []projectstate.DeploymentSettingSpec{
			{Name: "listenAddr", Type: "string", Env: "ARCHISTRATOR_LISTEN_ADDR", Default: str(":8080")},
			{Name: "mcpResourceUri", Type: "string", Env: "ARCHISTRATOR_MCP_RESOURCE_URI", Default: str("")},
		}, true},
		{"declared with a value", []projectstate.DeploymentSettingSpec{
			{Name: "mcpResourceUri", Type: "string", Env: "ARCHISTRATOR_MCP_RESOURCE_URI", Default: str("https://x/mcp")},
		}, true},
		{"not declared", []projectstate.DeploymentSettingSpec{
			{Name: "listenAddr", Type: "string", Env: "ARCHISTRATOR_LISTEN_ADDR", Default: str(":8080")},
		}, false},
		{"no settings at all", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := declaresMcpSurface(tc.settings); got != tc.want {
				t.Fatalf("declaresMcpSurface = %v, want %v", got, tc.want)
			}
		})
	}
}
```

Check the import alias `manager_test.go` already uses for the projectstate package and match it.

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd server && GOWORK=off go test ./internal/manager/operations/ -run TestDeclaresMcpSurface -v
```

Expected: compile failure — `undefined: declaresMcpSurface`.

- [ ] **Step 3: Add the contract field and regenerate**

In `.aiarch/state/project.json`, find the `operatedruntime` service contract's `RuntimeDesiredState` type and add a `McpSurface` field of type `bool` immediately after `SelfManaged`, matching the exact JSON shape the sibling fields use in that contract. Then:

```bash
cd server && make gen-models gen-client
git status --short -- internal/resourceaccess/operatedruntime/contract.gen.go \
                      internal/resourceaccess/projectstate/toolcatalog.gen.go
```

Expected: both regenerated; `contract.gen.go` now has `McpSurface bool \`json:"McpSurface"\`` on `RuntimeDesiredState`.

- [ ] **Step 4: Implement the helper and the fold**

In `server/internal/manager/operations/deploy.go`, add above `assembleDesiredState`:

```go
// mcpResourceUriSetting is the deployment-model setting whose PRESENCE declares
// that an app serves an MCP surface. Presence, not value: the value is supplied
// at deploy time by the renderer (derived from the app Host), so a project that
// declares the setting with an empty default still gets its /mcp and
// protected-resource-metadata routes.
const mcpResourceUriSetting = "mcpResourceUri"

// declaresMcpSurface reports whether the deployment model declares the MCP
// resource-URI setting.
func declaresMcpSurface(settings []projectstate.DeploymentSettingSpec) bool {
	for _, s := range settings {
		if s.Name == mcpResourceUriSetting {
			return true
		}
	}
	return false
}
```

Then in `assembleDesiredState`, add the field to the `desired` literal, directly after `SelfManaged`:

```go
		SelfManaged: appName == selfManagedProjectRef,
		McpSurface:  declaresMcpSurface(model.Deployment.Settings),
```

`model` is not in scope at that point — `resolveCloudEnvironment` currently reads it and returns only the environment. Change `resolveCloudEnvironment` to also return the model, or add a sibling resolver that returns `*projectstate.DeploymentOperationsModel`, following whichever shape keeps `assembleDesiredState` under the gocyclo limit. Do not duplicate the `proj.OperationalConcepts.Model.(*projectstate.DeploymentOperationsModel)` type assertion in two places — extract it once and have both callers use it.

- [ ] **Step 5: Run the tests to verify they pass**

```bash
cd server && GOWORK=off go test ./internal/manager/operations/ -v
```

Expected: PASS, including the existing tests.

- [ ] **Step 6: Verify the drift gates are clean**

```bash
cd server && make gen-models-check gen-fakes-check gen-client-check
```

Expected: all exit 0.

- [ ] **Step 7: Commit**

```bash
git add .aiarch/state/project.json server/internal/resourceaccess/operatedruntime/contract.gen.go \
        server/internal/resourceaccess/projectstate/toolcatalog.gen.go \
        server/internal/manager/operations/deploy.go server/internal/manager/operations/manager_test.go
git commit -m "feat(operations): fold the declared MCP surface into RuntimeDesiredState"
```

---

### Task 3: Render the `/mcp` and protected-resource-metadata gateway routes

**Files:**
- Modify: `server/internal/resourceaccess/operatedruntime/operatedruntimeaccess.go` — `gatewayRoutes()` (~line 1659)
- Modify: `server/internal/resourceaccess/operatedruntime/testdata/golden/production/archistrator-gateway-routes.yaml`
- Test: `server/internal/resourceaccess/operatedruntime/access_test.go`

**Interfaces:**
- Consumes: `RuntimeDesiredState.McpSurface` from Task 2; the existing `mk(suffix, path, backend, port, browserFacing)` closure and `routeSpec` struct.
- Produces: routes named `<app>-mcp-route` and `<app>-oauth-prm-route`, both `BrowserFacing: false`.

- [ ] **Step 1: Write the failing tests**

Append to `server/internal/resourceaccess/operatedruntime/access_test.go`. `testDesiredState()` (line ~478) is the shared fixture; it will need `McpSurface: true` added in Step 3 so the production goldens stay representative.

```go
func TestRender_McpRoutesOnlyWhenSurfaceDeclared(t *testing.T) {
	withSurface := testDesiredState()
	withSurface.McpSurface = true
	names := routeNames(gatewayRoutes(withSurface))
	for _, want := range []string{"archistrator-mcp-route", "archistrator-oauth-prm-route"} {
		if !slices.Contains(names, want) {
			t.Fatalf("route %q missing when McpSurface is declared; got %v", want, names)
		}
	}

	without := testDesiredState()
	without.McpSurface = false
	names = routeNames(gatewayRoutes(without))
	if len(names) != 4 {
		t.Fatalf("undeclared app must render exactly the four base routes; got %v", names)
	}
	for _, unwanted := range []string{"archistrator-mcp-route", "archistrator-oauth-prm-route"} {
		if slices.Contains(names, unwanted) {
			t.Fatalf("route %q rendered for an app that never declared an MCP surface", unwanted)
		}
	}
}

func TestRender_McpRoutesAreNotBrowserFacing(t *testing.T) {
	d := testDesiredState()
	d.McpSurface = true
	for _, r := range gatewayRoutes(d) {
		switch r.Name {
		case "archistrator-mcp-route", "archistrator-oauth-prm-route":
			if r.BrowserFacing {
				t.Fatalf("%s is browser-facing: the OIDC SecurityPolicy would install an "+
					"authorization-code redirect filter that a non-browser MCP client cannot follow", r.Name)
			}
		}
	}
}

// routeNames extracts route names in render order.
func routeNames(routes []routeSpec) []string {
	out := make([]string, 0, len(routes))
	for _, r := range routes {
		out = append(out, r.Name)
	}
	return out
}
```

Add `"slices"` to the test file's imports if absent.

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/operatedruntime/ -run TestRender_Mcp -v
```

Expected: FAIL — `withSurface.McpSurface` compiles (Task 2 added it) but the routes are missing.

- [ ] **Step 3: Implement**

In `gatewayRoutes()`, replace the `return []routeSpec{...}` literal with a built slice:

```go
	routes := []routeSpec{
		// Browser SPA — full authorization-code redirect login, and the route
		// that owns /oauth2/callback via its OIDC filter (see the doc above).
		mk("webapp", "/", webapp, webAppContainerPort, true),
		// API — the edge validates the Keycloak JWT (defense in depth) and
		// forwards the Authorization header unchanged; the Go server validates
		// that same bearer token itself.
		mk("api", "/api", server, serverContainerPort, true),
		// Liveness / readiness — unauthenticated by construction.
		mk("healthz", "/healthz", server, serverContainerPort, false),
		mk("readyz", "/readyz", server, serverContainerPort, false),
	}
	if d.McpSurface {
		// MCP transport. NOT browser-facing: the OIDC SecurityPolicy installs an
		// authorization-code redirect filter, and an MCP client cannot follow a
		// browser redirect — the connection would die before it ever presented a
		// token. The Go server validates the bearer token itself, and additionally
		// requires the MCP audience on this surface only.
		routes = append(routes, mk("mcp", "/mcp", server, serverContainerPort, false))
		// RFC 9728 protected resource metadata. MUST answer unauthenticated —
		// it is what tells an MCP client where the authorization server is. The
		// PathPrefix match also covers the
		// /.well-known/oauth-protected-resource/mcp sub-path form.
		routes = append(routes, mk("oauth-prm", "/.well-known/oauth-protected-resource", server, serverContainerPort, false))
	}
	return routes
```

Also set `McpSurface: true` in the `testDesiredState()` fixture so the production goldens represent the deployed app.

- [ ] **Step 4: Update the production goldens**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/operatedruntime/ -run 'TestRender_(GatewayRoutesMatchProduction|MatchesProductionGoldens)' -v
```

Expected: FAIL with a diff. Read the diff and hand-add the two new HTTPRoute documents to `testdata/golden/production/archistrator-gateway-routes.yaml`, matching the byte shape of the existing `archistrator-healthz-route` document (same non-browser-facing shape) with the new names and paths. Re-run until the goldens match. Do **not** regenerate goldens blindly from output — the golden is the review surface.

- [ ] **Step 5: Run the whole package to verify**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/operatedruntime/ -v
```

Expected: PASS, including `TestRender_SecurityPolicyTargetsOnlyBrowserFacingRoutes` (line ~1017) and `TestRender_HasNoDedicatedOAuth2Route` (line ~1000) — the two standing guards that neither new route drifts under the OIDC policy.

- [ ] **Step 6: Commit**

```bash
git add server/internal/resourceaccess/operatedruntime/
git commit -m "feat(operatedruntime): render /mcp and protected-resource-metadata routes"
```

---

### Task 4: Emit `ARCHISTRATOR_MCP_RESOURCE_URI` from the renderer

The value is derived from `d.Host`, so the advertised resource URI and the route that serves it cannot drift.

**Files:**
- Modify: `server/internal/resourceaccess/operatedruntime/operatedruntimeaccess.go` — the server env block (~line 1339)
- Modify: `server/internal/resourceaccess/operatedruntime/testdata/golden/production/archistrator-server.yaml`
- Test: `server/internal/resourceaccess/operatedruntime/access_test.go`

**Interfaces:**
- Consumes: `RuntimeDesiredState.McpSurface`, `RuntimeDesiredState.Host`.
- Produces: `func mcpResourceURI(host string) string` returning `"https://" + host + "/mcp"`. Task 5 depends on this exact format reaching the server as `cfg.McpResourceUri`.

- [ ] **Step 1: Write the failing test**

```go
func TestRender_ServerEnvCarriesMcpResourceURIWhenDeclared(t *testing.T) {
	d := testDesiredState()
	d.McpSurface = true
	yaml := renderServerYAML(t, d)
	const want = "https://archistrator.capture-gtd.com/mcp"
	if !strings.Contains(yaml, "ARCHISTRATOR_MCP_RESOURCE_URI") || !strings.Contains(yaml, want) {
		t.Fatalf("server manifest missing ARCHISTRATOR_MCP_RESOURCE_URI=%s\n%s", want, yaml)
	}
}

func TestRender_ServerEnvOmitsMcpResourceURIWhenUndeclared(t *testing.T) {
	d := testDesiredState()
	d.McpSurface = false
	if yaml := renderServerYAML(t, d); strings.Contains(yaml, "ARCHISTRATOR_MCP_RESOURCE_URI") {
		t.Fatalf("app with no MCP surface must not advertise a resource URI\n%s", yaml)
	}
}
```

`renderServerYAML` is a helper you write to render and return the `archistrator-server` Deployment manifest text — the existing render tests (e.g. `TestRender_EmitsServerDeploymentWithModelKey`, line ~516) already locate a manifest by Kind+Name; reuse that same lookup rather than inventing a second one, and factor it into the helper if it is currently inline.

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/operatedruntime/ -run TestRender_ServerEnv -v
```

Expected: FAIL — env var absent.

- [ ] **Step 3: Implement**

Add near the other derivation helpers:

```go
// mcpResourceURI is the canonical RFC 8707 resource identifier for an app's MCP
// surface. Derived from the app Host so the URI the server ADVERTISES in its
// protected-resource metadata and the gateway route that SERVES it are one
// derivation and cannot drift. Claude requires this to match the connector URL
// the user types, exactly, including the path.
func mcpResourceURI(host string) string {
	return "https://" + host + "/mcp"
}
```

In `serverEnv(d RuntimeDesiredState, serverName string) []envVar` (~line 1336), append to its `env` slice, after the existing entries and before the return:

```go
	if d.McpSurface {
		env = append(env, envVar{Name: "ARCHISTRATOR_MCP_RESOURCE_URI", Value: mcpResourceURI(d.Host)})
	}
```

That function's doc comment states its order matches the golden file "so a human diffing the two can read them side by side" — so add the golden entry in Step 4 at the matching position, not at the end of the manifest's env list if that differs.

- [ ] **Step 4: Update the server golden**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/operatedruntime/ -run TestRender_MatchesProductionGoldens -v
```

Read the diff, hand-add the env entry to `testdata/golden/production/archistrator-server.yaml` in the position the renderer emits it, re-run until clean.

- [ ] **Step 5: Run the whole package**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/operatedruntime/ -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add server/internal/resourceaccess/operatedruntime/
git commit -m "feat(operatedruntime): derive the MCP resource URI env from the app host"
```

---

### Task 5: Serve RFC 9728 protected resource metadata

**Files:**
- Create: `server/cmd/server/mcp_oauth.go`
- Modify: `server/cmd/server/hooks.go` — `ExtraMounts` (~line 535)
- Test: `server/cmd/server/mcp_oauth_test.go`

**Interfaces:**
- Consumes: `cfg.McpResourceUri` (Task 1), populated in production by Task 4.
- Produces:
  - `const mcpScope = "archistrator-mcp"`
  - `func resourceMetadataURL(resourceURI string) (string, error)`
  - `func protectedResourceMetadataHandler(resourceURI, issuer string) (http.Handler, error)`
  - `func metadataPaths(resourceURI string) ([]string, error)` returning the two mount paths
  Tasks 6 and 7 consume `mcpScope` and `resourceMetadataURL`.

- [ ] **Step 1: Write the failing tests**

Create `server/cmd/server/mcp_oauth_test.go`:

```go
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const (
	testResourceURI = "https://archistrator.capture-gtd.com/mcp"
	testIssuer      = "https://keycloak.capture-gtd.com/realms/archistrator"
)

func TestResourceMetadataURL(t *testing.T) {
	got, err := resourceMetadataURL(testResourceURI)
	if err != nil {
		t.Fatalf("resourceMetadataURL: %v", err)
	}
	const want = "https://archistrator.capture-gtd.com/.well-known/oauth-protected-resource/mcp"
	if got != want {
		t.Fatalf("resourceMetadataURL = %q, want %q", got, want)
	}
}

func TestResourceMetadataURLRejectsGarbage(t *testing.T) {
	for _, in := range []string{"", "archistrator.capture-gtd.com/mcp", "://nope"} {
		if _, err := resourceMetadataURL(in); err == nil {
			t.Fatalf("resourceMetadataURL(%q) = nil error, want a failure", in)
		}
	}
}

func TestMetadataPaths(t *testing.T) {
	got, err := metadataPaths(testResourceURI)
	if err != nil {
		t.Fatalf("metadataPaths: %v", err)
	}
	want := []string{
		"GET /.well-known/oauth-protected-resource",
		"GET /.well-known/oauth-protected-resource/mcp",
	}
	if len(got) != len(want) {
		t.Fatalf("metadataPaths = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("metadataPaths[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestProtectedResourceMetadataBody(t *testing.T) {
	h, err := protectedResourceMetadataHandler(testResourceURI, testIssuer)
	if err != nil {
		t.Fatalf("protectedResourceMetadataHandler: %v", err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — discovery MUST answer unauthenticated", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}

	var got struct {
		Resource               string   `json:"resource"`
		AuthorizationServers   []string `json:"authorization_servers"`
		ScopesSupported        []string `json:"scopes_supported"`
		BearerMethodsSupported []string `json:"bearer_methods_supported"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, rec.Body.String())
	}
	if got.Resource != testResourceURI {
		t.Fatalf("resource = %q, want %q — Claude requires an exact match with the connector URL", got.Resource, testResourceURI)
	}
	if len(got.AuthorizationServers) != 1 || got.AuthorizationServers[0] != testIssuer {
		t.Fatalf("authorization_servers = %v, want exactly [%q] — Claude uses the first entry and never falls back", got.AuthorizationServers, testIssuer)
	}
	if len(got.ScopesSupported) != 1 || got.ScopesSupported[0] != mcpScope {
		t.Fatalf("scopes_supported = %v, want [%q]", got.ScopesSupported, mcpScope)
	}
	if len(got.BearerMethodsSupported) != 1 || got.BearerMethodsSupported[0] != "header" {
		t.Fatalf("bearer_methods_supported = %v, want [\"header\"]", got.BearerMethodsSupported)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd server && GOWORK=off go test ./cmd/server/ -run 'TestResourceMetadata|TestMetadataPaths|TestProtectedResource' -v
```

Expected: compile failure — undefined symbols.

- [ ] **Step 3: Implement**

Create `server/cmd/server/mcp_oauth.go`:

```go
// OAuth surface for the MCP transport: RFC 9728 protected resource metadata, the
// RFC 6750 WWW-Authenticate challenge, and the audience check that binds a token
// to THIS resource. Composition-root glue like mcp_mount.go / mcp_apps.go —
// lives outside internal/, wired from the ExtraMounts hook.
//
// Why this exists: Keycloak does not implement RFC 8707 resource indicators, so
// the `resource` parameter Claude sends is ignored and nothing binds the issued
// token to this server. The binding is rebuilt out of a Keycloak client scope
// carrying an Audience mapper (see the realm runbook): the challenge asks for the
// scope, the scope stamps the audience, and mcpAudienceValidator requires it.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// mcpScope is the Keycloak client scope whose Audience protocol mapper stamps
// this resource's URI into the access token's aud claim. Advertised in the
// metadata document and named in the 401 challenge, which is what makes Claude
// request it.
const mcpScope = "archistrator-mcp"

// wellKnownProtectedResource is the RFC 9728 well-known path prefix.
const wellKnownProtectedResource = "/.well-known/oauth-protected-resource"

// protectedResourceMetadata is the RFC 9728 document. Field names are wire
// contract — an MCP client reads them verbatim.
type protectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
}

// parseResourceURI validates the configured MCP resource URI and returns it
// parsed. An https URI with a path is required: RFC 8707 canonical form, and
// the path is what the well-known sub-path form is built from.
func parseResourceURI(resourceURI string) (*url.URL, error) {
	u, err := url.Parse(resourceURI)
	if err != nil {
		return nil, fmt.Errorf("mcp resource URI %q is not a URL: %w", resourceURI, err)
	}
	if u.Scheme != "https" || u.Host == "" || u.Path == "" || u.Path == "/" {
		return nil, fmt.Errorf("mcp resource URI %q must be an https URL with a path (e.g. https://host/mcp)", resourceURI)
	}
	return u, nil
}

// resourceMetadataURL is the absolute URL of the metadata document for
// resourceURI, in RFC 9728's path-inserted form: the resource's path is appended
// to the well-known prefix. This is the value the WWW-Authenticate challenge
// points at.
func resourceMetadataURL(resourceURI string) (string, error) {
	u, err := parseResourceURI(resourceURI)
	if err != nil {
		return "", err
	}
	return u.Scheme + "://" + u.Host + wellKnownProtectedResource + u.Path, nil
}

// metadataPaths returns the ServeMux patterns the metadata document is mounted
// at: the path-inserted form Claude tries first, and the root form it falls back
// to. Both are served so a client that skips the WWW-Authenticate pointer still
// finds the document.
func metadataPaths(resourceURI string) ([]string, error) {
	u, err := parseResourceURI(resourceURI)
	if err != nil {
		return nil, err
	}
	return []string{
		"GET " + wellKnownProtectedResource,
		"GET " + wellKnownProtectedResource + u.Path,
	}, nil
}

// protectedResourceMetadataHandler serves the constant, config-templated RFC
// 9728 document. Mounted OUTSIDE the auth boundary: an MCP client reads it
// precisely because it does not yet hold a token.
func protectedResourceMetadataHandler(resourceURI, issuer string) (http.Handler, error) {
	if _, err := parseResourceURI(resourceURI); err != nil {
		return nil, err
	}
	if issuer == "" {
		return nil, fmt.Errorf("mcp protected resource metadata: authorization server issuer is required")
	}
	body, err := json.Marshal(protectedResourceMetadata{
		Resource:               resourceURI,
		AuthorizationServers:   []string{issuer},
		ScopesSupported:        []string{mcpScope},
		BearerMethodsSupported: []string{"header"},
	})
	if err != nil {
		return nil, fmt.Errorf("mcp protected resource metadata: marshal: %w", err)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(body)
	}), nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd server && GOWORK=off go test ./cmd/server/ -run 'TestResourceMetadata|TestMetadataPaths|TestProtectedResource' -v
```

Expected: PASS.

- [ ] **Step 5: Mount it in `ExtraMounts`**

In `server/cmd/server/hooks.go`, immediately after the existing `root.Handle("/mcp", …)` line, add:

```go
	// RFC 9728 protected resource metadata — unauthenticated by requirement, and
	// mounted only when this deployment declares an MCP resource URI (empty on
	// the local profile, where dev-mode auth means no client ever needs it).
	if cfg.McpResourceUri != "" {
		prm, err := protectedResourceMetadataHandler(cfg.McpResourceUri, cfg.KeycloakIssuer)
		if err != nil {
			h.logger.Error("mcp oauth metadata not mounted", "error", err)
		} else {
			paths, perr := metadataPaths(cfg.McpResourceUri)
			if perr != nil {
				h.logger.Error("mcp oauth metadata paths", "error", perr)
			} else {
				for _, p := range paths {
					root.Handle(p, prm)
				}
			}
		}
	}
```

`cfg.KeycloakIssuer` (`config.gen.go:35`, from `ARCHISTRATOR_KEYCLOAK_ISSUER`) is the existing field the Keycloak validator is already constructed from. Reuse it — do not add a new setting for the issuer.

- [ ] **Step 6: Verify the build and full package tests**

```bash
cd server && GOWORK=off go build ./... && GOWORK=off go test ./cmd/server/ -v
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add server/cmd/server/mcp_oauth.go server/cmd/server/mcp_oauth_test.go server/cmd/server/hooks.go
git commit -m "feat(mcp): serve RFC 9728 protected resource metadata"
```

---

### Task 6: `WWW-Authenticate` challenge on `/mcp` 401s

**Files:**
- Modify: `server/cmd/server/mcp_oauth.go`
- Modify: `server/cmd/server/mcp_mount.go` — `newMCPHandler`
- Test: `server/cmd/server/mcp_oauth_test.go`

**Interfaces:**
- Consumes: `resourceMetadataURL`, `mcpScope` (Task 5).
- Produces: `func mcpAuthChallenge(metadataURL string, next http.Handler) http.Handler`.

- [ ] **Step 1: Write the failing tests**

Append to `server/cmd/server/mcp_oauth_test.go`:

```go
func TestMcpAuthChallengeStampsOn401(t *testing.T) {
	metaURL, err := resourceMetadataURL(testResourceURI)
	if err != nil {
		t.Fatalf("resourceMetadataURL: %v", err)
	}
	h := mcpAuthChallenge(metaURL, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))

	got := rec.Header().Get("WWW-Authenticate")
	want := `Bearer resource_metadata="https://archistrator.capture-gtd.com/.well-known/oauth-protected-resource/mcp", scope="archistrator-mcp"`
	if got != want {
		t.Fatalf("WWW-Authenticate = %q, want %q", got, want)
	}
}

func TestMcpAuthChallengeSilentOnSuccess(t *testing.T) {
	h := mcpAuthChallenge("https://example.test/meta", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))

	if got := rec.Header().Get("WWW-Authenticate"); got != "" {
		t.Fatalf("WWW-Authenticate = %q on a 200; Claude does not honor it there and it misleads other clients", got)
	}
}

func TestMcpAuthChallengeForwardsFlusher(t *testing.T) {
	var sawFlusher bool
	h := mcpAuthChallenge("https://example.test/meta", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, sawFlusher = w.(http.Flusher)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/mcp", nil))
	if !sawFlusher {
		t.Fatal("wrapped ResponseWriter dropped http.Flusher — streamable-HTTP SSE responses would stall")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd server && GOWORK=off go test ./cmd/server/ -run TestMcpAuthChallenge -v
```

Expected: compile failure — `undefined: mcpAuthChallenge`.

- [ ] **Step 3: Implement**

Append to `server/cmd/server/mcp_oauth.go`:

```go
// mcpAuthChallenge stamps the RFC 6750 / RFC 9728 challenge on 401 responses
// from next, pointing an MCP client at the metadata document and naming the
// scope to request. Scoped to the /mcp mount ONLY — the REST surface has no
// business advertising MCP resource metadata. Claude ignores a WWW-Authenticate
// header on a 200, so it is stamped on 401 and nothing else.
func mcpAuthChallenge(metadataURL string, next http.Handler) http.Handler {
	challenge := fmt.Sprintf("Bearer resource_metadata=%q, scope=%q", metadataURL, mcpScope)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&challengeWriter{ResponseWriter: w, challenge: challenge}, r)
	})
}

// challengeWriter adds the challenge header the moment a 401 status is written,
// and is otherwise transparent. It forwards http.Flusher because the MCP
// streamable-HTTP transport streams SSE — a wrapper that swallowed Flush would
// stall every server-to-client notification.
type challengeWriter struct {
	http.ResponseWriter
	challenge string
	wrote     bool
}

func (w *challengeWriter) WriteHeader(code int) {
	if !w.wrote {
		w.wrote = true
		if code == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", w.challenge)
		}
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *challengeWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd server && GOWORK=off go test ./cmd/server/ -run TestMcpAuthChallenge -v
```

Expected: PASS.

- [ ] **Step 5: Wire it into the MCP handler**

`newMCPHandler` in `mcp_mount.go` gains a `resourceURI string` parameter and returns the challenge-wrapped handler. Its final line becomes:

```go
	handler := devCORS(dev.Enabled, web.AuthMiddleware(dev, validator)(transport))
	if resourceURI == "" {
		// Local/dev profile: no OAuth surface is advertised, and dev-mode auth
		// never issues a 401 anyway.
		return handler
	}
	metadataURL, err := resourceMetadataURL(resourceURI)
	if err != nil {
		// Misconfigured URI: serve MCP without a challenge rather than not at
		// all; Task 5 has already logged the same fault for the metadata mount.
		return handler
	}
	return mcpAuthChallenge(metadataURL, handler)
```

Update the `root.Handle("/mcp", newMCPHandler(...))` call in `hooks.go` to pass `cfg.McpResourceUri`.

- [ ] **Step 6: Verify the build and full package tests**

```bash
cd server && GOWORK=off go build ./... && GOWORK=off go test ./cmd/server/ -v
```

Expected: PASS, including the existing `TestMCPMountInitializeAndListTools` and `TestMCPAppsSeam`.

- [ ] **Step 7: Commit**

```bash
git add server/cmd/server/mcp_oauth.go server/cmd/server/mcp_oauth_test.go server/cmd/server/mcp_mount.go server/cmd/server/hooks.go
git commit -m "feat(mcp): challenge 401s with the protected-resource metadata pointer"
```

---

### Task 7: Require the MCP audience on `/mcp` only

The security boundary of this wave. Before it, any archistrator-realm token is accepted at `/mcp`.

**Files:**
- Modify: `server/cmd/server/mcp_oauth.go`
- Modify: `server/cmd/server/mcp_mount.go` — `newMCPHandler`
- Test: `server/cmd/server/mcp_oauth_test.go`

**Interfaces:**
- Consumes: `security.Validator`, `security.Principal.Claims` (already carries every claim verbatim).
- Produces: `func mcpAudienceValidator(inner security.Validator, want string) security.Validator`.

- [ ] **Step 1: Write the failing tests**

Append to `server/cmd/server/mcp_oauth_test.go` (add imports `context`, `errors`, and the platform `security` package):

```go
// stubValidator returns a fixed principal, standing in for the Keycloak validator.
type stubValidator struct {
	principal security.Principal
	err       error
}

func (s stubValidator) ValidateAccessToken(context.Context, string) (security.Principal, error) {
	return s.principal, s.err
}

func principalWithAud(aud any) security.Principal {
	return security.Principal{Subject: "user-1", Claims: map[string]any{"aud": aud}}
}

func TestMcpAudienceValidator(t *testing.T) {
	cases := []struct {
		name      string
		principal security.Principal
		wantOK    bool
	}{
		{"string aud matches", principalWithAud(testResourceURI), true},
		{"array aud contains the resource", principalWithAud([]any{"account", testResourceURI}), true},
		{"array aud without the resource", principalWithAud([]any{"account", "archistrator-webapp"}), false},
		{"string aud mismatch", principalWithAud("archistrator-webapp"), false},
		{"aud absent", security.Principal{Subject: "user-1", Claims: map[string]any{}}, false},
		{"no claims at all", security.Principal{Subject: "user-1"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := mcpAudienceValidator(stubValidator{principal: tc.principal}, testResourceURI)
			p, err := v.ValidateAccessToken(context.Background(), "token")
			if tc.wantOK {
				if err != nil {
					t.Fatalf("ValidateAccessToken: %v, want the principal through", err)
				}
				if p.Subject != "user-1" {
					t.Fatalf("Subject = %q, want the inner principal passed through unchanged", p.Subject)
				}
				return
			}
			if err == nil {
				t.Fatal("ValidateAccessToken accepted a token not issued for this resource")
			}
		})
	}
}

func TestMcpAudienceValidatorPropagatesInnerFailure(t *testing.T) {
	sentinel := errors.New("bad signature")
	v := mcpAudienceValidator(stubValidator{err: sentinel}, testResourceURI)
	if _, err := v.ValidateAccessToken(context.Background(), "token"); err == nil {
		t.Fatal("inner validator failure was swallowed")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd server && GOWORK=off go test ./cmd/server/ -run TestMcpAudience -v
```

Expected: compile failure — `undefined: mcpAudienceValidator`.

- [ ] **Step 3: Implement**

Append to `server/cmd/server/mcp_oauth.go` (import the platform `security` package as `mcp_mount.go` already does):

```go
// mcpAudienceValidator wraps inner with the audience check MCP requires of a
// resource server: the token must have been issued FOR this resource.
//
// Applied to the /mcp mount ONLY, and that is load-bearing: AuthMiddleware
// shares one validator across /api/v1 and /mcp, so requiring the MCP audience
// globally would reject every webapp token and take the SPA down.
//
// The claim is read from the already-verified principal (mapPrincipal carries
// every claim through verbatim in Principal.Claims), so nothing re-parses the
// JWT and the platform validator is untouched.
func mcpAudienceValidator(inner security.Validator, want string) security.Validator {
	return audienceValidator{inner: inner, want: want}
}

type audienceValidator struct {
	inner security.Validator
	want  string
}

func (v audienceValidator) ValidateAccessToken(ctx context.Context, raw string) (security.Principal, error) {
	p, err := v.inner.ValidateAccessToken(ctx, raw)
	if err != nil {
		return security.Principal{}, err
	}
	if !audienceContains(p.Claims["aud"], v.want) {
		return security.Principal{}, security.NewError(security.ErrUnauthenticated)
	}
	return p, nil
}

// audienceContains reports whether the aud claim names want. Keycloak emits aud
// as a bare string when there is one audience and an array when there are
// several, so both shapes are handled.
func audienceContains(claim any, want string) bool {
	switch aud := claim.(type) {
	case string:
		return aud == want
	case []string:
		for _, a := range aud {
			if a == want {
				return true
			}
		}
	case []any:
		for _, a := range aud {
			if s, ok := a.(string); ok && s == want {
				return true
			}
		}
	}
	return false
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd server && GOWORK=off go test ./cmd/server/ -run TestMcpAudience -v
```

Expected: PASS.

- [ ] **Step 5: Wire it into the MCP handler**

In `newMCPHandler`, before building the middleware, narrow the validator — and only when a resource URI is configured, so the local profile is untouched:

```go
	if resourceURI != "" && validator != nil {
		validator = mcpAudienceValidator(validator, resourceURI)
	}
```

Place it above the existing `web.AuthMiddleware(dev, validator)` call. Do not touch the validator passed to any other mount.

- [ ] **Step 6: Verify nothing else narrowed**

```bash
cd server && GOWORK=off go build ./... && GOWORK=off go test ./cmd/server/ -v
grep -n "mcpAudienceValidator" cmd/server/*.go
```

Expected: tests PASS; the grep shows the definition in `mcp_oauth.go` and exactly one call site, inside `newMCPHandler`.

- [ ] **Step 7: Commit**

```bash
git add server/cmd/server/mcp_oauth.go server/cmd/server/mcp_oauth_test.go server/cmd/server/mcp_mount.go
git commit -m "feat(mcp): require the MCP audience on the /mcp surface only"
```

---

### Task 8: Full-suite verification and lint

**Files:** none created — this is the gate before anything touches the cluster.

- [ ] **Step 1: Run the full server suite**

```bash
cd server && GOWORK=off go test ./...
```

Expected: PASS.

- [ ] **Step 2: Run every drift gate**

```bash
cd server && make gen-models-check gen-fakes-check gen-client-check gen-config-check
```

Expected: all exit 0.

- [ ] **Step 3: Run lint, vet, and the architecture gates**

```bash
cd server && make fmt vet lint method-check encapsulation-check sumtype-check
```

Expected: clean. If `gocyclo` flags `assembleDesiredState` after Task 2, split the resolver out rather than raising the limit or adding a nolint.

- [ ] **Step 4: Commit any formatting the gates applied**

```bash
git status --short
# only if make fmt changed files:
git add -A && git commit -m "chore: gate fixes for the MCP OAuth surface"
```

---

### Task 9: Keycloak realm runbook

Realm configuration is hand-applied in the admin console (the archistrator realm has always been), so it ships as a checked-in runbook rather than automation.

**Files:**
- Create: `docs/runbooks/2026-08-11-keycloak-mcp-dcr.md`

- [ ] **Step 1: Write the runbook**

Create `docs/runbooks/2026-08-11-keycloak-mcp-dcr.md` with these sections, each as an ordered checklist an operator can follow in the Keycloak admin console against realm `archistrator`:

1. **Client scope `archistrator-mcp`** — create it, set **Type: Optional** (not Default: a default scope would stamp the MCP audience onto webapp tokens and make the resource-server check decorative), add an **Audience** protocol mapper with Included Custom Audience `https://archistrator.capture-gtd.com/mcp`, "Add to access token" ON.
2. **Anonymous client-registration policies** (Realm Settings → Client Registration → Anonymous access policies):
   - **Trusted Hosts**: add `claude.ai`; **"Client URIs Must Match" ON**; **"Host Sending Client Registration Request Must Match" OFF** — Anthropic registers from `160.79.104.0/21`, which does not reverse-resolve to `claude.ai`, so host matching would reject every registration.
   - **Allowed Client Scopes**: must permit `archistrator-mcp`, or a registered client cannot request it and every token then fails the audience check.
   - **Consent Required**: leave ON — the founder should see a consent screen naming the client and its redirect host.
   - **Max Clients**: leave at 200 as the sprawl backstop.
3. **Discovery preconditions** — record the two commands and their required output:

```bash
curl -s https://keycloak.capture-gtd.com/realms/archistrator/.well-known/openid-configuration \
  | jq '{registration_endpoint, code_challenge_methods_supported, token_endpoint_auth_methods_supported}'
```

`registration_endpoint` must be present and `code_challenge_methods_supported` must include `S256` — Claude refuses to start the flow without the latter.

4. **Post-connection audit** — after the first connector is added, confirm exactly one new client exists in the realm and decode its issued access token to check `aud` contains `https://archistrator.capture-gtd.com/mcp`.
5. **Known future work** — enabling CIMD requires Keycloak ≥ 26.7.0 (keycloak#49730: `"none"` missing from `token_endpoint_auth_methods_supported`, which Claude requires before it will pick CIMD over DCR); the cluster runs 26.4.6.

- [ ] **Step 2: Commit**

```bash
git add docs/runbooks/2026-08-11-keycloak-mcp-dcr.md
git commit -m "docs: Keycloak realm runbook for MCP DCR"
```

---

### Task 10: Restrict the registration endpoint at the gateway

Spec §6.4. This is the **only** task that touches the `software` GitOps repo and the cluster-wide Keycloak front door — it is deliberately last and is independently revertible.

**Files (repo `~/mixofrealitystudio/software`):**
- Modify: the Keycloak gateway route / SecurityPolicy under `shared/security/helm/aiarch-keycloak/templates/`

- [ ] **Step 1: Read the current Keycloak route**

```bash
cd ~/mixofrealitystudio/software && cat shared/security/helm/aiarch-keycloak/templates/keycloak.yaml
grep -rn "HTTPRoute\|SecurityPolicy" k8s/argocd/auth/ shared/security/helm/
```

Establish where Keycloak's public route is defined before changing anything — it may live outside this chart. If it is not in this repo, stop and report; do not invent a location.

- [ ] **Step 2: Add the restriction**

Add an Envoy Gateway `SecurityPolicy` (or an HTTPRoute match with a client-IP authorization rule, whichever the installed Envoy Gateway version supports) restricting path prefix `/realms/archistrator/clients-registrations/` to source CIDR `160.79.104.0/21`. Everything else on the Keycloak host stays reachable — the browser login flow for every app runs through this same host, so an over-broad rule locks users out of every application.

- [ ] **Step 3: Verify before merge**

```bash
helm template shared/security/helm/aiarch-keycloak | grep -A 20 "clients-registrations"
```

Expected: the rendered policy scopes exactly the registration path prefix and nothing else.

- [ ] **Step 4: Commit**

```bash
git add shared/security/helm/aiarch-keycloak/
git commit -m "feat(keycloak): restrict anonymous client registration to Anthropic egress"
```

- [ ] **Step 5: Confirm login still works after ArgoCD syncs**

Load the archistrator webapp in a browser and complete a fresh login. A broken rule here breaks authentication for every app in the cluster, so this check is mandatory, not optional.

---

## Acceptance (run after deployment, in this order)

Each step isolates a different failure domain; do not skip ahead.

1. **Metadata is reachable and unauthenticated:**

```bash
curl -si https://archistrator.capture-gtd.com/.well-known/oauth-protected-resource/mcp | head -20
```

Expected: `200`, `Content-Type: application/json`, and a body whose `resource` is exactly `https://archistrator.capture-gtd.com/mcp`.

2. **The challenge is present:**

```bash
curl -si -X POST https://archistrator.capture-gtd.com/mcp \
  -H 'Content-Type: application/json' -d '{}' | grep -i 'HTTP/\|www-authenticate'
```

Expected: `401` carrying `WWW-Authenticate: Bearer resource_metadata="…/.well-known/oauth-protected-resource/mcp", scope="archistrator-mcp"`. A `200` with HTML means the gateway route did not land; a `401` with no header means the wrapper is not wired.

3. **The full OAuth flow, before touching claude.ai** — drive it with `mcp-remote` or the MCP Inspector against production. This separates resource-server bugs from connector behaviour: a failure here is ours.

4. **Add the connector**: claude.ai → Connectors → Add custom connector → `https://archistrator.capture-gtd.com/mcp`, leaving client ID and secret blank. Complete consent, list tools, call one **read-only** tool (`getSessionState`).

5. **Audit the realm**: exactly one new client registered; its issued token's `aud` contains the resource URI.

## Rollback

Every server-side piece is inert without configuration: clearing `ARCHISTRATOR_MCP_RESOURCE_URI` disables the metadata mount, the challenge, and the audience check in one move, leaving `/mcp` exactly as it behaves today. Removing the `mcpResourceUri` setting from the deployment model withdraws the two gateway routes on the next publish. Task 10 is reverted independently of everything else.
