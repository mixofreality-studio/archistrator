# ISS-032-2 part 3: the stp task has no typed route for an unbindable contract (2026-11-26)

Parts 1 and 2 (276901f5, 155353d2) make the test_plan task's rule check per contract
(`rule:tp:<contract>`), so a contract with no recorded plan can no longer pass the stp task
vacuously, and a testingState Defect no longer stands in for a plan.

Consequence: RUN-032's viewNoticeAccess contract genuinely cannot be bound (AnnounceListChanged
returns only an error; no designed contract observes it, so TP-EXPECT-REQUIRED and TP-STATE refuse
every binding). The stp task now fails each attempt and stops noProgress there instead of at
integration. The contract defect belongs to the senior-developer's design, and the test-engineer
has no route to send it back.

## What is missing

A `reportContractDefect` verb for the stp task, citing the TP-* findings the plan write was refused
with, opening a system-authored send-back on `designReview` (the shape of `reportUpstreamDefect`
for the architecture draft).

## Why it is not in this change

The route is declared by the lifecycle, not by the app: `SendsUpstreamTo` lives on a dispatch task
in PLAT `method-assets/lifecycles.go`, and `projectstate.UpstreamRouteOf` reads it from the
released method-assets module. Today the test_plan task declares no route, so the verb would be
refused ("declares no upstream route"). It needs, in order:

1. PLAT method-assets: `SendsUpstreamTo: [{designReview-owning type, gate designReview}]` on the stp
   dispatch task, with a lifecycles test. Needs a method-assets release.
2. APP projectstate: a third UpstreamDefect case (contract-scoped TP rule findings, stamped from the
   refusal like StampUpstreamRuleRefs) and its admission in AdmitUpstreamDefects.
3. APP delivery: judge/run the round for a reporter whose route target is designReview of the
   service-detailed-design activity (routeUpstreamDefect / gatesJudging today assume core use cases).
4. APP state-mcp: the verb, registered for the stp task's mode, plus charter/skill text for the
   test-engineer; bump `projectstate.TaskSpecSchemaVersion`; drain workflows before deploy
   (`changeUpstreamDefectRoute`-style fence).

Until then todomvc stops at stp on viewNoticeAccess. Do not close ISS-032-2 on parts 1 and 2 alone.
