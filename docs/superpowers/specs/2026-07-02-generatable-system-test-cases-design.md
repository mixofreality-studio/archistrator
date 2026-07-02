# Generatable system test cases — design

Turn the System Test Plan (N-STP) from basic use-case flows into real, generatable
**test cases** — each carrying concrete inputs, expected outputs (or expected
errors), and adversarial (negative / boundary) variants — grounded in Löwy's testing
doctrine. The enriched flow is the source a harness (N-STH) later generates test
code from.

## Doctrine (Righting Software — ch09/11/14, App A)

A test case is a **falsification attempt**, not a happy-path traversal: `(inputs,
condition/boundary, expected output OR expected error)`, traced to a **core use
case**, adversarial ("all the ways to break the system and prove it does not work"),
asserted **black-box at the client surface**. Negative/boundary cases are the point
of a test engineer, not optional. Test-engineer authors the plan (N-STP) and harness
(N-STH) early/high-float; software-tester runs the terminal N-IT; QA owns process
(N-QA) — these stay distinct.

## Surface & dependency (decided)

- A **scenario = one core use case** (a WebClient op: `driveDesignPhase`,
  `superviseConstruction`, `operateDeliveredSystem`, `managePaymentLifecycle`,
  `readOperatedSystemView`).
- Its steps are the concrete **manager-operation calls** driven over the wire (the
  generated client-facing API). The manager/engine/RA contracts already carry 111
  operations with typed `params` + `result` JSON Schemas — that is where each step's
  inputs/expected come from.
- **N-STP now depends on the contracts it drives** — add `C-CW` (WebClient build,
  which folds in the client contract) and the driven manager construction activities
  to N-STP's `dependsOn` in `.network.dependencies`. Consequence: N-STP is no longer
  requirements-only; its start moves later and CPM / critical-path recompute. Do this
  via the-method-network-draft, not a hand edit.

## Model (Go — `server/internal/resourceaccess/projectstate/phaseartifacts.go`)

Add a **case layer** between scenario and steps (negative/boundary are first-class,
per doctrine):

```
TestScenario (per core UC)  { id, useCase, title, description, cases: []TestCase }
TestCase   { id, kind: "happy"|"negative"|"boundary", title,
             proves,           // what this proves / the failure it exposes
             steps: []TestStep,
             expectedOutcome }  // overall success, OR the specific expected failure
TestStep (enriched) {
  seq, component, operation, status,   // existing
  inputs:  []TestArg { name, value, schemaRef? },   // concrete values (generatable)
  expect:  TestExpect { result?: raw JSON value/shape, error?: { expected, codeOrType } },
  assertion: string,                                // human-readable
}
```

Concrete `value`s (not just types) are required so a harness can emit runnable tests.
`TestScenario.Steps` (flat) is replaced by `TestScenario.Cases[].Steps`; the read
model / mapper / OpenAPI view mirror the new shape, and the webApp type regenerates
from OpenAPI. Regeneration runs through the existing tooling (`server/cmd/modelgen`
/ `schemagen` / `shapegen` → quicktype); `contract.gen.go` and the webApp `schema.ts`
are outputs, not hand-edited.

## Authoring

The N-STP author (test-engineer / the-method-testing skill, dispatched as an agent)
writes cases by reading each driven op's **contract schema** (for inputs/expected) +
the **dynamic view** (for call order) + adversarial reasoning (negative/boundary).
This slice authors **exemplar** cases for 1–2 core UCs to prove the pipeline
end-to-end; full authoring of all 5 UCs is a follow-on test-engineer activity.
Generating the actual test CODE (the N-STH harness consuming this) is downstream and
out of scope here.

## Rendering (webApp)

Extend the existing layered step-through on N-STP (reuses `DynamicViewFlow` /
`ScenarioBrowser`):
- a **case selector** (happy / negative-X / boundary-Y) beside the scenario picker;
- the step caption shows **inputs → expected** (expected-error styling for negative
  steps);
- a case-level **"what this proves / expected outcome"** banner.

## Scope

- **This slice:** enriched model (Go struct + read-model view + OpenAPI + regen) →
  webApp types + renderer → author exemplar rich cases for 1–2 core UCs → wire the
  N-STP→contract dependency.
- **Deferred:** full authoring of all 5 UCs; the N-STH test-code generation; back-
  filling every contract's params.

## Risks / notes

- The Go→OpenAPI→webApp regen pipeline is the main implementation risk; understand
  `server/cmd/{modelgen,schemagen,shapegen}` before changing structs, and run the
  regen rather than hand-editing generated files.
- Replacing `TestScenario.Steps` with `Cases[].Steps` is a breaking model change;
  the existing committed N-STP content in `project.json` must be migrated to the new
  shape (the exemplar authoring covers this).
- `webApp` currently reads `scenario.steps`; the ScenarioBrowser→DynamicViewModel
  mapping moves to `case.steps`.
