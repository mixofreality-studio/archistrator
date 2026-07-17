# /mission-draft

> Draft (or amend) the **Mission** artifact — vision, business objectives, mission statement — as one design-rail CI job. The job's ambient env fixes the artifact kind and target slot.

**Arguments** — none. Kind, job mode, branch, and project come from the ambient `AIARCH_*` env baked into this CI run.

**Agent + skills.** **Adopt the `system-architect` role**: read `.claude/agents/system-architect.md` and act per that charter yourself, in this session. Do NOT dispatch a subagent or use an Agent/Task tool — the CI job runs single-session; you ARE the system-architect. Follow **[[the-method-business-alignment]]** (including its "Draft-job doctrine" section — that is your task statement) and **[[the-method-project-state]]** for the tool flow.

## Steps

> **State changes go through the `aiarch-state` MCP tools, not hand-edits.** Never hand-edit `.aiarch/state/project.json`; never run `git` for state.

1. **Read your inputs.** `listResearchSources`/`getResearchSource` for the research corpus; `getCommittedSlot` for every committed predecessor slot this artifact builds on; on an amendment, `getDraftSlot` for the current draft.
2. **Read the review ledger** with `getReviewThread`. If open comments exist, this is a redraft: your draft MUST address every open comment. Also read `getCritique` — if it carries a `revise` verdict, its notes are the PM's revision guidance and your draft MUST address them. Also read the current draft slot's `notes` field (`getDraftSlot`) — founder rejection/send-back feedback lands THERE, not in the review thread; every point in it MUST be addressed (answer each via `respondToReviewComment` where a matching comment exists, otherwise in your `publishDraft` message).
3. **Draft** the typed model per [[the-method-business-alignment]]. Submit with `putDraftModel` — it validates and returns actionable errors; fix and resubmit until accepted.
4. **Carry forward excluded constraints.** Founder-stated technical/deployment/operational constraints (e.g. "will be operated by archistrator") are correctly excluded from the vision, objectives, and mission — but must not be silently dropped. Enumerate each one explicitly, verbatim where possible, per the skill's constraints carry-forward clause: name them in your review-thread responses where a relevant comment exists, and always in your `publishDraft` message (the typed model has no notes field — the co-author thread is the carry-forward channel), so requirements analysis and volatility identification receive them.
5. **Respond to every open ledger comment** with `respondToReviewComment` — accept (say what you changed) or rebut (say why not, grounded in the Method). Silent non-response is a defect.
6. **Finish** with `publishDraft` (exactly once). Do not open PRs, do not merge, do not touch phase status — the server owns the loop. If dispatched as a redraft and you find no open review comments, no revise verdict, AND no unaddressed rejection notes on the draft slot, do not exit without publishing: re-validate the existing draft and call `publishDraft` anyway to re-affirm it. A draft job must NEVER end without calling `publishDraft` — an empty exit fails the run's silent-failure guard.
