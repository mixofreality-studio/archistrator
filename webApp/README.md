# archistrator webApp

The human-operator SPA for archistrator's **UC1 system-design** slice. It lets a
signed-in user drive a Phase-1 system design for one project: start the design,
co-author the seven ordered Method artifacts through the human review gate, and
render committed artifacts. Login itself happens at the Envoy edge (Keycloak
OIDC) — the SPA only confirms the session via `GET /api/userinfo`.

This is a **Method product** — there is **no BDD/Gherkin**. Tests are plain,
regression-first component/hook tests (vitest + Testing Library).

## Stack

React 19 (strict TS) · MUI 7 · TanStack Query · React Router 7 · Vite · openapi-fetch.

## API client (generated)

The typed client is generated from the server's **frozen** OpenAPI spec
(`../server/api/openapi.yaml`, the source of truth) with `openapi-typescript`:

```bash
npm run gen:api   # → src/api/schema.ts (committed; regenerate when the spec changes)
```

`src/api/client.ts` wraps it with `openapi-fetch`; `src/api/systemDesign.ts`
exposes one typed function per server op.

## Auth (edge-OIDC, GTD parity)

The SPA runs **no** client-side OIDC. The Envoy edge handles the Keycloak
authorization-code redirect login, sets the session cookie, and forwards the
validated access token to the Go server (which self-validates it into a
principal). On mount the SPA probes `GET /api/userinfo` (same-origin, so the edge
cookie rides along; the server returns the principal's claims):

- **200** → authenticated; the app renders with the signed-in user.
- **401** → no/expired session; the SPA reloads, and the edge answers that
  top-level navigation with the OIDC redirect to Keycloak (the JSON probe itself
  gets a 401 via the edge `denyRedirect` rule, not a CORS-breaking 302).

`VITE_AUTH_MODE` only toggles the "DEV AUTH" badge (see `.env.example`):

- **`dev`** (default): no edge in front; the Go server injects an env-gated dev
  principal, so `/api/userinfo` returns it — locally runnable with no IdP.
- **`keycloak`**: real edge-OIDC deployment.

## Develop

```bash
cp .env.example .env            # adjust VITE_AUTH_MODE as needed (defaults to dev)
npm install
npm run dev                     # Vite proxies /api → http://localhost:8888 (the Go server)
```

Run the archistrator Go server in dev-mode auth alongside it for a full local loop.

## Verify

```bash
npm run gen:api && npm run build && npm run lint   # all clean (TS strict, no eslint errors)
npm run test                                        # vitest component/hook tests
```

## Build (container)

```bash
docker build -t archistrator-webapp .
```

The committed `src/api/schema.ts` lets the image build without the server spec
(which lives outside the Docker context). nginx serves the SPA; `/api/*` is
routed to `archistrator-server` by an Envoy HTTPRoute in-cluster, not by nginx.

## Layout

| Path | Purpose |
|------|---------|
| `src/api/` | Generated schema + typed client + per-op service + view readers |
| `src/auth/` | Session gate: `/api/userinfo` probe (`UserContext`) + user type |
| `src/hooks/` | TanStack Query polling + mutation hooks |
| `src/components/` | Co-author gate, draft/render panels, app shell |
| `src/screens/` | Project gate, workspace |
| `src/navigation/` | Routes |
| `src/constants/UIIdentifiers.ts` | Centralized `data-testid`s |
