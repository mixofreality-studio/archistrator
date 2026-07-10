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

### Step-1 outcomes (2026-07-07)

Recorded once all 5 managers (systemdesign/projectdesign/construction/
operations/billing) were migrated and Task 14's gates landed.

**Deviations from this section as designed:**

- **No alias table.** The registered-name alias table above assumed a 1:1
  rename of existing handwritten names. The migration's payload arity changed
  (the generated `Activities` methods thread `fwra.Context`/idempotency args
  the handwritten activities did not take — see the emitter rule below), which
  defeats a name-only alias: an in-flight workflow replaying against an
  aliased-but-reshaped activity would still fail to deterministically decode
  history. The precondition is **drain-before-deploy** instead: no in-flight
  executions on a task queue at migration-cut time. This lands hardest on
  design CoAuthor sessions, which commonly dwell at `AwaitingReview` for days
  (a human review window, not a processing wait) — those are the executions
  most likely to be in-flight across a deploy, so quiescing/draining the
  design task queues needs the most lead time of the five managers.
- **No `codec.gen.go`.** The emitter was never invoked because there is
  nothing for it to consume: zero `x-go-sumtype` definitions are committed in
  `.serviceContracts` today. The handwritten per-manager `codec.go` survives
  unchanged in `systemdesign`, `projectdesign`, and `construction` (see the
  earmark below to retire it via contract promotion).
- **No engine wrappers.** All five managers' Engine deps have zero RA edges,
  so every one resolved to the "Engine dep with zero RA edges → direct
  in-workflow typed wrapper" branch — there was no case exercising the
  activity-hosted (RA-injected) engine path. The generated worker layer
  carries no engine-hosted activity methods.

**Emitter rules added during the migration** (not anticipated by the design
above, needed to make the generated surface match hand-call-site behavior):

- Contract params typed `fwra.IdempotencyKey` are **hidden from the generated
  invoker/activity signatures** and auto-filled by the emitted code itself
  (derived via the run-scoped `workflowID:runID:activityID` rule for
  activity-originated calls, or threaded from the caller-supplied key where
  one already exists) — callers never pass an idempotency key explicitly.
- RA contract types using a **bare, dot-free `x-go-type`** (a type name with
  no package-qualifying dot, e.g. a type meant to resolve in the RA's own
  package) are now emitted **alias-qualified** to the owning RA package,
  rather than assumed to be in scope unqualified — the earlier assumption
  broke as soon as a generated file outside that RA's package needed the
  type.

**Earmarks confirmed** (unchanged targets, now validated against the real
5-manager migration rather than projected):

- Contract promotion for the branch-aware / ledger / provenance / reconciling
  extension ports plus the `constructionTransition`/`gitActivityStatus`
  surfaces — this is what retires the survivor `codec.go` (above) and most of
  the remaining hand-written custom activities.
- `noopRevenueLedger` excision (billing's `adapters.go`/`activities_custom.go`/
  `workermanifest.go`) once a real ledger implementation lands.
- Vestigial `genWorkerManifest.ActivityOptions` field cleanup — every migrated
  manager still carries the per-activity option-preset hook field even where
  a manager's preset map covers all contract-backed ops uniformly; worth
  revisiting once the presets stabilize across all five.
- **Release/pins follow-up** (deferred out of Task 14 by the controller scope
  ruling — tags must point at merged commits): tag
  `framework-go-projectmodel`/`framework-go-app-generator`, pin them in
  `server/go.mod`, drop the `appgen` build tag, flip `gen-temporal` to
  `GOWORK=off`, fold it into the `gen` aggregate, and enable the commented-out
  CI drift step in `server-checks.yml` (today's local equivalent: `make
  gen-temporal-check`).

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

## Holistic codegen targets (2026-07-07 sweep, founder-ratified)

A full sweep of server, systemtests, uitests, and webApp found ~5,000 lines of
hand-written mirrors of project.json / its derived artifacts, explained by
three root causes:

**RC1 — opaque OAS models.** The OAS types every artifact `model` as `null`;
slot-model schemas exist nowhere (the Go structs are the authority). Every
consumer re-types them by hand: webApp `models.ts` (468 L) + most of `wire.ts`
(526 L), uitests `designStubs.ts` envelopes, systemtests wire DTOs.
**Fix:** reflect Go slot models → JSON Schema at gen time (schemagen
machinery) and emit them into the OAS `$defs`; webApp's existing
`openapi-typescript` step then produces the TS types for free.

**RC2 — iota ordinals cross the wire as bare ints.** Hand-kept ordinal↔name
tables in webApp `enums.ts` (318 L), systemtests transports (~120 L), uitests
stubs, plus ~5 parallel artifact-kind lists — a server enum reorder breaks all
of them silently. **Fix:** emit enum descriptors (`x-enum-varnames` + ordinal
maps) into the OAS from the contract `$defs`; generate every table from that
one source.

**RC3 — no generated Go client SDK.** systemtests hand-writes per-op HTTP
(612 L) and MCP (595 L) transports + a Transport interface + opTable glue
(~470 L) — two full hand-copies of the contract surface. **Fix:** a
`transportgen` emitter in app-generator producing typed Go HTTP+MCP clients
per contract — also a product artifact (delivered apps' consumers get an SDK).

**Server-residual mechanical code** (unblocked by RC1's reflected schemas):
`projectstate/modelfields.go` (483 L validation walks — highest drift risk),
`enumjson.go` (375 L closed-enum codecs), `registry.go` (78 L kind→model
switch) — all modelgen-emittable.

**Deployment-model consumers beyond main.go:** the systemtests harness
duplicates the server env contract (~130 L) — the substrate catalog also emits
a test-harness env helper.

**Cleanups (small, ratified):** shared stable component id between
serviceContracts and systemDesign (deletes the webApp `contractComponentId.ts`
heuristic AND projectmodel's join heuristic); `activityprofile.go` → JSON
emission (kills webApp `lifecycleTemplates.ts`, 388 L); snapshot-generate the
uitests `coreUseCasesProject.json` fixture; uitests `testids.ts` fixed by
importing webApp's `UIIdentifiers.ts` (sharing beats generating).

**Deliberate non-targets:** eslint-boundaries config (SPA's own layering,
correctly independent of the Method model), router (10 stable routes),
slash-command prose + MCP verb descriptions (bespoke prompt surfaces; only the
file-set × `CommandFor` matrix gets a consistency check), team/role charter
prose (doctrine), postgres DDL/scan (judgment-laden; revisit later), the
aiarch-state MCP tools (already generic over slot kinds).

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

## Delivery order (each step independently shippable; revised 2026-07-07)

1. Extract `framework-go-projectmodel` (contracts + relationships parsing);
   ship `temporalgen`; migrate all 5 archistrator managers. No project.json
   schema changes. (Plan:
   `docs/superpowers/plans/2026-07-07-projectmodel-temporalgen.md`.) **Done
   2026-07-07** — see "Step-1 outcomes (2026-07-07)" under Temporal codegen
   above for the deviations (no alias table, no `codec.gen.go`, no engine
   wrappers), the two emitter rules added during migration, and the confirmed
   earmarks including the deferred release/pins follow-up.
2. Migrate `framework-go-http-generator` + `framework-go-mcp-generator` onto
   `projectmodel`; delete both embedded `contract/` copies; re-pin
   `clientgen`.
3. Move the modelgen emitter into `app-generator/modelgen` (in-repo
   `server/cmd/modelgen` becomes a shim) → closes the gtdapp
   `contract.gen.go` gap.
   **DONE 2026-07-09** (app-generator/v0.2.0): faithful port with
   `Config{ModulePath, EngineImplAllowlist}`; descriptor types moved (schemagen
   re-imports them; `cmd/internal/codegen` deleted); byte-identity proven by
   platform goldens against two real outputs + archistrator's full 22-file
   zero-diff regen; new `gen-models-check` drift gate in Make + CI.
   Earmarks added: consolidate modelgen onto projectmodel types (needs
   projectmodel to expose structured $defs), extensible infra-bindings
   registry, emitter-state struct (pendingImports global is not
   concurrent-Generate-safe), header-string modernization, Ollama/Replay
   binding fixture coverage.
4. **Typed OAS** (RC1+RC2): reflect slot-model schemas into OAS `$defs` +
   enum descriptors (clientgen change) → webApp deletes
   `models.ts`/`enums.ts`/most of `wire.ts`; uitests stubs re-derive.
   **DONE 2026-07-09.** Honest outcome vs the original promise: `models.ts`
   deleted (11/12 unions DERIVED from generated types; FloatBand hand-pinned);
   `enums.ts` deleted and REPLACED BY GENERATED `enums.gen.ts` (a boundary
   ordinal↔name table always exists — the wire ships ints; 7 non-mechanical
   app-string mappings survive in `enumMappings.ts` typed over generated
   varname unions); `wire.ts` THINNED not deleted (its PascalCase→camelCase
   view renaming is orthogonal to typed models); display order/labels
   consolidated into `METHOD_METADATA` as hand-authored product data.
   Slot-model schemas are reflected at gen-client time (never added to
   serviceContracts — modelgen would emit duplicate structs). Two generator
   root-cause fixes shipped: string-marshalled int enums emit string schemas
   (live-marshal registry + source-walk completeness guard) and non-omitempty
   struct pointers emit nullable. New drift gates: `gen-client-check` +
   webApp gen regen+diff in CI. Earmarks (latent, none live today):
   enum-override coverage for map values/nested wrappers; completeness guard
   for non-int custom marshalers; uitests fixture stubs may need real model
   bodies if client decode tightens.
5. **`transportgen`** (RC3): generated Go HTTP+MCP client SDK → systemtests
   transports + opTable deleted; SDK becomes a delivered-app artifact.
   **DONE 2026-07-10** (http-generator/v0.3.0 exports the op planner —
   single route truth; app-generator/v0.3.0+v0.3.1 ship transportgen reusing
   modelgen.EmitTypes). Self-contained stdlib-only SDK generated into
   systemtests/internal/sdk (18 files, uuid-as-string, prune-stale,
   gen-sdk-check drift gate in Make+CI); harness transports are thin
   delegates (Transport seam + sentinels preserved); hand wire structs, the
   MCP JSON-RPC/SSE loop, and all 11 ordinal tables deleted. Proof: 23-op
   route-fidelity golden vs the retired hand transport, vet-ing compile
   sandbox, FULL live systemtests suite green (usecases 530s) incl. the R4
   HTTP/MCP cross-surface equivalence property. One emitter defect found by
   the consumer and root-fixed pre-release (pointer path params → value
   scalars, v0.3.1). Earmarks: MCP protocol-level errors no longer map to
   ErrBadRequest (unreachable via typed harness calls; platform follow-up);
   agentic_github.go's two pre-existing hand ArtifactKind maps fold onto
   enums.go later. Note: opTable itself survives (it maps plan steps →
   Transport methods — orchestration, not transport duplication).
6. **projectstate validation codegen**: `modelfields`/`enumjson`/`registry`
   emitted by modelgen from the reflected schemas.
   **DESCOPED 2026-07-10 (founder-ratified).** Deep recon overturned the
   premise: (a) generating these requires a reflection driver that imports
   projectstate while emitting compiled `.go` INTO it — a malformed emission
   bricks both the package and the generator (every existing reflection
   driver deliberately emits outside the compile path); (b) enumjson's wire
   strings are the terminal authority (3 of 13 enums have non-mechanical
   const prefixes) and modelfields' valuable rules are Method-semantic
   (F81 layer/kind cross-field, guardedFlow, encapsulates gating,
   structural floors) — generation would relocate hand data behind an
   intermediate-artifact pipeline. Shipped instead: the one live drift
   hazard closed by `enumwire_completeness_test.go` (bidirectional
   wire-map ↔ contract-ordinal completeness over all 14 enums,
   fail-first-proven both directions), joining the existing registry
   coverage test, F81 regression suite, and step-4's registry source-walk.
7. Deployment-model schema extension (via archistrator's amendment rail —
   dogfooding the design process) + `config.gen.go` + systemtests harness env
   helper.
   **DONE 2026-07-10** (projectmodel/v0.2.0, app-generator/v0.4.0,
   framework-go/v0.4.5). operationalConcepts.deployment gained
   infrastructure/bindings/settings + a `local` dev-boot environment
   (surgical additive splice, 380 insertions, gated by cmd/validate +
   methoddesign + projectmodel + all gen-checks); methodcheck learned the
   local dev-boot profile (permitted, coverage-exempt; real environments
   unchanged). configgen emits config.gen.go (env parsing with verbatim
   legacy names, missing-var collection, dormant warnings) — hand config.go
   DELETED, a thin adapter keeps the 6 genuinely-hand behaviors
   (unconditional postgres, conditional construction creds, PEM _FILE,
   chained account default, installation-id coercion, dev principal) with a
   parity test; harness env-var names now generated consts (rename → compile
   error). Profile RESOLUTION stays hand until step 8; MissingFor/
   DormantWarnings become load-bearing there. Earmarks: provides[] cannot
   express internal git-store ports (ties to contract promotion);
   Binding.Settings parsed but unconsumed; container-scoped infra filtering;
   three appgen runs in CI drift steps foldable to one.
8. `composegen` + policy-variant folding → delete handwritten `cmd/server`
   main.
9. **Cleanups**: shared component id (kills both join heuristics),
   activity-profile JSON emission, uitests fixture snapshotting,
   testids-by-import.
10. **Earmarked follow-ups (out of scope):** typed signal/query/update
    generation, replay-determinism harness, op-level operation allowlists,
    postgres DDL/scan generation, `x-go-sumtype` promotion of `ArtifactModel`
    + branch-aware/ledger/transition/git-status contract promotion (retires
    the migration-surviving `codec.go` + hybrid custom activities).
