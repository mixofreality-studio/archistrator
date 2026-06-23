# Add Use Case

> Add a new use case to an existing Method-designed product. Decide whether it fits the existing decomposition (just a new call chain) or requires architectural change.

archistrator is a single Go-server repo; canonical state is the typed JSON aggregate in `.aiarch/state/project.json` (git-as-DB). The architecture is the typed `System` in `.systemDesign`; Structurizr DSL is a render-on-read of it. There are no `designs/<product>/*.md` files.

**Skill reference:** Invoke `the-method` skill.

## Usage

```
/add-use-case <product>
```

## Prerequisites

The committed **systemDesign** artifact must exist in `.aiarch/state/project.json` → `.systemDesign`.

## Workflow

### Step 1: Capture the use case

Dispatch `product-manager`:

> Ask the user to describe the new use case as a *required behavior*:
>   - Who is the actor?
>   - What is the trigger?
>   - What is the outcome?
>   - What are the success/error paths?
>
> Strip solutions-masquerading-as-requirements. If the user says "we need
> a notification service," ask what behavior — usually "user must learn
> about state changes promptly."

Carry the captured behavior as a pending use-case entry on the working `CoreUseCases` draft (not a `new-use-cases-pending.md` file).

### Step 2: Core or regular?

Dispatch `product-manager` + `system-architect` together:

> Is this a new *core* use case, or a *regular* use case (a variation of
> an existing core use case)?
>
> Core use cases are rare. Most "new use cases" are variations. Ask:
>   - Does this represent a fundamentally new behavior, or a permutation
>     of an existing core behavior?
>   - Does it require new business activities, or just new orchestration
>     of existing activities?

If **regular** → proceed to Step 3 (low cost).

If **core** → proceed to Step 4 (potentially high cost).

### Step 3 (regular use case): New call chain only

Dispatch `system-architect`:

> Can this use case be supported by composing the *existing* components,
> with at most:
>   - one new Manager method (workflow change)
>   - one new ResourceAccess verb if necessary
>
> Draft the call chain. Add it as a new `DynamicView` to the typed
> `System` in `.aiarch/state/project.json` → `.systemDesign`.
>
> Update `.coreUseCases` only if this is a notable new variation worth
> tracking; otherwise leave alone.
>
> Then validate the new dynamic view against the convention rules (skill
> file). If it can't be drawn cleanly using existing components, escalate
> to Step 4.

Estimate the implementation cost (probably small): one Manager method, one
or two construction activities. Recommend appending these as activities to
the typed `ActivityList` in `.activityList` rather than a full re-design.

### Step 4 (core use case or won't fit): Re-validate decomposition

Dispatch `system-architect`:

> The existing decomposition does not cleanly support this use case as a
> regular variation. Two possibilities:
>
>   a) A volatility was missed in the original decomposition. Add a new
>      `Component` encapsulating it (apply the Four Questions) to the typed
>      `System` in `.systemDesign`; the missed volatility should also be
>      recorded in `.volatilities`.
>
>   b) The nature of the business has changed (rare). Significant
>      architectural change is warranted. Treat as a partial
>      re-architecture: re-run `/system-design <product>` for the
>      affected subsystem.
>
> Record the decision rationale in the affected artifact's `Notes` (and the
> commit message / PR body for the session branch) — not a
> `decisions/<date>-<topic>.md` file.

If (a): add new components + new call chain. Recommend running
`/sdp-review <product>` because the project plan likely needs revision.

If (b): tell the user this is a significant event; recommend full
re-architecture of the affected area.

### Step 5: Update project plan if needed

If new components or significant new activities are required:

Dispatch `project-manager`:

> Add new activities to the typed `ActivityList` (`.activityList`) and the
> typed `Network` (`.network`):
>   - For each new component: detailed-design + construction + integration activities
>   - Update dependencies of existing activities if they now depend on new components
>
> Recompute floats. Check if the critical path moved. If duration
> changes by more than a week, recommend `/sdp-review` to surface
> options to management.

### Step 6: Report

Show user the diff:

- Use cases added: ...
- Components added/modified: ...
- Activities added: ...
- New estimated duration vs. previous: ...

Recommend next step:
- If trivial: just resume `/implement-project`.
- If material: run `/sdp-review` first.

## Error handling

- **Use case can't be drawn as a call chain even after adding a component** → the use case itself may be incoherent; ask product-manager to refine.
- **Adding a component pushes Manager count > 5** → consider introducing subsystems.
- **Architecture change requested but it's actually just a feature request** → reject; this is a regular variation, do Step 3 instead.
