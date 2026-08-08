# System Audit Log — Design

- **Date:** 2026-08-01
- **Author:** system-architect (driven), founder (ratified)
- **Status:** Approved design — ready for implementation planning
- **Phase target:** cross-cutting (spans ResourceAccess, Engine, Manager, Client, SPA, codegen)

## 1. Motivation

Archistrator dispatches AI agents that write and merge code into customer repositories. Today
nothing durably records what those agents did, what a human authorized, or who was accountable
for either. We want one audit log that answers, for any change: **what happened, when, where,
who or what caused it, what the outcome was, and who authorized it** — usable four ways:

1. **Our own SIEM / security ops** — detect an agent behaving outside its envelope.
2. **Engineering analytics** — tokens, tool calls, rework ratio (the calibration loop).
3. **Customer-facing trust surface** — "here is everything an AI did to your repo."
4. **SOC 2 evidence, including for the customer's own audit** of the app archistrator built.
Consumer (4) is the strictest and therefore drives the design. Everything else is a view over the
same record.

**Not a consumer of this pipeline: archistrator's own performance analysis** (episode durations,
tool-call counts, which validations an episode fought through, prompt-change A/B). It consumes the
same *capture source* but is a separate pipeline — see §4.

## 2. Current terrain (reconnaissance, verified 2026-08-01)

**Nothing is captured today, on either venue.**

**Local venue.** `claudeArgv`
(`server/internal/resourceaccess/agenticjob/agenticjobaccess.go:2295`) invokes
`claude --dangerously-skip-permissions --settings <sandbox> --mcp-config <cfg>
--strict-mcp-config --output-format json -p <prompt>`. Stdout is buffered (`:1592`), but
`claudeResultEnvelope` (`:1977`) decodes only `subtype/is_error/result/error`, and the output is
mined **only on the failure path** — on success it is discarded. Note this package was renamed
from `constructionpipeline` to `agenticjob` after the 2026-07-20 spec was written; that spec's
file references are stale.

**GH venue.** `.github/workflows/aiarch-construct.yml` runs `anthropics/claude-code-action@v1`
with `show_full_output: true`. The action declares outputs `execution_file` ("Path to the Claude
Code execution output file"), `session_id`, `structured_output`, and `branch_name` — **none of
which the workflow consumes**. It also accepts a `settings` input (JSON string or path). GH
capture is therefore a handful of workflow lines, not a subsystem; the 2026-07-20 spec deferred
it as hard, and that assessment was wrong.

**Codegen rail (the key seam).** `server/cmd/modelgen` is a thin CLI shim over the platform
library `framework-go-app-generator/modelgen`. It reads `.serviceContracts` from
`.aiarch/state/project.json` and emits `<goPackage>/contract.gen.go`, then makes **a second,
symmetric pass** (`modelgen.GenerateFakes`) emitting `<goPackage>/fake/fake.gen.go` for every
contract with an interface. `server/cmd/clientgen` separately emits per-manager REST handlers
and MCP tools plus a component-agnostic auth middleware, from the same 5 web-exposed manager
contracts (systemDesign, projectDesign, project, construction, operations).

**Durable-execution topology (the load-bearing seam).** Manager methods **are Temporal workflows**
— `constructactivity.go`, `pumpnextactivity.go`, `coauthorartifact.go` are workflow code, so they
are deterministic and cannot perform I/O. `temporalgen` emits, per manager: `activities.gen.go`
(one Temporal Activity per operation of each I/O dependency — "the manager's architecture-approved
call surface"), `invokers.gen.go` (`genInvokers`, the workflow-side typed call surface, so no
string activity names appear in hand-written workflow code), and `worker.gen.go`.
`genActivityIdempotencyKey` already derives a run-scoped `workflowID:runID:activityID` key per
logical write.

Child workflows are already idiomatic here: `ExecuteChildWorkflow` with
`ParentClosePolicy: PARENT_CLOSE_POLICY_ABANDON` appears at `pumpnextactivity.go:98`,
`pumpsweep.go:107`, and `systemdesignphase.go:64`.

**Identity propagation already exists.** `temporalprop.NewPrincipalPropagator()` is registered as a
Temporal `ContextPropagator` (`server/cmd/server/main.gen.go:361`), so the calling principal
already flows transport → workflow → activity. A W3C trace propagator and OTel globals are
installed at `:335`. The delegation chain in §5.4 rides existing machinery.

**No mutation metadata exists.** `.aiarch/state/project.json` carries no `readOnly` / `mutates`
flag on contract operations — verified by inspection. §5.2's event selection depends on adding one.

**Managers:** billing, construction, operations, projectdesign, systemdesign.
**Engines:** autoscaler, billing, designhealth, estimation, intervention, operationestimation,
review.

**Prior recon, inherited from the 2026-07-20 spec (not re-verified here):** the `usageAccess`
ledger reserves a `construction-token` unit with zero writers and is Postgres/cloud-only;
`ConstructionProgress.Points[].AcPct` has no writer; `EstimateOverrun` is never raised; and
`ReplanSweepWorkflow.flagVariances` is a `nil` stub. Also note the durable-execution
ResourceAccess is mid-move to `server/internal/utility/messagebus` on the current branch.

## 3. Standards basis

The content and shape of the record are decided by standards rather than by preference.

- **[NIST SP 800-53 AU-3](https://csf.tools/reference/nist-sp-800-53/r5/au/au-3/)** — an audit
  record establishes *what* type of event occurred, *when*, *where*, the *source*, the
  *outcome*, and the *identity* of individuals/subjects/objects involved. Supporting content
  includes process identifiers, success/fail indications, and filenames involved. **Payload is
  not required.** The standard-specified audit record is metadata.
- **[AU-2](https://csf.tools/reference/nist-sp-800-53/r5/au/au-2/)** — event selection is an
  explicit organizational decision. Ours is recorded in §5.2.
- **[AU-9](https://csf.tools/reference/nist-sp-800-53/r5/au/au-9/)** — audit information is
  protected by cryptographic means, restricted access, and storage separate from the audited
  system. This is why the trustworthy store is server-side and unreachable from the agent.
- **AU-10** — non-repudiation via digital signatures. Satisfied by §5.6 checkpoint sealing.
- **[AU-11](https://csf.tools/reference/nist-sp-800-53/r5/au/au-11/)** — retention is
  organization-defined.
- **[OWASP ASVS V7.1 / V7.2](https://github.com/OWASP/ASVS/blob/master/4.0/en/0x15-V7-Error-Logging.md)**
  — log all authentication decisions (success and failure), all access-control failures, and
  input-validation failures; never log credentials or payment details; store session tokens only
  in irreversible hashed form. Drives §5.9.
- **[OpenTelemetry GenAI semantic conventions](https://github.com/open-telemetry/semantic-conventions-genai)**
  — content capture is **opt-in, never default**, and the spec blesses three patterns: omit
  content, record it inline, or store it externally and record a reference. It warns that tool
  definitions and message arrays are large enough to be a problem on their own.
- **[OWASP Agent Observability Standard](https://aos.owasp.org/spec/trace/extend_ocsf/)** —
  OCSF has no native AI/agent event class (schema is at 1.9.0-dev). The emerging practice maps
  agent activity onto **Application Activity / API Activity (`category_uid: 6`,
  `class_uid: 6003`, `type_uid: 600301`)**, sets `actor.user.type_id: 99` for an AI agent, and
  carries agent specifics in an extension namespace.
- **[EU AI Act Art. 12 / 26(6)](https://www.firetail.ai/blog/article-12-and-the-logging-mandate-what-the-eu-ai-act-actually-requires)**
  — automatic logging across the system lifetime, full traceability, **≥6 months retention**,
  deployer accountable regardless of who built the system. In force for high-risk systems from
  2026-08-02.

**Derived rulings.** OCSF is our wire schema. OTel supplies vocabulary (`gen_ai.*`) but is
**never the system of record** — its transport is batched and best-effort, and it is not
customer-exposed as evidence. There is **no content tier**: the audit record is metadata, and
the repository's own git history is the content record the customer already owns.

## 4. Goals and non-goals

**Goals**
- One audit record shape covering every auditable operation, whoever or whatever caused it, plus
  the access events (authn/authz decisions) that never reach an operation.

**Explicitly a separate pipeline: performance analysis / prompt optimization.** It shares the
capture source (§5.2 Layer 2) but not the pipeline, on a volatility test rather than convenience.
Evidence changes when regulation or a standard changes — slowly, externally driven, adversarially,
and it must stay immutable and sealed. Analysis changes whenever we form a new hypothesis —
constantly, internally driven, and it wants to reshape, backfill, and discard data freely. They
also differ on the second axis: audit varies with each customer's regulatory posture, while
analysis does not vary by customer at all. Coupling them would mean mutating a compliance-critical
schema to chase prompt experiments, and would force exploratory work onto an append-only sealed
store. Analysis therefore reads the capture source, **not** the audit store.

The decomposition is three volatilities, not one: *how we observe what happened* (venue-specific —
`agenticjob`), *how evidence is retained and protected* (this spec), and *what we measure and how
we learn* (a separate spec).
- Structural completeness: audit emission is generated, not hand-written.
- Both venues captured; identical features on local and deployed, with differing assurance.
- Tamper-evident storage with signed checkpoints on the deployed profile.
- Gaps recorded explicitly — a missing record is never silently missing.
- A UI in v1 sufficient to validate the above against real state.

**Non-goals (v1)**
- SIEM forwarder / OCSF export pipeline (phase 2).
- Customer evidence-package export (phase 2).
- Rewiring the calibration loop onto this store (separate spec — see §9).
- Content capture of prompts, responses, or file contents (cut deliberately; §3).
- Read-operation auditing beyond opt-in per op (§5.2).
- Retention enforcement/pruning. Metadata is small; revisit before it bites. This does not put us
  below the AI Act floor — retaining everything trivially satisfies a ≥6-month minimum. What is
  deferred is the *deletion* policy, not the *keeping* of records.

## 5. Design

### 5.1 The unit is an auditable operation, not an agent episode

An **agent episode is one kind of auditable operation** — a long-running one that contains
sub-events. Human approvals, review comments, waivers, ratifications, scope changes, merges, and
scheduled system actions are equally first-class and are not satellites of an agent run.

In the Method, **Manager operations are the use-case entry points**. So "audit every auditable
operation" resolves to "audit every state-changing Manager operation," which is a contract-level
property rather than a discipline-level one.

An **episode** remains a correlation scope: events emitted during an agent run carry its
`episode_id`. Events from direct human action carry none.

### 5.2 Two capture layers

**Layer 1 — structural, complete by construction.** For every **state-mutating** ResourceAccess
operation, `temporalgen` emits a **generated child workflow** that performs the business activity
and then the audit activity. Workflow-side callers invoke that child through `genInvokers` instead
of the bare activity; read-only operations remain plain activities.

Three properties follow, and each replaces something that would otherwise be a hope:

- **Pairing is structural.** A business write and its audit record are one generated unit that
  cannot be invoked halfway. Two sibling activities would make pairing an emergent property of
  adjacent generated calls; a child workflow makes it a fact of the call graph.
- **Audit failure cannot corrupt business control flow.** The audit activity carries its own,
  effectively unbounded retry policy. Putting the audit write *inside* the business activity would
  mean a failed audit returns an error for a business write that already succeeded — the workflow
  would be told an operation failed when it did not.
- **Durability is inherited, not invented.** Once the business activity completes, Temporal has
  durably recorded it in the child's history, so the subsequent audit activity is guaranteed to
  run eventually. That is the transactional-outbox property, supplied by the durable rail rather
  than by a hand-built outbox.

This is the established idiom in this codebase, not new machinery: `ExecuteChildWorkflow` with
`ParentClosePolicy: PARENT_CLOSE_POLICY_ABANDON` is already used at
`server/internal/manager/construction/pumpnextactivity.go:98`, `pumpsweep.go:107`, and
`server/internal/manager/systemdesign/systemdesignphase.go:64`. ABANDON matters here: an abandoned
child outlives parent termination and still completes its audit write.

Two implementation constraints. Child workflow IDs must be derived deterministically from the
parent ID plus a sequence, so a parent replay cannot spawn duplicates — the same discipline
`genActivityIdempotencyKey` already applies to activities. And the ContinueAsNew interaction must
be preserved; `pumpnextactivity.go:44` documents the existing determinism requirement against the
child-execution command sequence.

Because generation is driven from `.serviceContracts`, a newly added mutating operation is audited
without anyone remembering to do anything, and a drift gate can assert every mutating operation
routes through a generated child. Same enforcement shape as the existing encapsulation and layer
gates.

**Costs, stated plainly.** One workflow execution per state write is heavier than one activity and
makes the Temporal UI noisy with many small executions. Runtime overhead is negligible in this
system specifically because these workflows are dominated by multi-minute agent episodes rather
than tight write loops, but that is a property of the current workload, not a general one — revisit
if a high-frequency write path appears.

**Event selection (AU-2):** all state-mutating operations are audited by default. Read operations
are audited only when the contract marks them opt-in — reads are per-page-load volume, and "who
changed what, authorized by whom" is the question that matters. **This flag does not exist today**:
`.aiarch/state/project.json` carries no `readOnly`/`mutates` metadata on contract operations, so
adding it is real work touching every contract plus `modelgen`.

**The intent record.** "Who asked for what" is emitted as the **first** audit activity the manager
workflow schedules, not at the transport boundary. Caller identity is already available inside the
workflow: `temporalprop.NewPrincipalPropagator()` is registered as a Temporal ContextPropagator at
`server/cmd/server/main.gen.go:361`, and a W3C trace propagator is installed at `:335`. One
mechanism, one generator, no transport middleware.

**Layer 2 — observational.** What an agent did *outside* our Managers: file edits, bash commands,
LLM turns, token usage. Derived strictly from **what the supervisor observed**, never from agent
self-report:

- **Local:** `agenticjob` switches to `--output-format stream-json --verbose` and tees the event
  stream, parsing `tool_use` / `tool_result` / per-turn `usage` / the terminal `result` event.
  The raw stream is written to the RA's existing durable log dir for ops debugging; it is not an
  audit tier and is not committed.
- **GH:** the server **pulls** `execution_file` from the run it already observes via
  `PipelineObservation`, rather than accepting an in-job push, so a failing or compromised job
  cannot skip its own audit trail.

Agent MCP tool calls are already Manager operations and are therefore covered by Layer 1; Layer 2
is strictly the additional detail about what happened between them.

**The trust rule: the audit write path is never an MCP tool.** If a `recordEpisode` verb were
exposed alongside `recordServiceContract`, an agent could forge or omit its own audit trail and
the log would be theater. This is a direct correction to the 2026-07-20 spec, which proposed
exactly that.

### 5.3 The record

One record shape, carrying the AU-3 six fields, emitted under the OCSF class appropriate to the
event: **API Activity** (`category_uid: 6`, `class_uid: 6003`, `type_uid: 600301`) for operations
and state changes, and **Authentication (3002)** / **Authorize Session (3003)** for the access
events of §5.9. The AU-3 core and the archistrator block are identical across all three — only the
classification and the agent block differ — so a consumer parses one shape.

The sketch below is the 6003 form.

**Naming rule: use OCSF core fields wherever OCSF has them, and AOS's `unmapped.aos.*` naming
for the agent-specific remainder.** OCSF core covers the majority of the record — `actor`, `api`,
`status_id`, `time`, `duration`, `src_endpoint`, `metadata` — and those are what any OCSF
consumer already parses. For the remainder, following AOS verbatim means a SIEM that understands
agent observability finds our fields where it expects them; a private namespace would be
semantically tidier but interoperable with nobody. Archistrator-only concepts that AOS has no
slot for (venue, assurance, activity/component/contract ids) sit under `unmapped.aiarch.*`,
clearly separated from the AOS-standard block.

Sketch:

```
AuditEvent {
  # ---- OCSF core ----
  # AU-3: what
  class_uid: 6003, category_uid: 6, type_uid: 600301, activity_id
  api.operation        string    # manager op name, or: dispatch | llm_request | tool_use |
                                 #   permission_decision | artifact_publish | review_verdict | merge
  api.service          { name }  # MCP / A2A service when the op crossed one
  # AU-3: when
  time, duration
  # AU-3: source + identity
  actor.user           { uid, name, type_id }   # 99 = AI agent; human and system otherwise
  actor.invoked_by     { uid, name }            # delegation chain (§5.4)
  src_endpoint                                  # runner / host
  # AU-3: outcome
  status_id, status_detail, error_type
  metadata { version, product, ocsf { version }, trace_id, span_id, parent_span_id }

  # ---- AOS agent block (unmapped.aos.*, verbatim AOS naming) ----
  context              { agent, session { id }, model { id, provider { name } } }
  tool_call            { name, arguments }
  step                 { id, type, turn_id, reasoning,
                         operation { tool { id, execution_id, inputs, outputs, is_error } } }

  # ---- archistrator-only (unmapped.aiarch.*) ----
  venue                enum      # local | github | platform
  repo, ref, run_id
  episode_id, sequence           # sequence is per-episode: ordering + at-least-once dedupe
  worker_class
  activity_id, component_id, contract_id
  tool_decision        { decision, source }   # config | hook | user_permanent | user_reject | ...
  tokens               { in, out, cache_read, cache_create }
  skill, agent, command
  prompt_pin           string    # generation of the seated .claude surface — provenance for an
                                 #   AI-authored change (§5.3a)
  assurance            enum      # local-selfhosted | supervised-deployed
  content_ref          string?   # NULL in v1 — seam preserved, tier not built
}
```

Note `tool_call.arguments` and `step.operation.tool.{inputs,outputs}` are AOS-defined slots that
we deliberately populate with **metadata only** (target paths, command, sizes) and never with
file contents or model text — §3's no-content-tier ruling, and consistent with the OTel GenAI
opt-in stance. Populating a standard field does not oblige us to fill it with payload.

**§5.3a — `prompt_pin` justification.** This records the generation of the seated `.claude` prompt
surface: `AIARCH_STATE_MCP_PIN` on the GH venue, where `aiarch-construct.yml` already stamps it on
the stated grounds that "the pin IS the provenance," and the server build locally, where
`agenticjobaccess.go:1629` seats from the server's own binary.

It earns its place on **audit** grounds, not analytics ones: "which prompt generation authored this
change" is provenance for an AI-authored change, and it is what an auditor asks when probing how
agent behavior is controlled — it would belong here even if no one ever optimized a prompt. It is
also what makes prompt drift detectable, which matters precisely because a prompt change is not
itself a code change in the audited repository. Called out because a field that *also* serves the
sibling analysis pipeline invites later confusion over ownership: this pipeline owns it.

`decision` and `decision_source` come from Claude Code's permission model
(`config` / `hook` / `user_permanent` / `user_temporary` / `user_reject` / `user_abort`) and are
the field a security reviewer reads first.

### 5.4 Actor model and the delegation chain

`actor` is one of **human**, **agent**, or **system** (scheduled/automated). Agent-caused events
carry `actor.invoked_by` naming the human who authorized the work — recovered from `project.json`
(who ratified the artifact, who approved the dispatch).

This chain — *human ratified → server dispatched → agent acted* — is the accountability story an
auditor actually wants, and it is the one thing no LLM-side telemetry source can produce. It is
the reason Layer 1 is the spine and Layer 2 is supplementary.

### 5.5 Storage: one contract, two substrates

A new **`auditAccess`** ResourceAccess encapsulates *where evidence is stored and how it is
protected*:

| Profile | Substrate | Assurance |
|---|---|---|
| **deployed** | Postgres, append-only (app role holds no UPDATE/DELETE grant) | `supervised-deployed` |
| **local** | append-only JSONL under `.aiarch/audit/`, **gitignored** | `local-selfhosted` |

Identical contract, identical features, identical schema. The local file is deliberately not
committed: per-branch audit files would conflict on every merge, and local runs are for
development and validation, not evidence. Every record carries its `assurance` value, so a future
export can never overclaim — an auditor reads the assurance level off the evidence itself.

Operations: append batch, query by filter, seal checkpoint.

### 5.6 Integrity (AU-9 / AU-10)

The deployed profile writes **signed checkpoints**: periodically, a record covering a sequence
range and a Merkle root over that range, signed server-side. This gives non-repudiation and
tamper-evidence without per-row signing cost, and verification is a single pass.

The local profile uses the same schema and emits no checkpoints; it is labeled, not pretended.

### 5.7 Failure handling and gaps

Ordering within the generated child workflow is **business write first, audit write second**. A
record claiming something happened when it did not is a correctness violation; a missing record is
a completeness violation, which §5.6's sequence numbering makes detectable. Under-reporting that we
can detect beats over-reporting that we cannot.

Durability comes from Temporal, not from a hand-built mechanism: once the business activity
completes it is in the child's history, so the audit activity is guaranteed to run. Its retry
policy is effectively unbounded and independent of the business retry policy, so an audit store
outage delays records rather than losing them. Dedupe on redelivery uses `episode_id` + `sequence`,
with the deterministic child workflow ID preventing duplicate executions on parent replay.

This narrows genuine loss to a small set: a child workflow **terminated** mid-flight (ABANDON keeps
it alive through *parent* termination, but not its own), and Layer-2 observations whose source
artifact expired before the pull. Both produce explicit gap records.

**If the store is unreachable past the retry budget, an explicit audit gap record is written for
that episode, with a reason.** The same applies to a GH run whose artifact expired or whose run
was deleted before the pull. Provable completeness matters far more to an auditor than perfect
capture: a log that quietly drops records is worse than one that says where it lost them.

### 5.8 The UI (v1)

A thin **`auditManager`** owns the "review the evidence for an AI-built project" use-case family
— which phase 2 extends with export and SIEM forwarding rather than replaces. Two operations,
flowing through the existing `clientgen` rail so REST handlers, MCP tools, OAS, and typed client
come out generated:

- **list episodes for a target** (activity id or design artifact kind)
- **get the event timeline for an episode**

Two SPA surfaces sharing one component, on **both** construction activity pages and design
artifact pages — the design rail is the faster loop to validate against, and the marginal cost
over one rail is only the emission wiring:

1. **Episodes panel** — per episode: outcome, duration, model, worker class, token totals
   (in/out/cache), tool-call count, and two badges: **assurance** and **completeness**
   (sealed / unsealed / **has gaps**).
2. **Episode timeline** — ordered events: `llm_request` turns with per-turn tokens; `tool_use`
   rows with tool name, target paths, permission decision and its source, outcome. Filterable by
   operation type.

**The badges are the point.** Token counts are easy and boring to verify. The property that
needs eyeballing is provable completeness — that gaps surface rather than hide — so the
acceptance test kills the audit store mid-episode and confirms the panel shows a gap instead of
a clean-looking short list.

### 5.9 Access events at the request boundary

State-change auditing (§5.2) only sees requests that reach a workflow. A request rejected at
authentication or authorization never starts one, so without a second path it leaves no trace —
and that is the event a SIEM cares about most.

**[OWASP ASVS V7.1/V7.2](https://github.com/OWASP/ASVS/blob/master/4.0/en/0x15-V7-Error-Logging.md)**
requires logging all authentication decisions (success *and* failure), all access-control failures,
and input-validation failures. It equally forbids logging credentials or payment details, and
requires session tokens to appear only in irreversible hashed form. **OCSF classes these
separately from 6003**: **Authentication (3002)** and **Authorize Session (3003)** in the Identity
& Access Management category; API Activity (6003) covers calls whose authorization already matched
an ALLOW.

So access events are emitted at the transport boundary — `clientgen`'s existing
`middleware.gen.go`, where authentication already runs — as OCSF 3002/3003 records, buffered and
**best-effort**.

**Reliability class matches consequence class.** A state change gets the guaranteed path because a
lost record means an unaudited change to a customer repository. An access event gets the
best-effort path because a lost record means a lost observation of something that changed nothing.
Spending workflow machinery per HTTP request would add latency and cost to buy a guarantee that
protects nothing.

This does not reinstate the transport-boundary *intent* record rejected in §5.2 — intent for a
state change still originates inside the workflow. These are different events, different OCSF
classes, and different durability requirements.

Records here carry no request bodies and no tokens; anything token-shaped is hashed, per ASVS.

## 6. Layering

| Layer | Additions |
|---|---|
| **ResourceAccess** | **`auditAccess`** (new): append/query/seal, two substrate profiles. `agenticjob` (extend): stream-json tee locally, `execution_file` pull on GH, both reporting observations only. |
| **Engine** | **`auditEngine`** (new): normalize Layer-1 operations and Layer-2 observations into canonical records, map to OCSF, compute and verify checkpoint seals. |
| **Manager** | **`auditManager`** (new, thin): list episodes, get timeline. Existing construction / projectdesign / systemdesign Managers emit via generated child workflows — no hand-written audit call sites. |
| **Client/SPA** | Generated REST + MCP ops and OAS for `auditManager`; access-event emission (OCSF 3002/3003) in the generated `middleware.gen.go`; episodes panel + episode timeline components; wired into activity and design-artifact pages. |
| **Codegen** | `temporalgen`: per-mutating-op child workflow (business activity + audit activity), the audit activity itself, and `genInvokers` entries routing mutating ops through the child. `modelgen`: the new `mutates` contract flag. Drift gate asserting every mutating op routes through a generated child. |

**Known cost:** the emitters live in the platform repo, so this needs a `temporalgen` release (and
a `modelgen` release for the contract flag) coordinated with the server pin. That has been a
friction point before and should be sequenced first in the plan.

**Placement constraint (load-bearing — decide now, expensive later).** The audit spine is built in
the **platform** — `framework-go`, `temporalgen`, `app-generator` — with archistrator as its first
dogfood consumer. It is *not* archistrator-app-local code that a generator later learns to copy.

The consequence is that **every app archistrator builds inherits the audit log for free**, which is
what lets a built app produce its own audit evidence (§8) rather than needing one bolted on
afterward. Honoring this costs nothing now
because the emitters are platform-side anyway; retrofitting it after the fact means re-homing
working code across a repo boundary and a release cycle.

## 7. Testing

Per the-method-testing:

- **Contract tests** on `auditAccess`, run against both substrate profiles.
- **Golden-fixture tests** in `auditEngine`: real captured stream-json and a real
  `execution_file` mapped to expected OCSF records. Fixtures are taken from actual runs, not
  hand-authored — hand-authored fixtures encode our assumptions rather than the CLI's behavior.
- **Access-event tests**: a request rejected at authentication and one rejected at authorization
  each produce an OCSF 3002/3003 record; assert no request body, credential, or unhashed token
  appears in any emitted record (ASVS V7.1).
- **Tamper test**: mutate a sealed range; assert checkpoint verification fails.
- **Gap test**: store unreachable past the retry budget; assert a gap record with a reason.
- **Codegen test**: add a mutating operation to a contract; assert the drift gate fails until the
  generated child workflow exists.
- **Workflow tests** (Temporal test framework, as the existing manager tests already use): assert
  the child emits business-then-audit ordering; assert an audit-activity failure retries without
  surfacing an error to the parent; assert a parent replay does not spawn duplicate children;
  assert a terminated parent's ABANDONed child still completes its audit write.
- **System test**: one real local episode end-to-end, asserting record count and presence of
  every AU-3 required field.
- **UI**: Playwright on the existing `uitests` rail, run locally against real state, including
  the gap-badge case. Stops for founder review per the standing UI review loop.

## 8. Phase 2 (deferred, not designed here)

- **SIEM export** — OCSF forwarder, and confirmation of whether an OCSF extension should be
  registered rather than carrying `unmapped.aiarch.*` privately.
- **Customer evidence export** — per-project package, retention policy, and the DPA/privacy
  posture that comes with handing evidence to a customer.
- **Retention enforcement** — AU-11 is organization-defined and metadata is cheap; set a policy
  before volume forces one.
- **Auditor-legible evidence export** — an auditor wants a control matrix mapping each
  trust-services criterion to its evidence with population/sampling support, not a JSONL stream.
  That artifact is what makes consumer (4) real. The criteria this design can actually serve are
  **CC6.x** (logical access — §5.9's authn/authz decision logging), **CC7.2** (monitoring — the
  audit log with AU-3 fields, retention, tamper-evidence), and **CC8.1** (change management —
  every change traced to its authorization, which archistrator has natively because it
  *orchestrated* the change). Plus EU AI Act Art. 12 logging where a built app is itself an AI
  system.

  **Not a compliance product.** SOC 2 is an organizational audit — policies, vendor management, HR
  controls, incident response, an auditor, a Type II window. Technical controls are roughly a
  third of it, so archistrator supplies evidence for a customer's audit; it does not deliver,
  broker, or shortcut the audit itself (founder ruling, 2026-08-01).

**Sibling spec, not a phase of this one:** performance analysis / prompt optimization (§4). It
consumes the capture source independently and needs its own volatility analysis, storage
characteristics, and — for causal rather than correlational reads — a replay harness. Nothing in
it should land on the audit record.

## 9. Relationship to the 2026-07-20 calibration spec

This **supersedes the capture half** of
`2026-07-20-token-usage-calibration-and-tracing-design.md`. Specifically:

- Its `recordEpisode` MCP verb is **withdrawn** — it violates §5.2's trust rule.
- Its committed `.aiarch/traces/<episodeId>.jsonl` content tier is **cut** — §3 removes the
  content tier entirely, and §5.5 explains why a per-branch committed file is the wrong shape.
- Its local-venue-only scope is **superseded**: both venues are captured here.

What survives intact and becomes a **consumer** of `auditAccess`: the frozen prediction baseline
at `sdpCommit`, the per-activity actuals rollup, `AcPct`/CPI/EAC on the EV chart, the
`EstimateOverrun` variance raise, and the end-of-project rateCard calibration advisory. That spec
should be amended to read from this store rather than owning capture; it gets materially smaller
in the process.

## 10. Open questions

1. **AOS namespace prefix.** AOS's own documentation is inconsistent: the OCSF-extension page
   uses `unmapped.aos.*` while the implementation-examples page uses `unmapped.asop.*` — almost
   certainly a rename in flight (ASOP → AOS). This spec picks **`aos`**, matching the current
   project domain. Confirm against the AOS schema repo before implementation, and keep the
   prefix in one constant so a rename is a one-line change.
2. **OCSF extension registration** — registering a formal OCSF extension for the
   `unmapped.aiarch.*` block (rather than carrying it privately) is a phase-2 export question,
   and depends on which SIEM we target first. If OCSF ships a native agent class before then,
   revisit the whole mapping.
3. **Child-workflow volume** — measure executions per construction activity on the pump before
   rollout. The overhead is expected to be negligible against multi-minute agent episodes, but if
   the Temporal UI becomes unusable or namespace limits bind, the mitigation is batching audit
   writes per workflow task — not reverting to an in-activity write, which reintroduces the
   control-flow coupling §5.2 rejects.
4. **Checkpoint cadence** — per N records, per time window, or per episode completion. Needs the
   same volume measurement.
5. **Which reads to opt into** — §5.2 defers the initial opt-in set to implementation. Viewing
   the audit log itself is the obvious first candidate.
6. **System actor identity** — scheduled sweeps (e.g. the replan sweep) have no human in the
   chain. Confirm whether `actor.invoked_by` is null or names the policy that scheduled them.
7. **Local UI without a deployed store** — the local JSONL profile must support the query op
   efficiently enough for the panel. If a project accumulates enough local episodes to make
   linear scans slow, an index file is the fallback.
