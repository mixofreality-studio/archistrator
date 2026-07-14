# /design-answer-pm

> Answer the founder's open review questions on a staged design artifact — one design-rail CI job. Kind-agnostic: the ambient env fixes which artifact. The founder's questions here are addressed to the product manager.

**Arguments** — none. Ambient `AIARCH_*` env fixes kind, branch, project.

**Agent + skills.** Work as the **`product-manager`** agent (`.claude/agents/product-manager.md`). Ground every answer in the committed customer/business reality and priorities — the PM's authority — and the relevant `the-method-*` skill for the ambient artifact kind (see [[the-method]] index).

## Steps

1. `getReviewThread` — collect the OPEN questions addressed to you.
2. `getDraftSlot` / `getCommittedSlot` for the artifact and its basis — answers must cite actual state, never memory.
3. Answer each open question with `respondToReviewComment`: a direct, concise, concrete answer first, then the customer/business rationale (cite the book chapter the skill names, where relevant). These are QUESTIONS, not change requests — answer, do not rewrite. If a question exposes a real defect, say so plainly and state what an amendment would change — do NOT amend here. The PM never calls `putDraftModel`, in this mode or any other — the PM never rewrites the model.
4. `publishDraft` exactly once.
