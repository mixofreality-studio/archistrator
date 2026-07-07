# App-Generator Codegen Expansion — Design

**Date:** 2026-07-06
**Status:** Approved design, pending implementation plan
**Repos:** `archistrator-platform` (new modules), `archistrator` (dogfood migration), delivered projects (e.g. `gtdapp`) via scaffold

## Goal

Extend the schema-first codegen pipeline so that all deterministic Temporal and
composition-root code is generated from `project.json`, leaving humans/agents to
write only policy and sequencing logic. The more that is generated, the less
agents can get wrong, and the more the Method is enforced by construction.

Three deliverables:

1. **Temporal codegen** — per-manager activity boilerplate, workflow-side typed
   invokers, worker registration, payload codecs, all scoped to the manager's
   architecture-approved dependency surface.
2. **Composition-root codegen** — fully generated `main.go` + `config.go` per
   deployment container, driven by a typed deployment model.
3. **Shared model extraction** — one platform module that parses and validates
   `project.json` for every generator, replacing the per-generator embedded
   `contract/` copies, and moving the in-repo `modelgen` emitter to the platform
   so delivered projects (gtdapp) can generate `contract.gen.go` too.

Generators must be fully generic: driven only by `project.json`, hosted in
`archistrator-platform`, dogfooded on archistrator's own server first, and
delivered to constructed projects through the managed scaffold.

## Decisions (ratified during brainstorming)

- **Target:** both archistrator (dogfood first) and delivered systems (gtdapp).
- **main.go:** fully generated from a typed deployment model. Zero handwritten
  composition-root files is the end state; residual policy folds into component
  packages as named profile variants.
- **Activities allowlist:** component-level scoping for now — one generated
  activity per method of each RA the manager depends on (per the
  `.relationships` edges). Op-level allowlists are an earmarked follow-up.
- **Engines:** derived per-engine from the architecture graph. An engine with
  zero RA edges is pure → direct in-workflow typed wrapper (zero history
  events). An engine with RA edges does I/O → activity-hosted (Engine→RA is
  legal in the Method; purity is computed, not flagged).
- **Also generated:** workflow-side typed invokers (the enforcement surface),
  `RegisterWorker` + task-queue constants, payload envelope codecs.
- **Out of scope (earmarked):** typed signal/query/update generation,
  replay-determinism test harness, op-level allowlists.
- **Packaging:** one shared loader module (`framework-go-projectmodel`) + one
  emitter module (`framework-go-app-generator`). The http/mcp generators
  migrate onto `projectmodel` **now** (in scope), deleting their embedded
  `contract/` copies. The in-repo `server/cmd/modelgen` emitter moves into
  `app-generator` (an in-repo shim remains until deleted).

## Module layout

```
archistrator-platform/
  framework-go-projectmodel/          # NEW — the shared loader
    projectmodel.go                   # Load(path) → Model
    contract.go, gotype.go, naming.go # lifted from the http-gen copy (superset)
    relationships.go                  # systemDesign slot → typed dependency graph
    deployment.go                     # operationalConcepts.deployment → containers/profiles/bindings
  framework-go-app-generator/         # NEW — the emitters (all import projectmodel)
    modelgen/                         # contract.gen.go emitter (moved from server/cmd/modelgen)
    temporalgen/                      # activities/invokers/worker/codec emitters
    composegen/                       # main.gen.go + config.gen.go emitters
  framework-go-http-generator/        # MIGRATED — imports projectmodel, embedded contract/ deleted
  framework-go-mcp-generator/         # MIGRATED — imports projectmodel, embedded contract/ deleted
```

- `framework-go-projectmodel` owns one question: *given a project.json, what is
  the architecture?* It parses **and cross-validates**: every relationship
  endpoint resolves to a contract, every deployment-container component exists,
  layer rules hold (Manager→Engine/RA, Engine→RA, downward only), every binding
  component exists, every referenced infra key is declared. `Load` fails loudly
  on drift; downstream emitters assume a consistent model. Method enforcement
  happens once, here — not per-emitter.
- `framework-go-app-generator` is emit-only: text out, deterministic,
  byte-idempotent (sorted iteration, `go/format`, never compiles the target) —
  the same discipline as httpgen.
- Per-project driver: a thin `cmd/appgen` (archistrator: `server/cmd/appgen`;
  gtdapp: seated by the managed scaffold, same delivery as the design-workflow
  files), wired into `make gen`.
- Compatibility surface: platform modules are consumed by tagged release, so
  project.json schema evolution versions with the platform; the scaffold pin
  decides which generator version a delivered repo runs.

## Schema ownership & evolution

**Dependency direction is fixed:** platform never imports archistrator, and
delivered apps consume only the platform. So shared shape lives in the platform
as code — but ownership of the document splits:

- **Archistrator owns the full project.json** — it is product state; most
  slots are irrelevant to codegen.
- **`projectmodel` owns the codegen-relevant subset** (`serviceContracts`,
  `systemDesign` components/relationships, `operationalConcepts.deployment`)
  and parses it **tolerantly**: unknown fields are ignored, absent fields get
  a defined default (the `OperatingModel.OrDefault` precedent, made a hard
  rule). Every old document parses under every newer projectmodel.

**Adding a property — two cases:**

1. **App-only** (generators don't read it): change archistrator only. No
   platform release, no pin bumps — tolerant parse means projectmodel never
   notices. This is the common case.
2. **Codegen-relevant**: platform-first, then consume:
   - Platform PR: field + default + validation + fixture in `projectmodel`
     (+ the consuming emitter). Tag release. The new generator still parses
     every old document (default applies), so this never blocks on writers.
   - Archistrator: bump the platform dep; writers (state MCP tools, draft
     prompts, slot schema) start producing the field; regenerate via
     `cmd/appgen`. Review-gated slots pick it up through the amendment rail.
   - Delivered apps: scaffold pin bump (`appgen` + `aiarch-state`),
     version-gated for in-flight workflow compatibility (StateMcpModulePin
     rail).

**Drift guard:** a contract test in archistrator CI feeds the documents
archistrator's writers produce (the live project.json + writer-output
fixtures) through `projectmodel.Load`. An overlapping-shape change made
app-side without the platform step fails archistrator's own PR, not a
delivered repo weeks later. Non-additive (breaking) changes are the exception
and gate on the existing top-level `version` field.

## Temporal codegen (`temporalgen`)

**Inputs:** the manager's dependency edges from `.relationships` + each
dependency's contract interface. **Workflow bodies stay handwritten** — the
sequencing logic is the designed part. Emitted into the manager's `goPackage`:

### `activities.gen.go`

A generated `Activities` struct: one field per RA dep (typed to the contract
interface), one activity method per contract op.

- **Idempotency** — derived from the op signature, no annotations: if the op
  takes an `fwra.IdempotencyKey` param, the body derives
  `workflowID:runID:activityID` from the activity context (the run-scoped rule;
  single blessed implementation). Read ops (no key param) get none.
- **Error mapping** — the generic terminal-error mapper applied to every RA
  result, tagging port failures with stable Temporal error types.
- **Registered names** — stable convention `<component>.<opName>` (e.g.
  `projectStateAccess.readProject`). Archistrator migration registers a
  one-time alias table with the old handwritten names so in-flight workflows do
  not strand.

### `invokers.gen.go`

The workflow-side enforcement surface: one **typed** method per approved
activity (`acts.ReadProject(wctx, id) (Project, error)`) wrapping
`ExecuteActivity` with the registered name and options. No string names, no
`interface{}` in workflow code — and no method exists for anything outside the
manager's approved dependency surface. Options come from generated per-layer
defaults (RA-write / RA-read / engine-hosted profiles), overridable via an
optional handwritten `ActivityOptions(op string)` hook: policy human, plumbing
generated.

### Engine handling (derived)

- Engine dep with zero RA edges → direct in-workflow typed wrapper.
- Engine dep with RA edges → activity-hosted methods (the RA is injected into
  the engine; the engine op runs inside one activity).
- Adding an RA dep to a previously-pure engine automatically moves it out of
  the workflow on regeneration — correct by construction.

### `worker.gen.go`

`TaskQueue` constant derived from the component name + `RegisterWorker(w, m)`
registering every workflow and every generated activity. Workflow funcs come
from a small handwritten manifest (the one thing codegen cannot know).
Forgetting a registration becomes impossible.

### `codec.gen.go`

Activity payloads must round-trip Temporal's JSON converter; interface-typed
sum-type models do not. `modelgen` knows every sum type's variants, so
`temporalgen` emits the envelope types + discriminated encode/decode for any
interface-typed param/result crossing an activity boundary. Replaces the
handwritten per-manager `codec.go`.

**Post-migration hand-written remainder per manager:** `workflow.go`
(sequences), prompts, signals, the workflow-func manifest. Deleted across all
five managers: `activities.go`, `worker.go`, `codec.go`.

## Deployment model + `config.gen.go`

`operationalConcepts.deployment` already carries `containers`
(component→binary) and `environments` (keyed by `profile`). Three typed
additions make it composition-complete:

### `infrastructure`

Shared satellite clients per container, per profile — the singletons main.go
builds first (temporal client, postgres pool, github-app client, keycloak
validator, otel provider):

```json
"infrastructure": [
  {"key": "temporal",  "substrate": "temporal",   "profiles": ["local", "cloud", "dryrun"]},
  {"key": "postgres",  "substrate": "postgres",   "profiles": ["cloud", "local"]},
  {"key": "githubApp", "substrate": "github-app", "profiles": ["cloud"], "presence": "optional"}
]
```

### `bindings`

Per RA component: which generated DI constructor variant runs under which
profile, wired to which infrastructure keys, with presence semantics:

```json
{"component": "projectStateAccess",
 "perProfile": {"local": {"variant": "GitLocal"}, "cloud": {"variant": "GitHub", "infra": ["githubApp"]}},
 "presence": "required"}
{"component": "sourceControlAccess",
 "perProfile": {"cloud": {"variant": "GitHub", "infra": ["githubApp"]}},
 "presence": "optional-dormant"}
```

`optional-dormant` generates today's convention uniformly: unconfigured → nil
dep + one warn log naming the enabling env vars; downstream consumers
nil-check (rail dormant, worker unregistered).

### `settings`

Non-infra scalars threaded into constructors (escalation timeout, intervention
mode, repo base): `{name, type, default, description}`, per binding or
container-global.

### Substrate catalog (generator-owned)

`app-generator` ships a registry: substrate name → satellite constructor, its
config inputs, its shutdown hook (e.g. `"postgres"` →
`framework-go-infrastructure-postgres.NewPool`, input URL). project.json
declares *intent* (architect's review-gated artifact); the platform owns
*mechanism* (versioned code). Adding an infra satellite = one catalog entry.

### `config.gen.go`

Fully derivable: one struct with a field per (infrastructure input + setting);
env names by convention `<PROJECT>_<KEY>_<INPUT>` with an explicit `env`
override (archistrator keeps its existing names — no live deployment breaks);
profile selection via `<PROJECT>_PROFILE` validated against declared
environments; boot-time validation reports **all** missing required vars for
the selected profile at once; a generated warn-log per dormant optional.

**Method process:** these additions live inside the operationalConcepts
artifact — architect-designed, review-gated; `projectmodel.Load`
cross-validates them. For archistrator this lands via the amendment rail.

## Composition-root codegen (`composegen`)

**Emits the whole `cmd/<container-key>/` package** per deployment container —
`main.gen.go` + `config.gen.go`, no handwritten files. `run()` is a topological
walk of the architecture graph: telemetry → infra satellites (profile-filtered)
→ RAs (binding profile-switch resolved once at boot) → engines → utilities →
managers (generated DI constructors, deps threaded by the graph) → workers
(`temporalgen.RegisterWorker`) → client handlers → server mount → reverse-order
shutdown.

### Policy folding rule

The generator calls constructors; it never contains policy. main.go's
remaining substance moves into component packages as named **profile
variants** with conventional signatures, referenced by bindings:

- `projectStateGitAdapter` + credential minters → `projectstate` package,
  behind `NewGitLocalProjectStateAccess` / `NewGitHubProjectStateAccess`.
- `dryRunPipeline` / `dryRunArtifacts` → their RA packages as `DryRun`
  variants (dry-run becomes a first-class substrate variant).
- Repo resolvers / `authenticatedOnlyPDP` → sourcecontrol and security
  packages respectively.

Variants remain handwritten where genuinely policy — but they live inside the
owning component, are named in the architecture artifact, and the generator
just calls them. This completes the folding trend already underway
(founder model 2026-06-28).

### Graph features the real main.go proves necessary

- **Multi-contract instances** — the git store satisfies `ProjectStateAccess`
  + `ConstructionTransitionAccess` + `GitActivityStatusAccess`. A binding may
  declare `provides: [list]`: one constructed instance bound to several
  component interfaces; cross-checked against the contracts.
- **Conditional workers** — a manager's worker registers iff all
  `presence: required` deps are non-nil and each effects-critical
  `optional-dormant` dep resolved or the profile stubs it (derives today's
  `selectConstructionDeps` from presence semantics).
- **Framework-conventional mounts** — `/healthz`, `/readyz`,
  `/api/userinfo`-style extras are catalog entries declarable as `httpExtras`
  in the deployment model (catalog mechanism, model intent).

### Migration

Emit alongside the handwritten root → diff-review → boot all three profiles +
systemtests green → delete `cmd/server/*.go` hand files. gtdapp never sees the
handwritten era.

## Enforcement gates (CI, all projects)

1. **`appgen validate`** — `projectmodel.Load` as a standalone gate:
   architecture consistency fails the build, not just generation. Slots into
   the methodcheck posture.
2. **Byte-idempotency drift gate** — regenerate all, `git diff --exit-code`.
   Hand-edited `.gen.go` or stale regen cannot merge (same discipline as the
   modelgen encapsulation gate).
3. **Arch checker extension** — manager packages may not call
   `workflow.ExecuteActivity` / `RegisterActivity` outside `.gen.go` files.
   With invokers-only surfaces, off-plan calls are structurally impossible
   *and* lint-caught.

## Testing

- **Generators:** golden-file tests in `app-generator` (httpgen `testdata/`
  pattern). Corpus = archistrator's real project.json + a synthetic greenfield
  fixture — the fixture catches "works for archistrator, breaks for gtdapp"
  generality bugs before gtdapp reaches phase 3.
- **projectmodel:** table-driven validation tests over the same corpus plus
  deliberately-broken fixtures (dangling edge, layer violation, unbound
  component).
- **Dogfood migration:** per-manager cutover — a manager's handwritten
  `activities.go` is deleted only when its systemtests pass on generated code.
  Boot all three profiles. Verify the activity-name alias table against a live
  in-flight workflow in dev before the platform release.
- **Release mechanics:** platform tagged releases + scaffold pin bump,
  version-gated for in-flight workflow compatibility (the state-MCP-pin rail).

## Delivery order (each step independently shippable)

1. Extract `framework-go-projectmodel` (contracts + relationships parsing);
   ship `temporalgen`; migrate all 5 archistrator managers; delete
   `activities.go` / `worker.go` / `codec.go`. No project.json schema changes.
2. Migrate `framework-go-http-generator` + `framework-go-mcp-generator` onto
   `projectmodel`; delete both embedded `contract/` copies; re-pin
   `clientgen`.
3. Move the modelgen emitter into `app-generator/modelgen` (in-repo
   `server/cmd/modelgen` becomes a shim) → closes the gtdapp
   `contract.gen.go` gap.
4. Deployment-model schema extension (via archistrator's amendment rail —
   dogfooding the design process) + `config.gen.go`.
5. `composegen` + policy-variant folding → delete handwritten `cmd/server`
   main.
6. **Earmarked follow-ups (out of scope):** typed signal/query/update
   generation, replay-determinism harness, op-level operation allowlists.
