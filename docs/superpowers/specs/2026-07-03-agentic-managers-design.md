# Agentic Managers — doctrine + construction application

Date: 2026-07-03
Status: approved direction (founder), spec pending review

## Problem

Agents executing construction work (inside `aiarch-construct.yml` runs) free-edit
`project.json` under prompt-taught discipline (`the-method-project-state`
record-then-commit). Consequences: invalid states are possible, every prompt
re-teaches the slot map, and edits are unauditable by supervision. Separately,
the Method (as we practice it) had no doctrine for components whose behavior is
LLM-orchestrated, which we need both for archistrator itself and for the
agentic applications archistrator constructs for customers.

## Decision summary

**Agentic is an implementation strategy, not a layer.** No new taxonomy
element. An agentic Manager is Löwy's ch. 5 workflow Manager with the LLM as
the workflow interpreter: sequence volatility so high it binds at run time
instead of deploy time. Binding-time continuum: coded (compile time) → stored
workflow (deploy time) → agentic (run time); bind as late as the volatility
demands, and no later.

Grounding in Righting Software (research/rightingsoftware):

- ch. 5 workflow Manager: "loads … a specific instance of it, with a particular
  state and context; executes the workflow; and persists it back to the
  workflow store." A GH action run is exactly this: checkout rehydrates the
  instance (project.json + repo = workflow store), the agent executes, the CAS
  commit persists back. The workflow execution tool (GH runner + Claude) is a
  Resource and does not appear as an architecture component.
- ch. 3 atomic business verbs: "practically immutable because they relate
  strongly to the nature of the business" — the ideal LLM tool surface: stable
  verbs, volatile sequencing.
- ch. 4 composable design: validation never required enumerating sequences,
  only showing core interactions are composable — which is why agentic
  nondeterminism does not break design validation.

## Doctrine rules (→ the-method-agentic-components skill)

1. **Layers that may be agentic**: Manager (sequences + writes) and Engine
   (judgment only). ResourceAccess, Resources, Utilities: never — by explicit
   omission.
2. **Tool manifest = allowed outbound edges.** An agentic component's tools are
   exactly its declared dependency edges in systemDesign, filtered by layer
   rules:
   - Agentic Manager → its Engines + its RA verbs (+ **queued** posts to other
     Managers only).
   - Agentic Engine → read-only RA verbs at most; side-effect-free.
   Never Manager operations as synchronous tools (that would make the agent a
   client and reopen sideways calls).
3. **Invariant sequencing never rides in the prompt.** If engine-before-RA must
   always hold, it is not volatile: compile it into a composed verb or a
   precondition token the engine issues. The LLM sequences only what is
   genuinely volatile.
4. **Prompts never carry business rules.** Business rules leaking into a prompt
   is the ch. 3 "expensive Manager" flaw (functional decomposition) in agentic
   form. Prompts carry goals and volatile sequencing only.
5. **Per-component justification.** Agentic must be justified by that
   component's sequence volatility (Löwy's workflow-tool warning: confirm
   volatility justifies the complexity). Coded is the default.
6. **Interaction don'ts become manifest lint.** No Engine→Engine, no RA→RA, no
   sideways sync calls, Manager→Manager queued only — all mechanically
   checkable at manifest-derivation time.
7. **Read-only Engines (universal).** All Engines — coded or agentic — depend
   only on Reader ports. **Extension beyond Löwy** (he allows Engine→RA
   unqualified); adopted because it makes the agentic-Engine guarantee a
   special case of a universal, compile-checkable rule.
8. **Hybrid = one component at two binding times.** Canonical example:
   ConstructionManager. Outer lifecycle (submit → observe → record exit) is
   fixed by the Method → coded durable workflow. Per-activity work is maximally
   volatile → agentic episode. One Manager box in the architecture; the split
   is an operational concept. The in-run agent is the Manager's externalized
   sequencing logic — not a client, not a second manager, not RA internals.
9. **AI inside an RA verb** is legitimate only when it decides *how to
   physically effect a fully specified change* (e.g. mechanical merge
   resolution). If it decides *what change to make*, it is sequence volatility
   hiding in the resource layer ("expensive ResourceAccess") — forbidden.

## Construction application

- `ConstructionPipelineAccess` unchanged: RA over the execution resource
  (submit/observe/cancel). It never touches project.json.
- The in-action agent's writes become **ConstructionManager → ProjectStateAccess**
  calls via generated MCP tools over the existing GitStore CAS verbs
  (`StageArtifactForReview`, `CommitArtifact`, `RejectArtifact`,
  `WithdrawArtifact`, `AdvancePhase`, `SetResearchInput`, `CreateProject`).
  Today's direct git edits are resource-to-resource coupling behind
  ProjectStateAccess's back; this removes it.
- Component ≠ process: the tool implementation running inside the GH job *is*
  ProjectStateAccess code operating on the checkout. Only ProjectStateAccess
  code touches project.json, wherever it runs.

## Work breakdown (dependency order)

1. **Doctrine skills.** New `the-method-agentic-components`; strategies section
   in `the-method-layers`; link from `the-method` index; Reader/Writer port
   rule in `the-method-service-contract`; rescope `the-method-project-state`
   to humans/debugging.
2. **Contract split.** Every RA contract → Reader + Writer ports in
   schema-first codegen; Engines take Reader ports only (compile-enforced).
3. **Internal MCP surface.** Retarget existing MCP codegen at Engine + RA
   contracts (public Manager surface unchanged; internal surface never in the
   OAS, separate auth). `readOnlyHint` derived from port.
4. **Local MCP server in the GH job.** New `cmd/` stdio-MCP binary: serves the
   manifest-scoped tool set; GitStore verbs against the checkout; Engines
   in-process. Wired into `aiarch-construct.yml` Claude config. Server-hosted
   variant deferred for hosted substrate.
5. **Manifest resolver.** (component, activity) → scoped tool list from
   systemDesign edges + layer filters. Extends the typed-manifest /
   per-substrate-transport pattern.
6. **Prompt/skill updates.** Construction agents: state changes only through
   tools; delete record-then-commit teaching from agent-facing prompts.
7. **Project state schema.** `implementation: coded | hybrid | agentic` on
   Managers/Engines; agentic-episode steps in dynamic views carry goal +
   manifest reference, never internal steps.
8. **WebApp diagrams.**
   - Design mode: agentic episode = dashed box containing the manifest tool
     palette; dashed unnumbered edges = may call, any order/multiplicity;
     solid constraint edges between tools where invariants are compiled into
     verbs/tokens (renders the guaranteed partial order, not a sequence).
   - Replay mode: recorded tool-call trace renders as an ordinary numbered
     call chain in the existing step-through UX. Requires episode trace
     capture (verbs already record `applied_mutations`; extend to a per-run
     trace artifact).

## Verification

One real construction activity through the local dry-run substrate
(CONSTRUCTION_DRYRUN) where every project.json change arrives via a verb tool;
diff of `applied_mutations` + trace vs. resulting state.

## Deferred

- Approach C: first-class committed manifests in systemDesign (adopt once
  manifest derivation is proven by tooling that consumes it).
- Server-hosted internal MCP transport for the hosted substrate.
- Estimation doctrine interactions (see estimation follow-up memory) untouched.
