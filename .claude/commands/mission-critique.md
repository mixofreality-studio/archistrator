# /mission-critique

> PM critique of the drafted **Mission** artifact — one design-rail CI job. Verdict only; the PM never rewrites the model.

**Arguments** — none. Kind, job mode, branch, and project come from the ambient `AIARCH_*` env.

**Agent + skills.** Work as the **`product-manager`** agent (`.claude/agents/product-manager.md`). Judge against **[[the-method-business-alignment]]** and [[the-method-project-state]] for the tool flow.

## Steps

1. **Read the draft** with `getDraftSlot` and its committed predecessors with `getCommittedSlot`.
2. **Read the ledger first** with `getReviewThread`. If you have critiqued before, critique the **delta** since your last verdict — not the artifact from scratch. Read `getCritique` to see your own prior verdict and notes — critique the delta against them.
3. **Apply verdict discipline** (anti-thrash — binding):
   - "revise" REQUIRES new, actionable comments tied to specific artifact content.
   - Never relitigate a resolved thread: if the architect responded to your comment, either accept the response or approve-with-noted-reservation. Repeating an already-answered comment is a defect.
   - Severity honesty: only defects against the mission/requirements justify "revise". Taste-level preferences are recorded as comments on an **approve**.
   - Mission doctrine you MUST enforce: the mission and vision must describe the BUSINESS CAPABILITY and USER-FACING VALUE in business and user language only. REVISE if the draft uses the words component, module, service, subsystem, layer, or any other system-architecture / software-decomposition terminology, or if it asserts or implies any breakdown of the system into parts — structural boundaries are derived LATER from volatility analysis, so a pre-decided decomposition here is a defect. Do NOT ask the architect to ADD component or architecture language; that is exactly what must be kept out.
4. **Record the verdict** with `setCritiqueVerdict` (approve/revise + comments).
5. **Finish** with `publishDraft` (exactly once). You have no `putDraftModel`; do not attempt to fix the model yourself.
