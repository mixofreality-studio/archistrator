# /scrubbed-requirements-critique

> PM critique of the drafted **Scrubbed Requirements** artifact — one design-rail CI job. Verdict only; the PM never rewrites the model.

**Arguments** — none. Kind, job mode, branch, and project come from the ambient `AIARCH_*` env.

**Agent + skills.** **Adopt the `product-manager` role**: read `.claude/agents/product-manager.md` and act per that charter yourself, in this session. Do NOT dispatch a subagent or use an Agent/Task tool — the CI job runs single-session; you ARE the product-manager. Judge against **[[the-method-requirements-analysis]]** and [[the-method-project-state]] for the tool flow.

## Steps

1. **Read the draft** with `getDraftSlot` and its committed predecessors with `getCommittedSlot`.
2. **Read the ledger first** with `getReviewThread`. If you have critiqued before, critique the **delta** since your last verdict — not the artifact from scratch. Read `getCritique` to see your own prior verdict and notes — critique the delta against them.
3. **Apply verdict discipline** (anti-thrash — binding):
   - "revise" REQUIRES new, actionable comments tied to specific artifact content.
   - Never relitigate a resolved thread: if the architect responded to your comment, either accept the response or approve-with-noted-reservation. Repeating an already-answered comment is a defect.
   - Severity honesty: only defects against the mission/requirements justify "revise". Taste-level preferences are recorded as comments on an **approve**.
4. **Record the verdict** with `setCritiqueVerdict` (approve/revise + comments). You MUST record your verdict with `setCritiqueVerdict` before finishing — a critique job that ends without recording a verdict fails the pipeline.
5. **Finish** with `publishDraft` (exactly once). You have no `putDraftModel`; do not attempt to fix the model yourself.
