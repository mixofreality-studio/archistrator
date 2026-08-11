# Production MCP OAuth for `/mcp` — design

**Date:** 2026-08-11
**Status:** Approved by founder 2026-08-11
**Supersedes:** the "production OAuth for `/mcp`" earmark in `docs/superpowers/specs/2026-07-13-mcp-apps-design.md` §4/§7

## 1. Goal

Make `https://archistrator.capture-gtd.com/mcp` addable as a **custom connector on claude.ai** — the founder pastes the URL, Claude discovers the authorization server, registers itself, the founder consents on a Keycloak screen, and the ~35 generated manager tools become callable from a Claude conversation.

Nothing about this is reachable today, for two independent reasons (recon 2026-08-11):

- **`/mcp` is not routed at the gateway.** `gatewayRoutes()` (`server/internal/resourceaccess/operatedruntime/operatedruntimeaccess.go:1659`) renders exactly four HTTPRoutes: `/`, `/api`, `/healthz`, `/readyz`. A request to `/mcp` matches the `/` prefix, lands on the nginx SPA behind Envoy's OIDC redirect filter, and returns HTML. The Go MCP transport mounted at `/mcp` (`server/cmd/server/hooks.go:551`) is never reached from outside the cluster.
- **No OAuth metadata exists anywhere.** `/mcp` sits behind `web.AuthMiddleware` (`server/internal/client/web/middleware.gen.go`), which validates a Keycloak bearer token in prod and denies everything when no validator is configured. Nothing serves `/.well-known/oauth-protected-resource` and nothing emits a `WWW-Authenticate` challenge, so an MCP client has no way to learn where the authorization server is.

## 2. Rulings taken during design

| # | Question | Ruling |
|---|---|---|
| R1 | How is the token bound to this resource, given Keycloak has no RFC 8707? | **Scope → audience mapper.** A dedicated optional client scope carries an Audience protocol mapper; the resource server requires that audience. |
| R2 | Who gets the new gateway routes? | **Per-app opt-in declared in the deployment model.** Archistrator is the only app that declares it. |
| R3 | Which client-registration mechanism? | **DCR** (RFC 7591). Founder ruling after weighing CIMD and pre-registration; see §3. |
| R4 | Bump Keycloak? | **No — separate wave.** DCR works on the deployed 26.4.6. |
| R5 | Gate spending operations behind a step-up scope? | **Earmarked, not in this slice.** See §9. |

### 2.1 Consequence worth stating up front

Every piece of this design lands **inside the archistrator repo**. `RuntimeDesiredState` is generated from `project.json`'s `.serviceContracts` (`contract.gen.go`), the MCP mount and its neighbours are composition-root-only files outside `internal/`, and the audience check is a decorator in that same composition root. **No `framework-go` change, no platform release, no pin coordination.**

## 3. Why DCR, and what it costs

The MCP 2025-11-25 spec offers three registration mechanisms; all three were evaluated against this deployment.

- **CIMD** is what the spec and Anthropic both prefer, and it was the founder's opening preference. It is not viable on the deployed Keycloak: CIMD landed experimental in 26.6, and until [keycloak#49730](https://github.com/keycloak/keycloak/issues/49730) (fixed in **26.7.0**, 2026-07-09) Keycloak omitted `"none"` from `token_endpoint_auth_methods_supported`. Claude selects CIMD **only** when the authorization-server metadata advertises *both* `client_id_metadata_document_supported: true` *and* `"none"` — otherwise it falls back to DCR. The cluster runs **26.4.6** with operator **26.4.2**. `cimd` is also not classified into Keycloak's supported or preview tier, so it carries no cross-release stability promise.
- **Pre-registration** (one hand-made realm client, client ID pasted into the connector dialog) needs no experimental feature and no upgrade, and is the spec's named choice for "client and server have an existing relationship."
- **DCR** was chosen. Its costs are real and are mitigated in §6:
  - Keycloak's anonymous registration is **disabled out of the box** — the Trusted Hosts policy ships with an empty allowlist ("anonymous client registration is de-facto disabled"). It requires the same order of realm configuration as the alternatives.
  - Claude **registers a fresh client on every new connection**. Anthropic's own guidance is to prefer CIMD or Anthropic-held credentials over DCR for this reason. The Max Clients policy (200/realm) is the backstop, and the sprawl is bounded in practice by there being one operator.
  - It exposes an anonymous registration endpoint on the public internet (mitigated in §6.4).

**The registration mechanism is orthogonal to the rest of this design.** §4, §5, §7 and §8 are byte-identical under DCR, CIMD, and pre-registration. Switching later is realm configuration plus what is typed into the connector dialog — no resource-server change. Enabling CIMD after a future 26.7.x bump is therefore a cheap follow-on, and it is also what would let **Claude Code** reach the remote server, since Claude Code identifies itself with its own Client ID Metadata Document and a loopback redirect rather than through Anthropic-held credentials.

## 4. Gateway routes

`gatewayRoutes()` gains two `routeSpec` entries, both **`browserFacing: false`**:

| Route | Path | Backend | Why not browser-facing |
|---|---|---|---|
| `mcp` | `/mcp` | server | The OIDC SecurityPolicy installs an authorization-code redirect filter. A non-browser MCP client cannot follow it; the connection would fail before any token was presented. The Go server validates the bearer token itself. |
| `oauth-prm` | `/.well-known/oauth-protected-resource` | server | Discovery **must** answer unauthenticated. The PathPrefix match also covers the `/.well-known/oauth-protected-resource/mcp` sub-path form. |

Both routes are gated on a new `McpSurface bool` field on `RuntimeDesiredState`, declared per-app in the deployment model and threaded through by the operations manager — mirroring the existing per-app `SelfManaged bool`. `RuntimeDesiredState` is generated, so the field is added to the `operatedruntime` service contract in `project.json` and regenerated; the exact deployment-model slot the operations manager reads it from (a `Deployment.Settings` entry versus a first-class field) is settled during planning recon, the same way the MCP-Apps spec deferred its `ui` annotation shape. Apps that do not declare it render exactly the four routes they render today, so no generated app ever advertises protected-resource metadata for an endpoint its server does not mount (app-generator emits an MCP *client* SDK, not a server mount).

`securityPolicyData.TargetRouteNames` continues to list only the browser-facing routes, so neither new route is touched by the OIDC policy. The render is pinned by the existing goldens in `server/internal/resourceaccess/operatedruntime/testdata/golden/production/archistrator-gateway-routes.yaml`.

## 5. Go composition root — `server/cmd/server/mcp_oauth.go`

A new composition-root-only file beside `mcp_mount.go` and `mcp_apps.go`, wired from the `hooks.go` `ExtraMounts` seam. Three pieces:

### 5.1 Protected resource metadata (RFC 9728)

Served unauthenticated at `/.well-known/oauth-protected-resource` and `/.well-known/oauth-protected-resource/mcp`, as a config-templated constant:

```json
{
  "resource": "https://archistrator.capture-gtd.com/mcp",
  "authorization_servers": ["https://keycloak.capture-gtd.com/realms/archistrator"],
  "scopes_supported": ["archistrator-mcp"],
  "bearer_methods_supported": ["header"]
}
```

- `resource` **must match the URL the user types into Claude exactly**, including the path — so it is one new configuration value, read in `ExtraMounts` alongside the `ARCHISTRATOR_WEBAPP_ORIGIN`/`ARCHISTRATOR_WEBAPP_ASSET_VERSION` precedent, not a derivation from the request host.
- `authorization_servers` reuses the **already-configured** Keycloak issuer (`products/archistrator/helm/archistrator-server/values.yaml` → `issuer`). Claude uses the first entry and does not fall back, so exactly one is listed.
- Both paths are mounted in both profiles; the values are config-driven, and on the local profile dev-mode auth means the challenge path below is simply never exercised.

### 5.2 `WWW-Authenticate` challenge, scoped to `/mcp`

A `ResponseWriter` wrapper around the MCP handler that stamps, when and only when a 401 is written:

```
WWW-Authenticate: Bearer resource_metadata="https://archistrator.capture-gtd.com/.well-known/oauth-protected-resource/mcp", scope="archistrator-mcp"
```

Same response-interception shape as the existing `log5xxResponses` (`http5xxlog.go`). Deliberately **not** applied to `/api/v1` — the REST surface has no business advertising MCP resource metadata. Claude does not honour a `WWW-Authenticate` header on a 200, and the header-based path avoids the extra round-trips of well-known probing. The `scope` parameter is what makes Claude request `archistrator-mcp`, which is what makes §5.3 pass.

### 5.3 Audience-checking validator decorator

A `security.Validator` decorator wrapping the configured Keycloak validator, applied **only** to the `/mcp` mount's `AuthMiddleware`:

```go
type mcpAudienceValidator struct {
    inner security.Validator
    want  string // the canonical MCP resource URI
}
```

It delegates, then requires `want` to appear in the verified token's `aud` claim, returning `security.ErrUnauthenticated` otherwise.

Two things make this the right shape rather than a shortcut:

- **It must be per-surface.** `AuthMiddleware` shares one validator across `/api/v1` and `/mcp`. Requiring the MCP audience on the shared validator would reject every webapp token, taking the SPA down. The decorator is applied at one mount only.
- **It needs no platform change.** `mapPrincipal` carries every claim through verbatim in `Principal.Claims` (`framework-go-infrastructure-keycloak/validator.go`), so the decorator reads `aud` from the already-verified claim set without re-parsing the JWT and without modifying the platform validator (whose doc comment states, correctly for the REST surface, that "Audience is not checked").

Keycloak emits `aud` as either a string or an array depending on how many audiences are present; both forms are handled.

**Earmark:** if a second app ever exposes an MCP surface, promote this into `framework-go-infrastructure-keycloak` as a `Config.RequiredAudience` and construct two validator instances instead.

## 6. Keycloak realm configuration (26.4.6, unchanged)

The archistrator realm is hand-managed in the admin console (see the comment in `products/archistrator/helm/archistrator-gateway-routes/values.yaml`), so this ships as a runbook alongside the code, not as automation.

### 6.1 Client scope `archistrator-mcp`

Create client scope `archistrator-mcp`, **type Optional**, with an **Audience** protocol mapper whose Included Custom Audience is the canonical MCP resource URI, included in the access token.

Optional — not default — is load-bearing. A default scope would stamp the MCP audience onto webapp tokens too, which would make the §5.3 check decorative and reintroduce exactly the cross-surface token reuse it exists to prevent. As an optional scope it is applied only when requested, and the §5.2 challenge is what requests it.

### 6.2 Anonymous client-registration policies

- **Trusted Hosts**: add `claude.ai`; **"Client URIs Must Match" ON**, **"Host Sending Client Registration Request Must Match" OFF**. Anthropic's registration calls originate from `160.79.104.0/21`, which does not reverse-resolve to `claude.ai`, so matching on the requesting host would reject every registration. Matching on the client's redirect/client URIs is the check that actually binds the registration to Claude.
- **Allowed Client Scopes**: must permit `archistrator-mcp`, or DCR-registered clients cannot request it and every token fails the audience check.
- **Consent Required**: left on. Anonymous DCR forces `consentRequired` on registered clients, which is the desired behaviour — the founder sees a consent screen naming the client and the redirect host.
- **Max Clients**: left at the 200 default as the sprawl backstop.

### 6.3 Discovery preconditions to verify

The realm's OIDC discovery document must advertise `registration_endpoint` and `code_challenge_methods_supported: ["S256"]`. Claude sends `code_challenge_method=S256` on every authorization request and, per spec, **refuses to proceed** if the metadata does not advertise PKCE support.

### 6.4 Hardening: restrict the registration endpoint

Restrict `/realms/archistrator/clients-registrations/*` at the Envoy gateway to Anthropic's published egress range `160.79.104.0/21`, so the anonymous endpoint is not reachable by the open internet. This lives in the shared security chart (`software/shared/security/helm/aiarch-keycloak`), not in the archistrator app chart, and is the one change in this design that touches the cluster-wide Keycloak front door.

## 7. Configuration summary

| Value | Source | Notes |
|---|---|---|
| MCP resource URI | new env, read in `ExtraMounts` | Must equal the connector URL exactly. |
| Authorization server issuer | existing `issuer` value | Already configured for the validator. |
| `McpSurface` | deployment model → `RuntimeDesiredState` | Archistrator only. |

## 8. Testing

- **Unit (`server/cmd/server`)**: protected-resource-metadata JSON golden; `WWW-Authenticate` present on 401 and absent on 200; audience decorator matrix — string `aud`, array `aud`, missing `aud`, wrong `aud`, valid `aud`; the decorator is applied to `/mcp` and *not* to `/api/v1`.
- **Render (`operatedruntime`)**: gateway-route goldens updated to cover both new routes, plus an assertion that neither appears in `securityPolicyData.TargetRouteNames` — the regression that would silently put the OIDC redirect filter back in front of `/mcp`.
- **Integration**: drive the real OAuth flow against production with `mcp-remote` / the MCP Inspector **before** touching claude.ai. This isolates resource-server bugs from connector-side behaviour; a failure here is ours, a failure only in Claude is theirs.
- **Manual acceptance**: add the custom connector on claude.ai, complete consent, list tools, call one read-only tool (`getSessionState`). Then confirm in the Keycloak admin console that exactly one client was registered and that its issued token carries the expected `aud`.
- **Explicit non-goal**: no automated test drives claude.ai.

## 9. Security notes

- **A connected Claude can spend money.** The connector's token reaches every tool on the MCP surface, including `executeNextActivity`. The in-UI confirm step from the MCP-Apps spec (§8.2) covers app-initiated clicks, not model-initiated `tools/call`. The spec's step-up mechanism — 403 with `error="insufficient_scope"` and a second scope for spending operations — is the correct eventual answer. **Earmarked, not built here** (R5): one operator, one connector, and every mutating op already carries server-side authorization on the principal.
- **Audience validation is the boundary this design adds.** Before it, any archistrator-realm token would be accepted at `/mcp`. After it, only tokens minted for the MCP resource are.
- **The registration endpoint is the new exposed surface**, mitigated by §6.2's URI matching, §6.4's IP restriction, and the Max Clients cap.
- **Token passthrough remains forbidden** — the MCP surface calls the same in-process managers as REST and never forwards the inbound token to an upstream API.

## 10. Non-goals

- Keycloak 26.4.6 → 26.7.1 and operator 26.4.2 → 26.7.x (separate wave, R4).
- CIMD enablement (`--features=cimd`) — cheap follow-on once the bump lands; also what unlocks Claude Code against the remote server.
- app-generator replication: generated apps gain neither the MCP server mount nor the metadata surface here.
- Step-up authorization for spending operations (R5, §9).
- Any change to the stdio `cmd/aiarch-state-mcp` server.
