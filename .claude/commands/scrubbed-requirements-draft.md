# /scrubbed-requirements-draft

> Draft (or amend) the **Scrubbed Requirements** artifact as one design-rail CI job. The job's ambient env fixes the artifact kind and target slot.

**Arguments** — none. Kind, job mode, branch, and project come from the ambient `AIARCH_*` env baked into this CI run.

**Agent + skills.** Work as the **`system-architect`** agent (`.claude/agents/system-architect.md`). Follow **[[the-method-requirements-analysis]]** (including its "Draft-job doctrine" section — that is your task statement) and **[[the-method-project-state]]** for the tool flow.

## Steps

> **State changes go through the `aiarch-state` MCP tools, not hand-edits.** Never hand-edit `.aiarch/state/project.json`; never run `git` for state.

1. **Read your inputs.** `getCommittedSlot` for every committed predecessor slot this artifact builds on; on an amendment, `getDraftSlot` for the current draft (basis: `.mission`, `.glossary`).
2. **Read the review ledger** with `getReviewThread`. If open comments exist, this is a redraft: your draft MUST address every open comment. Also read `getCritique` — if it carries a `revise` verdict, its notes are the PM's revision guidance and your draft MUST address them.
3. **Draft** the typed model per [[the-method-requirements-analysis]]. Submit with `putDraftModel` — it validates and returns actionable errors; fix and resubmit until accepted.
4. **Respond to every open ledger comment** with `respondToReviewComment` — accept (say what you changed) or rebut (say why not, grounded in the Method). Silent non-response is a defect.
5. **Finish** with `publishDraft` (exactly once). Do not open PRs, do not merge, do not touch phase status — the server owns the loop.
