# Deployment views with relationships — design

**Date:** 2026-08-07
**Status:** approved (founder, 2026-08-07)
**Slot affected:** `.aiarch/state/project.json` → slot 6 (`operationalConcepts`, customer label "Deployment & Operations Model")
**Repos affected:** `archistrator` (app), `archistrator-platform` (framework-go, framework-go-projectmodel, method-assets)

---

## 1. Problem

The deployment diagrams archistrator produces are boxes with no relationships. This is not a
rendering defect — it is a modelling gap that runs the full depth of the stack.

**The model has no edge concept.** `DeploymentTopology` in
`serviceContracts.projectStateAccess.$defs` carries exactly three fields —
`deliveryStyle`, `containers`, `environments` — and `DeploymentNode` carries
`children` / `infrastructureNodes` / `containerInstances` / `softwareSystemInstances`.
There is no type anywhere in the aggregate that could express "A talks to B".

**So the renderer draws none.** `webApp/src/components/flow/DeploymentFlow.tsx:288` passes
`edges={[]}` literally. It is not ignoring edges; there are none to pass.

**There is no frontend tier.** archistrator's own `archistrator-webapp` container is only ever
instanced as an nginx pod *inside the cluster*. There is no browser, no client device, no person,
and no agent-harness element anywhere in the model. The Structurizr idiom the founder asked for —
an app server that *delivers* a single-page application to a browser running on the customer's own
computer, alongside a mobile app doing the same over an API — has no representation at all.

**The gateway chain is doctrine that never reaches the picture.**
`platform-runtime-doctrine.md` #1 already states: "External callers reach in-process Clients
through Envoy Gateway." Every system built on the platform has the same front door — request hits
Envoy, Envoy performs OIDC against Keycloak, Envoy forwards to the app server. Because no template
carries that shape, every drafting agent re-derives it from scratch, and in archistrator's own
committed state it is simply absent: `Envoy Gateway` sits inside the *example operated-tenant*
namespace and `Keycloak` is filed under "External services", with nothing connecting either to the
archistrator server they actually front.

**Nothing gates any of it.** `framework-go/methodcheck/rules_deployment.go` carries ten `DEP-*`
rules covering container references, membership exclusivity, profile sets and coverage. None of
them concern edges, because until now there were no edges to check.

## 2. Goal

Deployment views that read like a Structurizr deployment view: labelled, technology-annotated
relationships between deployment elements; the frontend surfaces (SPA / mobile / agent harness)
shown where they actually run; and the platform's standard web-app front door present by
construction rather than by the drafting agent's memory.

Three founder rulings shape the solution:

1. **Edges are derived *and* authored.** Application edges derive from the committed System model
   so they cannot be forgotten or drift; only what derivation cannot know is authored.
2. **The web-app prototype is a hard, profile-gated rule.** Error severity where a gateway exists;
   exempt where one genuinely does not (the local profile).
3. **The prototype is a typed baseline in framework-go**, with the method asset and the gate
   expectations both generated from that one definition.

## 3. Schema changes

All shape lives in `serviceContracts.projectStateAccess.$defs` in `project.json`, which owns the
contracts; `modelgen` regenerates `server/internal/resourceaccess/projectstate/contract.gen.go`
from it. The platform mirrors in `framework-go/methodcheck/project.go` and the tolerant subset in
`framework-go-projectmodel/deployment.go` are updated to match.

### 3.1 Element identity

Every deployment element gains a **`key`**, unique within its environment. This is Structurizr's
identifier mechanism and it is what makes an element addressable as an edge endpoint.

| Type | New field | Note |
|---|---|---|
| `DeploymentNode` | `key` | |
| `InfrastructureNode` | `key` | |
| `SoftwareSystemInstance` | `key` | |
| `ContainerInstance` | `key` | instance identity — distinct from the existing `containerKey`, which says *which* container is instanced |

`ContainerInstance` needs its own key precisely because one container may be instanced in more
than one node (see §6.2), and the two instances are distinct edge endpoints.

### 3.2 Relationships

```jsonc
// $defs.DeploymentRelationship
{
  "from":       "string",   // element key within this environment
  "to":         "string",   // element key within this environment
  "label":      "string",   // "Makes API calls to"
  "technology": "string",   // "JSON/HTTPS" — rendered as the [bracketed] second line
  "mode":       "CallMode"  // reuses the existing sync/async enum
}
```

Authored relationships hang off the **environment**, not the topology:

```jsonc
// $defs.DeploymentEnvironment  (added)
"relationships": [ DeploymentRelationship ],
"persons":       [ DeploymentPerson ],
"computed":      DeploymentEnvironmentComputed   // compute-at-read only
```

Per-environment is the only correct placement: the local profile genuinely has no Envoy and no
Keycloak, so a global relationship list could not describe both profiles truthfully.

### 3.3 Persons

```jsonc
// $defs.DeploymentPerson
{ "key": "string", "name": "string", "description": "string" }
```

The actor tier the founder asked to be edge-checked. Kept deliberately minimal — a person is a
label and an edge source, not a modelling subsystem.

### 3.4 Typed classification

Two closed enums replace what would otherwise be string-matching on names. This is what lets the
gate find "the gateway" without grepping for `"Envoy"`.

```jsonc
// $defs.ContainerSurface — on DeployContainer.surface
"spa" | "mobile" | "agentHarness" | "cli" | "service"

// $defs.ElementRole — on InfrastructureNode.role and SoftwareSystemInstance.role
"gateway" | "identityProvider" | "database" | "objectStore" | "messaging"
| "observability" | "agentHarness" | "sourceControl" | "paymentGateway" | "other"
```

`service` and `other` are the defaults, so every existing document parses unchanged.

### 3.5 Compute-at-read block

```jsonc
// $defs.DeploymentEnvironmentComputed
{ "derivedRelationships": [ DeploymentRelationship ] }
```

Never committed. Populated on the read path only, exactly as `Network.Computed` is today
(`server/internal/manager/systemdesign/systemdesignmanager.go:2677`).

## 4. Where edges come from

### 4.1 Derived — one Go implementation

A pure function in `framework-go` maps the committed System relationships onto deployment
elements, per environment:

1. **Component → element.** A Client / Manager / Engine / ResourceAccess component maps to the
   container that packages it (`DeployContainer.components`), then to that container's instances
   in this environment. A Resource component maps to the infrastructure node or software-system
   instance whose **name matches** — reusing the name-match convention `DEP-RESOURCE-PRESENT`
   already established, so no new authoring burden and no second convention.
2. **Utilities are excluded.** Consistent with the existing architecture-diagram convention that
   utilities carry no lines.
3. **Self-edges dropped.** Both endpoints resolving to the same instance produces nothing.
4. **Deduped**, with the System relationship's label carried onto the edge.

For archistrator this collapses 73 component relationships into precisely the interesting
picture — SPA → server, server → Postgres, server → Temporal, server → GitHub, server → Stripe —
none of which anyone has to author, and none of which can drift from the architecture.

### 4.2 Authored — only what derivation cannot know

Envoy, Keycloak and the browser are not System components, so no derivation can produce them.
These are authored per environment:

- `nginx pod → browser SPA instance` — "Delivers the SPA to the architect's web browser [HTTPS]"
- `browser SPA instance → Envoy` — "Makes API calls to [JSON/HTTPS]"
- `Envoy → Keycloak` — "Authenticates the request against [OIDC]"
- `Envoy → archistrator-server` — "Forwards the authenticated request to [HTTP]"

### 4.3 Delivery seam

Derivation runs as a second operation on `DesignHealthEngine` — which has exactly one today
(`EvaluateDesignHealth`) — backed by the framework-go function. `systemDesignManager` calls it
during the existing compute-at-read enrichment and attaches the result to the served
`operationalConcepts` model under `environments[].computed.derivedRelationships`.

The webApp therefore renders `authored ∪ derived` with no mapping logic of its own. One
implementation in Go serves both the gate and the picture; there is no Go/TypeScript pair to
drift apart.

## 5. Gate rules — `framework-go/methodcheck/rules_deployment.go`

| Rule | Severity | Check |
|---|---|---|
| `DEP-KEY-UNIQUE` | Error | element keys are unique within an environment |
| `DEP-EDGE-REF` | Error | every relationship endpoint resolves to a declared element key in that environment |
| `DEP-EDGE-ISOLATED` | Error | every container instance, infrastructure node, software-system instance and person has ≥1 edge (authored or derived) |
| `DEP-FRONTEND-PRESENT` | Error | every environment instances ≥1 frontend surface — a container with `surface` ∈ {`spa`, `mobile`, `cli`}, or any element with `agentHarness` |
| `DEP-EDGE-GATEWAY` | Error | in any environment carrying a `gateway` element: frontend→gateway, gateway→identityProvider, and gateway→app-server edges are all present |

`DEP-EDGE-GATEWAY` is gated on the *presence of a gateway element*, per the founder ruling. The
local profile binds `127.0.0.1:8877` directly with no Envoy and no Keycloak; requiring the chain
there would encode a fiction into the model. Deviations elsewhere go through the existing waiver
mechanism rather than a weakened rule.

`DEP-EDGE-ISOLATED` counts derived edges, which is why derivation must live in the platform
alongside the rules: without it, every server→Postgres edge would have to be hand-authored purely
to satisfy the gate.

## 6. The web-app prototype

### 6.1 One definition, two consumers

`WebAppBaseline()` in `framework-go` returns the canonical typed fragment:

```
Person(user)
  └─→ DeploymentNode "Client device"
        └─ DeploymentNode "Web browser"  ──▶ ContainerInstance(spa)
                                               │
                                               ▼  Makes API calls to [JSON/HTTPS]
                                          InfrastructureNode(role=gateway, "Envoy Gateway")
                                               ├─▶ InfrastructureNode(role=identityProvider, "Keycloak")   [OIDC]
                                               └─▶ ContainerInstance(app server)                            [HTTP]
```

plus the agent-harness variant, in which the frontend surface is an `agentHarness` element
reaching the same gateway rather than a browser-hosted SPA.

Two consumers, no second definition:

1. `method-assets` materializes it into a `webapp-deployment-prototype` asset, and
   `the-method-operational-concepts` instructs the drafting agent to **start from** it rather than
   re-derive the front door.
2. The `DEP-*` rules read the same baseline for their expectations, so template and gate cannot
   drift apart.

### 6.2 Static delivery is an infrastructure node, not a second SPA instance

**Revised during implementation.** The design first proposed instancing the SPA container twice —
once in the nginx pod (delivery) and once in the browser (execution) — mirroring the reference
diagram. Building it showed why that is wrong here: derivation resolves a component to *every*
instance of its container, so the WebClient→Manager relationships would derive an edge from the
**nginx pod** to the server, asserting that the static-asset server calls the API. It does not.

Instead the SPA container is instanced **once**, in the browser, and the static delivery mechanism
is an `InfrastructureNode` on the serving node — `Static asset server (nginx)` in cloud,
`Embedded SPA assets (go:embed)` in local — with an authored "Delivers the SPA to the architect's
browser" edge to the browser instance. Derivation then produces exactly one SPA→server edge, from
the browser where the code actually runs.

This also removes the constraint that motivated the original choice. `DEP-GRAPH-IDENTITY` compares
the set of **component names** covered per profile, not the set of containers
(`checkCrossProfileCoverage`), and WebClient is covered in every profile because the SPA container
is instanced in each. Container identity across profiles holds either way.

## 7. Renderer

`DeploymentFlow.tsx` stops passing `edges={[]}`:

- Dashed Structurizr-style arrows carrying a two-line label — `Label` over `[Technology]`.
- Connection handles on all four node types (`deployGroup`, `deployContainer`, `deployInfra`,
  `deployExternal`), with edges routed across group boundaries.
- A new client-side band on the left of the canvas: person → client device → browser → SPA, and
  the agent-harness node beside it — mirroring the reference diagram's layout.
- Person nodes reuse the existing `PersonNode` component.

Edge sourcing in the view is `authored ∪ computed.derivedRelationships`, both already present on
the served model.

## 8. archistrator's own state

Slot 6's `deployment` block is rewritten to satisfy all five rules. Current state and its gaps:

- **cloud** — `Envoy Gateway` sits inside the *example operated-tenant* `gtd namespace`, fronting
  nothing; `Keycloak` is filed under "External services" with no relationship to it. The
  archistrator server has no modelled front door at all. No browser, no person, no harness.
- **test** — a flat in-process node; no edges, no frontend.
- **local** — a laptop node plus an empty `archistrator-server (child process)` child node; the
  `archistrator-webapp` container is not instanced at all.

The rewrite adds element keys throughout; a person and client-device/browser tier; `surface` on
each container and `role` on each infrastructure node and external system; an `agentHarness`
element for the `claude -p` runner; the authored Envoy→Keycloak→server chain in the cloud profile
(and deliberately not in local); and instances the SPA container in every profile so container
identity holds.

## 9. Verification

- `go test ./...` in framework-go (new rule tests, per-rule negative fixtures in the established
  `rules_deployment_test.go` style) and in the app server.
- `npm run check` in `webApp` (typecheck, lint, format, `node --test` unit tests).
- The uitests deployment specs updated for the new edges.
- The design-health / methodcheck gate run against archistrator's own rewritten `project.json` —
  it must come back clean, since archistrator dogfoods its own rules.
- archistrator served locally for founder review of the rendered diagram.

## 10. Out of scope

- Other projects' committed states (gtdapp, greenfield). The schema additions are optional and
  every existing document parses unchanged; those projects surface the new rules as findings and
  are re-drafted separately.
- Structurizr DSL export of the deployment view. The DSL render-on-read already exists for the
  static model; extending it to deployment relationships is a follow-on.
- Mobile as a real surface for archistrator. The `mobile` enum value exists so customer projects
  can use it; archistrator itself ships an SPA and an agent harness.
