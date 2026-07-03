# Structurizr-shaped deployment model + validation — design

Reshape the stored deployment topology (`OperationalConcepts.deployment` in
`project.json`) to mirror Structurizr's deployment-view metamodel, make it
**validate** against the committed System components, and enrich it so the
diagram reflects the **real** archistrator deployment — a single
`archistrator-server` container that holds most of the Method components, with
webapp, Postgres, and external infrastructure around it.

Source refs: Structurizr deployment view (`docs.structurizr.com/dsl/cookbook/
deployment-view`, `github.com/structurizr/structurizr`); real topology from the
Helm charts under `…/software/products/archistrator/helm/` and
`…/software/k8s/`.

## Problem (grounded in real data)

The current model (`server/internal/resourceaccess/projectstate/models_phase1.go`,
hand-mirrored in `webApp/src/api/models.ts`) is:

```
DeploymentNode { name; technology; children[]; instances[] ContainerInstance }
ContainerInstance { componentId; note }
```

Gaps vs. reality and vs. what we want:

1. **No container level.** In reality nearly all Method managers/engines/
   resource-access seams run inside **one Go binary** (`archistrator-server`,
   ×2 replicas, distroless). The model puts component instances *flat* in the
   namespace, so the diagram can't show "these components co-locate in the
   server; those don't." (Confirmed from `helm/archistrator-server/templates/
   deployment.yaml` — API + Temporal client + **embedded** worker + every seam
   in one container; there is **no** separate worker Deployment.)
2. **No infrastructure-node concept.** Non-component infra (Envoy Gateway,
   shared Temporal cluster, CloudNativePG Postgres, per-project GitHub repos,
   GitHub Actions runners, Anthropic API, Keycloak) currently has to be faked as
   component instances or omitted.
3. **No `instances` multiplier**, `description`, or `tags` — Structurizr staples
   (e.g. the server is ×2; Postgres ×1).
4. **No referential validation.** The `DEP-INSTANCE-EXIST` predicate is
   documented in comments but unimplemented. The live data already contains a
   **dangling ref**: deployment references `project-manager`, which is **not**
   one of the 38 committed System components.
5. **Generation is aspirational.** The System-design author prompt
   (`server/internal/manager/systemdesign/prompts.go`) tells the LLM to reference
   components **by name** and "the server resolves the name" — but no name→id
   resolution exists in Go. Decode (`codec.go`) unmarshals straight into structs
   whose only ref field is `json:"componentId"`.

## Design

### 1. Model — mirror Structurizr's deployment metamodel

Go structs (source of truth) + hand-mirror in `models.ts`. A `DeploymentNode`
becomes a recursive container holding four child kinds, three of them
Structurizr-native; the fourth (`componentInstances`) is archistrator's
adaptation (see granularity note).

```go
type DeploymentEnvironment struct {
    Profile DeploymentProfile `json:"profile"`
    Title   string            `json:"title"`
    Nodes   []DeploymentNode  `json:"nodes"`
}

type DeploymentNode struct {
    Name                string               `json:"name"`
    Technology          string               `json:"technology"`   // optional
    Description         string               `json:"description"`   // optional
    Instances           int                  `json:"instances"`     // multiplier; 0/absent ⇒ 1
    Tags                []string             `json:"tags"`          // optional
    Children            []DeploymentNode     `json:"children"`
    InfrastructureNodes []InfrastructureNode `json:"infrastructureNodes"`
    ComponentInstances  []ComponentInstance  `json:"componentInstances"`
}

// InfrastructureNode — non-component infra OR an external software system
// (gateway, DB engine, queue, GitHub, Anthropic, Keycloak). Self-describing;
// references nothing; NOT validated against System components.
type InfrastructureNode struct {
    Name        string   `json:"name"`
    Technology  string   `json:"technology"`
    Description string   `json:"description"`
    External    bool     `json:"external"`   // draws as an external/dashed node
    Tags        []string `json:"tags"`
}

// ComponentInstance — an instance of a System component deployed in this node.
// `Component` is the exact System component NAME (human-authored, matches the
// architecture view); resolved to a component id + layer downstream and
// validated. Structurizr's containerInstance analog at component granularity.
type ComponentInstance struct {
    Component  string   `json:"component"`   // System component NAME (was: componentId)
    Note       string   `json:"note"`
    Tags       []string `json:"tags"`
}
```

**Granularity note (the key adaptation).** Structurizr's `containerInstance`
references a *container*; archistrator's deployable detail is *components* (all
in one server binary). Per the review decision, the deployment leaf stays a
**component** instance so every Method component remains visible — but nested
inside real container `DeploymentNode`s (`archistrator-server`,
`archistrator-webapp`, `archistrator-postgres`). We deliberately **fold
Structurizr's `softwareSystemInstance` into `infrastructureNode`** (`external:
true`), because archistrator's model has exactly one software system; there is no
external-software-system registry to instance.

**Reference by name, not id.** Wire field becomes `component` (the System
component name), matching the existing prompt and the architecture view. This
implements the currently-missing name→id resolution: the webApp adapter and the
Go validator both resolve `component` → `{id, layer}` against the committed
System slot (id = `Slug(name)`; adapter already builds this map in
`adapters.ts:459-461`). Decode stays dumb (stores the name); resolution +
validation happen where the System slot is in hand.

### 2. Reality mapping — archistrator's deployment data

Rewrite `project.json .slots['6'].model.deployment` to the new shape and the real
topology. **cloud** environment:

```
DeploymentNode "Mixofreality Kubernetes Cluster" (k8s)
├─ DeploymentNode "gtd namespace" (k8s-namespace)
│   └─ InfrastructureNode "Envoy Gateway" (Gateway API; edge TLS + Keycloak OIDC)
├─ DeploymentNode "archistrator namespace" (k8s-namespace)
│   ├─ DeploymentNode "archistrator-server" (Go · distroless) instances:2
│   │     componentInstances: mcp-client, scheduler-client, ALL managers,
│   │       ALL engines, ALL resourceAccess, ALL utilities
│   ├─ DeploymentNode "archistrator-webapp" (nginx · React SPA) instances:2
│   │     componentInstances: web-client
│   └─ DeploymentNode "archistrator-postgres" (CloudNativePG · Postgres 16) instances:1
│         componentInstances: the Postgres-backed Resource components
│         (operated-system-state, billing-state, usage-log)
├─ DeploymentNode "temporal namespace" (k8s-namespace · shared)
│   └─ InfrastructureNode "Temporal Cluster" (frontend/history/matching/worker; Postgres-backed)
└─ InfrastructureNode "Keycloak" (OIDC realm archistrator) [external:true]

Sibling top-level nodes:
DeploymentNode "User's GitHub Account" [external]
├─ InfrastructureNode "Per-project state repos (aiarch-<id>)" (git JSON + ref CAS)
└─ InfrastructureNode "GitHub Actions runners" (construction pipeline)
InfrastructureNode "Anthropic Messages API" [external] (LLM workers)
```

The **test** environment ("in-process, ephemeral") keeps a single
`test process` node holding every component (externals become in-process
stubs) — same components as cloud, per the cross-profile invariant.

Placement rule (layer → node): `web-client` → webapp; other Clients + Managers +
Engines + ResourceAccess + Utilities → server; Resources → postgres; everything
non-component → infrastructureNode. Exact per-component assignment is finalized
against the committed System slot during implementation and is a review point.

### 3. Validation — implement DEP-INSTANCE-EXIST

A pure function over `(System, DeploymentTopology)`:

- **Referential integrity:** every `ComponentInstance.component` resolves to a
  committed System component (by name → `Slug`). Unresolved ⇒ error. (Catches
  today's `project-manager`.)
- **Coverage (warn):** every System component appears in ≥1 environment; every
  environment's component set is identical across cloud/local (the existing
  cross-profile invariant), and the test env includes them all.

Home: the Method-conformance gate `TestMethod` (`server/internal/arch_test.go`)
and `cmd/validate` (so CI `server-checks.yml` enforces it). Infra/external nodes
are exempt (they reference nothing).

### 4. Renderer

Builds on the just-shipped layered layout (`DeploymentFlow.tsx` /
`DeploymentNodes.tsx`). Changes:

- **Deeper nesting:** cluster → namespace → container → component-layer-rows. The
  layer-row bucketing already implemented runs at the container level (the
  `archistrator-server` node), so its ~30 components render as the labeled layer
  rows we just built.
- **`infrastructureNode` node type:** a distinct style (no layer color; a subtle
  "infrastructure" / external-dashed treatment) with name + technology +
  description.
- **Instance badge:** `×N` in a node header when `instances > 1`.
- **Adapter** (`adapters.ts` `toDeploymentView`): resolve `component` name →
  `{id, layer}`; carry `infrastructureNodes`, `instances`, `external`.

### 5. Generation — author prompt + resolution

Update `server/internal/manager/systemdesign/prompts.go` (operational-concepts
step) to teach the new nested node kinds (`children`, `infrastructureNodes`,
`componentInstances`, `instances`), the container level (put co-located
components inside their runtime container), infra/external nodes, and the
**reference-by-name** rule (already stated; now actually honored by the
resolver + validator). Re-running System Design then emits the new shape.

## Migration

- Rewrite the committed archistrator `deployment` slot (Section 2). Resolve the
  `project-manager` dangling ref — **open question** (see below).
- Update `cmd/shapegen` example (`server/cmd/shapegen/main.go:244-267`) to the
  new shape so the canonical `project.json` example matches.
- `cmd/validate` round-trip must stay byte-stable after the rewrite.

## Implementation phases

1. **Model:** Go structs + `models.ts` mirror + `shapegen` example. Regen OAS
   (`make gen-client`; `webApp` `npm run gen:api`) — payloads stay opaque, so no
   TS type break. (`GOWORK=off` for all Go `make` targets.)
2. **Validation:** DEP-INSTANCE-EXIST in `arch_test.go` + `cmd/validate`.
3. **Data rewrite:** archistrator `deployment` slot to the real topology; make
   validation pass (resolve `project-manager`).
4. **Renderer:** adapter + `infrastructureNode` type + instance badges + nested
   container rows.
5. **Generation:** author prompt update.

## Open questions

1. **`project-manager` dangling ref.** Is it a real component missing from the
   System design (then: add it to System), or a deployment-only mistake (then:
   remove/rename it to the correct manager)? Blocks Phase 3 validation.
2. **Resources placement.** Model the Postgres-backed state (`operated-system-
   state`, `billing-state`, `usage-log`) as `componentInstances` inside the
   `archistrator-postgres` node (proposed), or as `infrastructureNode`s? Proposed
   keeps them as System components (they exist in the architecture), which the
   "same components as system design" requirement favors.
3. **`instanceId`.** Structurizr numbers instances; we omit a per-instance index
   (the `instances` multiplier suffices for our diagram). Add later if needed.
