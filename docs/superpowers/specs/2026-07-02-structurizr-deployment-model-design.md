# Structurizr/C4 deployment model + validation — design

> **Revised after review.** Two decisions changed the shape from the first draft:
> (1) **Granularity = true C4**: the deployment view instances **containers**
> (`archistrator-server`, `archistrator-webapp`, `archistrator-postgres`), **not**
> individual Method components. (2) **Containers are declared in the deployment
> artifact** (not a new architecture-model level), each listing the System
> components it packages. Also: `project-manager` is a mistake → delete it; and
> the referential validation must live in the **platform** tooling that already
> exists (`methodcheck`), wired so archistrator's own project is checked.

Reshape the stored deployment topology (`OperationalConcepts.deployment` in
`project.json`) to mirror Structurizr's deployment metamodel, wire the existing
platform **`methodcheck`** validation to run against archistrator's own project,
and rewrite archistrator's deployment data to the **real** k8s topology.

Source refs: Structurizr deployment view (`docs.structurizr.com/dsl/cookbook/
deployment-view`, `github.com/structurizr/structurizr`); real topology from
`…/software/products/archistrator/helm/` + `…/software/k8s/`.

## Problem (grounded in real data)

Current model (`server/internal/resourceaccess/projectstate/models_phase1.go`,
hand-mirrored in `webApp/src/api/models.ts`; independently decoded by the
platform validator in `archistrator-platform/framework-go/methodcheck/project.go`):

```
DeploymentNode { name; technology; children[]; instances[] ContainerInstance }
ContainerInstance { componentId; note }   // a *component*, deployed flat
```

Gaps vs. C4 and vs. reality:

1. **Not C4.** C4 deployment instances **containers** and **software systems**,
   never components. Today the leaf is a Method *component* placed flat in the
   namespace, and there is **no container concept** at all.
2. **No container level in reality.** Nearly all Method managers/engines/
   resource-access seams run inside **one Go binary** (`archistrator-server`, ×2,
   distroless — API + Temporal client + embedded worker + every seam in one
   container; **no** separate worker Deployment). The webapp SPA (`archistrator-
   webapp`, nginx, ×2) and Postgres (`archistrator-postgres`, CloudNativePG) are
   the other two containers.
3. **No infrastructure / external-system nodes.** Envoy Gateway, shared Temporal,
   per-project GitHub repos, GitHub Actions runners, Anthropic API, Keycloak all
   have to be faked as component instances or omitted.
4. **No `instances` multiplier / `description` / `tags`** (server is ×2, etc.).
5. **Validation exists but doesn't run for archistrator.** The platform
   `methodcheck` package **already has** `DEP-INSTANCE-EXIST` +
   `DEP-{COVERAGE,PROFILE-SET,GRAPH-IDENTITY,NODE-WELLFORMED}`
   (`methodcheck/rules_deployment.go`, driven by `deploymentConsistency` →
   `validateOperationalConcepts` → `ValidateProject`). But archistrator's own
   repo has **no `TestMethod` running `methodcheck.Check` over its own
   `.aiarch/state/project.json`** — that gate is only scaffolded into *downstream*
   app repos (`sourcecontrol/assets/aiarch_method_test.go.tmpl`).
   `make method-check` only runs `TestMethodLayering` (`arch.Check`). **That is
   why the dangling `project-manager` ref went uncaught.**

## Design

### 1. Model — C4 container instances (Structurizr shape)

Containers are declared once in the deployment topology; deployment nodes
**instance** them by key. Go source of truth in the server package, mirrored in
the platform `methodcheck` decode structs and the webApp `models.ts`.

```go
type DeploymentTopology struct {
    DeliveryStyle DeliveryStyle           `json:"deliveryStyle"`
    Containers    []DeployContainer       `json:"containers"`      // NEW: declared once
    Environments  []DeploymentEnvironment `json:"environments"`
}

// DeployContainer — a deployable unit (C4 Container), packaging System components.
type DeployContainer struct {
    Key        string   `json:"key"`         // "archistrator-server"
    Name       string   `json:"name"`
    Technology string   `json:"technology"`  // "Go · distroless", "nginx · React SPA"
    Description string   `json:"description"`
    Components []string  `json:"components"`  // System component NAMES packaged here (validated)
}

type DeploymentEnvironment struct {
    Profile DeploymentProfile `json:"profile"`
    Title   string            `json:"title"`
    Nodes   []DeploymentNode  `json:"nodes"`
}

type DeploymentNode struct {
    Name                    string                   `json:"name"`
    Technology              string                   `json:"technology"`
    Description             string                   `json:"description"`
    Instances               int                      `json:"instances"`   // multiplier; 0 ⇒ 1
    Tags                    []string                 `json:"tags"`
    Children                []DeploymentNode         `json:"children"`
    InfrastructureNodes     []InfrastructureNode     `json:"infrastructureNodes"`
    ContainerInstances      []ContainerInstance      `json:"containerInstances"`
    SoftwareSystemInstances []SoftwareSystemInstance `json:"softwareSystemInstances"`
}

type ContainerInstance struct {
    ContainerKey string   `json:"containerKey"`  // → DeployContainer.Key
    Note         string   `json:"note"`
    Tags         []string `json:"tags"`
}

// InfrastructureNode — non-deployable infra (gateway, DB engine, message broker).
type InfrastructureNode struct {
    Name        string   `json:"name"`
    Technology  string   `json:"technology"`
    Description string   `json:"description"`
    Tags        []string `json:"tags"`
}

// SoftwareSystemInstance — an EXTERNAL software system (GitHub, Anthropic, Keycloak).
// External-only: archistrator's model has exactly one (internal) software system,
// so there is no internal software-system registry to reference.
type SoftwareSystemInstance struct {
    Name        string   `json:"name"`
    Technology  string   `json:"technology"`
    Description string   `json:"description"`
    Tags        []string `json:"tags"`
}
```

Container membership references System components **by name** (matches the
architecture view + the existing author prompt; id = `Slug(name)`, resolved by
the validator and the webApp adapter). This finally honors the "reference by
name, server resolves" rule the prompt already states but the code never
implemented.

### 2. Reality mapping — archistrator's deployment data

**Containers** (declared once): `archistrator-server` (Go · distroless) packaging
nearly every Method component (mcp-client, scheduler-client, all Managers,
Engines, ResourceAccess, Utilities); `archistrator-webapp` (nginx · React SPA)
packaging `web-client`; `archistrator-postgres` (CloudNativePG · Postgres 16)
packaging the Postgres-backed Resource components (operated-system-state,
billing-state, usage-log).

**cloud** environment nodes:

```
DeploymentNode "Mixofreality Kubernetes Cluster" (k8s)
├─ DeploymentNode "gtd namespace" (k8s-namespace)
│   └─ InfrastructureNode "Envoy Gateway" (Gateway API; edge TLS + Keycloak OIDC)
├─ DeploymentNode "archistrator namespace" (k8s-namespace)
│   ├─ DeploymentNode "server Deployment" instances:2
│   │     containerInstances: [archistrator-server]
│   ├─ DeploymentNode "webapp Deployment" instances:2
│   │     containerInstances: [archistrator-webapp]
│   └─ DeploymentNode "postgres (CNPG)" instances:1
│         containerInstances: [archistrator-postgres]
├─ DeploymentNode "temporal namespace" (shared)
│   └─ InfrastructureNode "Temporal Cluster" (frontend/history/matching/worker)
└─ softwareSystemInstances: [Keycloak (OIDC)]

Sibling top-level:
DeploymentNode "User's GitHub Account"
├─ softwareSystemInstance "GitHub" (per-project state repos aiarch-<id> + Actions runners)
softwareSystemInstance "Anthropic Messages API" (LLM workers)
```

**test** ("in-process, ephemeral"): a single `test process` node instancing the
same containers (externals become in-process stubs) — same container set as
cloud, per the cross-profile invariant. **Delete the `project-manager` entry.**

### 3. Validation — wire the platform gate to archistrator (+ retune for containers)

The referential check already lives in the platform (`archistrator-platform/
framework-go/methodcheck`). Two pieces of work:

**a. Retune the deployment rules to the container shape** (`methodcheck/
rules_deployment.go`, structs in `methodcheck/project.go`):
- `DEP-CONTAINER-REF` — every `ContainerInstance.containerKey` resolves to a
  declared `DeployContainer`.
- `DEP-MEMBER-EXIST` (reworked `DEP-INSTANCE-EXIST`) — every
  `DeployContainer.Components[]` name resolves to a committed System component.
- `DEP-COVERAGE` — every System component is packaged in exactly one container
  (both directions ⇒ "same components as the system design").
- `DEP-PROFILE-SET` / `DEP-GRAPH-IDENTITY` retuned to the container set (identical
  containers across cloud/local; test includes all).
  Infra / external software-system nodes reference nothing and are exempt.

**b. Close the gap so archistrator validates itself.** Confirmed: archistrator
does **not** run `methodcheck.Check` on its own `.aiarch/state/project.json`
today. Two distinct gates are easily conflated:
- `arch.Check` (`TestMethodLayering`; what `make method-check` actually runs via
  `go test -run TestMethod`) validates the **Go source** layering/encapsulation —
  runs on archistrator.
- `methodcheck.Check` validates the **project.json design artifact** (incl.
  `DEP-*`) — only the seated `go test` **scaffolded into downstream user repos**;
  never run over archistrator's own project. CI (`server-checks.yml`) runs
  `make test-short`, neither gate over project.json.

So archistrator dogfoods the Method for its *code* but not for its own *design
artifact* — hence `project-manager` slipped through. Fix: add a repo-root
`TestMethod` (or a dedicated target) that runs `methodcheck.Check` over
`.aiarch/state/project.json` (`framework-go` is already in `go.work`), and add a
CI job. This enforces the whole `methodcheck` suite on archistrator itself.

### 4. Renderer — container-level C4 deployment

The deployment view now shows a **handful of container-instance boxes** inside
deployment nodes (like the Structurizr banking example), not ~30 component boxes:

- **`containerInstance` node** — a C4 container box: name, `[Container: technology]`,
  description, and a compact "packages N components" affordance (hover/expand to
  list the packaged System component names — keeps "same components as the system
  design" visible without turning the deployment view back into a component dump).
- **`infrastructureNode`** — distinct infra style (no layer color).
- **`softwareSystemInstance`** — external/dashed style.
- **Instance badge** `×N` on a node header when `instances > 1`.
- Adapter (`adapters.ts`): resolve container membership names → System components;
  carry infra / external / instances. (The just-shipped per-layer row code is no
  longer used for the deployment view — it stays for the architecture views.)

### 5. Generation — author prompt

Update the operational-concepts prompt (`server/internal/manager/systemdesign/
prompts.go`) to emit the new shape: declare `containers` (with packaged component
names), instance them in `deploymentNodes`, and use `infrastructureNodes` /
`softwareSystemInstances` for infra + externals. Re-running System Design then
produces valid, container-based topologies.

## Migration

- Rewrite `project.json .slots['6'].model.deployment` to the container shape
  (Section 2); delete `project-manager`.
- Update the platform `methodcheck` fixtures/tests (`rules_deployment_test.go`)
  and the server `cmd/shapegen` example (`server/cmd/shapegen/main.go:244-267`)
  to the new shape.
- `cmd/validate` round-trip stays byte-stable after the rewrite.

## Scope — two repos

- **`archistrator-platform/framework-go`** (`v0.3.0`, local via `go.work`):
  `methodcheck` deployment structs (`project.go`) + rules (`rules_deployment.go`)
  + fixtures. Publish/bump per the platform release flow.
- **`archistrator`**: server project-state structs (`models_phase1.go`), webApp
  `models.ts` mirror, adapter + renderer, author prompt, `shapegen` example, the
  new root `TestMethod` + `make method-check`/CI wiring, and the data rewrite.

## Implementation phases

1. **Model** (both Go representations + TS mirror + `shapegen`). `GOWORK` on for
   the workspace build; server `make` targets use `GOWORK=off`.
2. **Validation retune** in platform `methodcheck` + fixtures.
3. **Self-gate**: archistrator root `TestMethod` + `make method-check` + CI.
4. **Data rewrite** (container shape; delete `project-manager`); gate goes green.
5. **Renderer** (container / infra / external nodes + instance badges).
6. **Generation** (author prompt).

## Resolved decisions

- Deployment leaf = **container instances** (true C4), not component instances.
- Containers **declared in the deployment artifact**, packaging System components
  by name; no new architecture-model level.
- External systems (GitHub, Anthropic, Keycloak, Temporal) = `infrastructureNode`
  / `softwareSystemInstance`, exempt from component validation.
- `project-manager` = **delete**.
- Validation lives in **platform `methodcheck`** (already there); the fix is to
  retune it for containers and **run it against archistrator itself**.

## Resolved (round 2)

- **Container box contents:** container is the primary unit; packaged component
  names shown as a **compact hover/expand list** inside the box.
- **`archistrator-postgres` = a container** packaging the Resource components
  (operated-system-state, billing-state, usage-log), keeping `DEP-COVERAGE`
  satisfiable.
- **Validation gap confirmed:** archistrator does not run `methodcheck.Check` on
  its own project.json (Section 3b); wiring it is in scope.
