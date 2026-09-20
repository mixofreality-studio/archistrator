# Design-experience comment margin — design

Date: 2026-09-19
Status: proposed (founder-approved in brainstorm; not yet planned)
Scope: the review/comment surface shared by System Design (Phase 1), Project
Design (Phase 2), and the Construction Console.

## 1. Problem

The Mission page — and every page in the design experience that follows the same
pattern — carries five status surfaces above the fold before a word of content,
and scatters one user intent across three unrelated affordances. The founder's
five complaints reduce to two root causes.

**Root cause A — the rail wears a chat costume over a work queue.**
`ChatRail` renders posts as left/right bubbles with reply carets under a
`CO-AUTHOR / architect` header, which promises turn-taking. What it is: a ledger
of filed items that sit staged until a batch verb (Send back / Amend / Ask)
consumes them. The oddities follow from the costume — a `response` field that
looks like a reply but only ever fills after a batch; a `PENDING · NOT SENT`
group collapsed behind a "Review" disclosure; an anchor pill that names
`Mission Objective 3` but is a `Tooltip`, not a link.

**Root cause B — outbound actions are grouped by lifecycle stage, not intent.**
"I want this changed" lands in three places depending on stage: `GatePanel` at
the bottom of the content when drafted; `Amend` in the header strip when
committed; `Ask N questions` at the bottom of the rail when the item is a
question.

Founder complaints, mapped:

| # | Complaint | Cause | Section |
|---|---|---|---|
| 1 | `project.json → slots.mission · step 1 of 5` is not useful to humans | B | §5 |
| 2 | `COMMITTED · revision 2` / Amend banner eats vertical space | B | §5 |
| 3 | Chat rendering implies a conversation that does not exist | A | §3, §4 |
| 4 | Amend lives in the header, Ask lives at the rail's foot | B | §4 |
| 5 | A comment names its content but does not link back to it | A | §3 |

## 2. Decisions taken in the brainstorm

1. **Google-Docs margin comments**, not a queue list. The margin *is* the rail on
   every artifact, prose and canvas alike.
2. **Threads, not a single response.** The AI replies in-thread; on a change
   request it states what it changed.
3. **Everything queues.** No reply dispatches on its own. Staged replies ride the
   one batch verb.
4. **The reviewer closes threads.** The AI answering marks a thread `answered`;
   only the reviewer resolves. `waive` disappears as a separate verb — resolving
   an untouched thread is waiving it.
5. **Resolve and Reopen stay immediate** (they cost no AI run). Only content —
   new comments and replies — queues.

## 3. The comment model

### 3.1 Contract ownership

`cmd/modelgen` generates `contract.gen.go` from `.serviceContracts` in
`.aiarch/state/project.json`; the per-component `contract.*.schema.json` files
are retired seeds (`server/cmd/modelgen/main.go`). Five contract entries carry
the affected `$defs` and must be amended in lockstep:

| Contract | goPackage | Carries |
|---|---|---|
| `designSessionAccess` | `internal/resourceaccess/projectstate` | `ReviewComment` |
| `projectStateAccess` | `internal/resourceaccess/projectstate` | `ReviewComment` |
| `systemDesignManager` | `internal/manager/systemdesign` | `ReviewCommentView`, `AnchoredComment` |
| `projectDesignManager` | `internal/manager/projectdesign` | `ReviewCommentView`, `AnchoredComment` |
| `constructionManager` | `internal/manager/construction` | `AnchoredComment` |

The amendment goes through `recordServiceContract`, then `make gen-models`, which
regenerates the Go models, the manager view types, the systemtests SDK, and the
SPA's `schema.ts`. Missing any one of the five leaves a partially-migrated wire
contract that compiles on one side of a call and not the other.

### 3.2 Shape change

`response: string` becomes `replies: ReviewCommentReply[]`:

```
ReviewCommentReply {
  id          string   // stable per utterance
  authorRole  string   // "architect-user" | "architect" | "pm"
  text        string
  at          string   // RFC3339
}
```

`ReviewCommentView` (the two manager contracts) mirrors the same change:
`response: string` → `replies: ReviewCommentReply[]`.

`required` stays presence-only per the contract-strictness ruling; non-emptiness
belongs in GO, not `minLength`.

### 3.3 Status vocabulary — a drift correction

The committed design already says `answered` and `resolved` (see
`dynamicViews[var-send-back-redraft].close-comments`:
`setReviewCommentStatus(projectId, kind, commentId, resolved)`). The code
drifted to `addressed` / `waived` (`systemdesignmanager.go:700`). This work
brings the code back to the design:

| Today (code) | After | Set by |
|---|---|---|
| `open` | `open` | the reviewer, on submit |
| `addressed` | `answered` | the AI, via `respondToReviewComment` |
| `waived` | `resolved` | **the reviewer only** |

`SetReviewCommentStatus(rc, projectID, kind, commentID, status string)` keeps its
signature. Only the legal transitions change:

- `open → resolved` (resolve an unanswered thread; this is the old waive)
- `answered → resolved` (accept the AI's response)
- `answered → open` (**a queued reply landing on an answered thread reopens it**,
  applied by the manager at submit time — so the pushback rides the redraft and
  blocks approve until answered again)
- `resolved → open` (reopen)

The old `addressed → open` reopen becomes `resolved → open`. `answered` is no
longer terminal-ish: it is a waypoint the reviewer passes through.

`answered → open` is the only transition the *manager* applies on its own; every
other one is an explicit reviewer action.

### 3.4 Approve gate

Unchanged in behaviour, restated in the new vocabulary: **only `open` change
requests block approve.** `answered` and `resolved` do not. Open *questions* warn
but never block, exactly as today (`systemdesignmanager.go:727`).

**Approve bulk-resolves every `answered` thread on the slot**, so a reviewer who
accepts a redraft wholesale does not have to click Resolve eight times. A thread
the reviewer explicitly reopened stays open and blocks.

### 3.5 Read-compat — no data migration

Git-as-DB history is not rewritten. The read-model mapper shims legacy entries:

- `response` non-empty and `replies` empty → synthesize one reply authored by the
  drafting role, `at` = the slot's commit time.
- `status == "addressed"` → read as `answered`.
- `status == "waived"` → read as `resolved`.

Old commits keep rendering; nothing is backfilled.

### 3.6 Agent side

`respondToReviewComment(id, response)` keeps its name and signature, so the
existing agent prompt at `server/cmd/aiarch-state-mcp/tools.go:215` ("You MUST
respond to every OPEN comment before publishing") does not churn. Two changes:

- It **appends a reply** rather than setting a field, and flips `open → answered`.
- Its tool description gains: on a change request the response must state, in one
  line, **what was changed** — not merely that the comment was read.

A reviewer reply staged against an existing thread is delivered to the agent as
part of the thread it reads via `getReviewThread`, so no new agent tool is needed.

### 3.7 Queued replies

`PostedComment` (client, `CommentContext`) gains `replyTo?: string` — the id of
the server thread the utterance answers. Absent means a new thread. Staged
replies persist to the same `pendingCommentsStore` slot and ride the batch verb.
On submit, `AnchoredComment` carries a new optional `replyTo` so the manager
appends into the existing ledger entry instead of seeding a new one. That field is
the reason `systemDesignManager`, `projectDesignManager` and `constructionManager`
are in the lockstep list of §3.1 — an `AnchoredComment` without `replyTo` can only
ever open a new thread.

## 4. The margin

### 4.1 Mechanism

`ChatRail` is deleted as a concept; `CommentMargin` replaces it.

An `AnchorRegistry` context maps `jsonPath → HTMLElement`. Cards measure their
anchor's offset within the scroll container, sort by it, then stack downward from
their desired y with a minimum gap — the standard Docs collision pass.

- **Prose registers for free.** `CommentableList` already computes
  `getAnchor(item, index)`; one `useRegisterAnchor` call inside it lights up every
  itemized artifact at once — glossary terms, required behaviors, business
  objectives, operational-concept decisions, activities, solution knobs.
- **Canvas** registers React Flow node elements. Re-measure rAF-throttled on pan
  and zoom.
- **Unanchored** threads pin to the top of the margin under an "unplaced" heading.

### 4.2 Behaviour

- Hover a card → highlight the anchored row/node and draw a leader line.
- Click a card → scroll the row into view; for a canvas, center the node.
  (This is complaint #5, and it works in both directions.)
- The active thread expands; the rest collapse to two lines.
- Resolved threads collapse into a `Resolved (N)` disclosure, reopenable.
- Each thread card carries: anchor reference, type badge (change request /
  question → addressee), status, the utterances, a reply box, and `Resolve`.

### 4.3 Responsive

Margin is a fixed ~300px column. Below ~1100px viewport width it collapses to a
toggleable overlay drawer; `ExperienceChrome`'s existing `onOpenChat` affordance
is repurposed to open it.

## 5. Chrome cleanup

- **Delete** `{meta.stateAddress} · step {n} of {m}`
  (`SystemDesignView.tsx:332`, `ProjectDesignExperience.tsx:509`). Step position
  is already carried by the `SlimSpine` directly above it. The slot path moves
  into the existing `(?)` `ArtifactInfoButton` popover, so it stays available for
  debugging at zero layout cost.
- **Delete** the `COMMITTED · revision N` `Paper` strip in
  `CommittedArtifactPanel`. It becomes a chip beside the title — `committed · r2`
  — whose tooltip carries the provenance line, which also absorbs the separate
  `provenanceSummary` caption beneath it. `StageChip` already occupies that slot
  in every other stage, so it is one chip in all states.
- **Delete** the header `Amend` button (moves to the submit bar, §6).

Net: roughly 110px of vertical chrome recovered above the fold.

## 6. One submit bar

A sticky bar across the bottom of the content+margin container. One primary verb,
resolved from `(stage, staged types)`:

| Stage | Staged | Open threads | Verb |
|---|---|---|---|
| drafted / awaitingReview | any change requests | — | `Send back (N)` |
| drafted / awaitingReview | nothing | none | `Approve` |
| drafted / awaitingReview | nothing | some `open` | `Approve` **disabled**, labelled `Resolve N threads to approve` |
| committed | any | — | `Amend (N)` |
| committed | nothing | — | no primary verb; overflow menu only |
| any | questions only | — | `Ask (N) — no redraft` |

A consequence line always sits beneath the verb: *"2 change requests → redraft ·
1 question → PM"*. Secondary verbs (Withdraw, Retry) move to an overflow menu.

This deletes the header `Amend` button, the rail's `Ask` button, and `GatePanel`'s
action row — complaint #4. `GatePanel` keeps its findings and diagnostics inline
in the content column; **only its buttons move.**

No Manager contract change: the bar continues to call the existing distinct
entries (`AskQuestions`, `SubmitReviewDecision`, `RequestArtifactDraft`). The
unification is in the UI, not the contract.

## 7. Design-state amendments

These UI/contract changes make three committed artifacts in
`.aiarch/state/project.json` stale. They must be amended, not left to drift.

### 7.1 `coreUseCases` — `drive-system-design` (slot 4, decision 0, **core**)

| Node | Change |
|---|---|
| `human-gate` | relabel → "Architect reviews the artifact; stages comment threads and replies in the margin" |
| *new* `resolve-threads` | action — "Resolve or reopen threads (no dispatch)"; edge `human-gate → resolve-threads → human-gate` |
| `decision` | relabel → "Submit routes by staged content?" |
| `ask-questions` | relabel → "Dispatch an answer job; the addressee appends a reply to each question thread (no redraft)" |
| `weave-feedback` | relabel → "Incorporate change requests into the next draft; the drafting agent appends a reply stating what it changed; repeat the critique round" |
| `approve-gate` | relabel → "Open change-request threads remain?"; add that approve bulk-resolves `answered` threads |

Guards on `decision` become `[questions only]`, `[any change request]`,
`[nothing staged: approve]`, `[withdraw]`.

### 7.2 `coreUseCases` — `ask-a-clarifying-question-during-review` (decision 14)

| Node | Change |
|---|---|
| `write-questions` | relabel → "Reviewer opens anchored question threads addressed to the PM or the architect; they stage, they do not dispatch" |
| *new* `submit-batch` | action — "Reviewer submits the staged batch"; sits between `write-questions` and `artifact-state` |
| `record-answers` | relabel → "Addressee appends a reply utterance; the thread becomes answered, not closed" |
| *new* `reviewer-closes` | decision — "Reviewer satisfied?" → `[yes]` resolve thread → `approve-unblocked`; `[no]` stage a reply → `submit-batch` |
| `approve-unblocked` | unchanged in meaning |

### 7.3 `coreUseCases` — `send-back-change-requests-for-a-redraft` (decision 15)

| Node | Change |
|---|---|
| `anchor-comments` | relabel → "Reviewer anchors change-request threads, and replies to existing threads, in the margin; all stage without dispatching" |
| `respond-per-comment` | relabel → "The drafting agent appends a reply stating what it changed" |
| `close-comments` | relabel → "Reviewer resolves each thread; approve bulk-resolves any still answered" |

The `resolved` decision node already carries `decidedBy: architect-user` — correct
as-is, and the reason the reviewer-closes rule needs no new doctrine.

### 7.4 `systemDesign` dynamic views (slot 5)

`uc1-drive-system-design`, `var-ask-review-question`, and `var-send-back-redraft`
carry a `steps[]` entry per `activityNodeId`. Every node added or renamed above
needs its matching step added or its call labels updated. The call *topology* does
not change — same components, same Manager entries — so this is label work plus
three new steps, not a decomposition change.

Notably `var-send-back-redraft.close-comments` already reads
`setReviewCommentStatus(..., resolved)`, so §3.3 brings the code **to** the
design rather than changing it.

### 7.5 Open question — the Amend use case

No committed use case covers "amend a committed artifact" as a first-class flow;
`reconcile-stale` in `drive-system-design` covers only the stale-basis path. The
submit bar makes `Amend (N)` a primary verb, which argues for a use case. Flagged
for the founder; **not** invented here.

## 8. Rollout

Three `ChatRail` consumers: `SystemDesignContainer` (Phase 1),
`ProjectDesignExperience` (Phase 2), `ConstructionConsole`. `ExperienceChrome`
takes `margin` instead of `chat`.

Order:

1. Contract + status drift correction + read-compat shim, with Go tests.
2. `CommentMargin` against Mission only — prose, no canvas math. **Stop for
   founder review in the running app.**
3. Chrome cleanup (§5) and the submit bar (§6) on the same page. **Stop for
   founder review.**
4. Canvas anchor registry (Architecture, Volatilities, Core Use Cases).
5. Sweep Phase 2 and the Construction Console.
6. Design-state amendments (§7).

## 9. Risks

**Canvas card positioning under pan/zoom** is the main build risk. Mitigated by
the prose-first order: if it proves bad, canvas surfaces fall back to a
select-on-canvas thread list without rearchitecting the margin.

**Amending two service contracts in project.json** may trip
`ACT-COMPONENT-COVERAGE` or review routing, depending on how strictly the design
rail polices contract edits outside a detailed-design activity. To be established
before the contract is touched, and reported rather than routed around.

**Amending the `coreUseCases` slot** is itself a design-rail operation. Whether it
runs as a dogfooded Amend through the app or as a direct `putDraftModel` via the
system-architect agent is a founder call (see §7.5 and the rollout note).

## 10. Testing

- Go: status transition table (legal and illegal), approve-gate predicate over
  `open`/`answered`/`resolved`, bulk-resolve on approve, read-compat shim over a
  legacy `response`/`waived` fixture.
- Vitest: margin stacking and collision pass, `replyTo` staging and persistence,
  submit-verb resolution table from `(stage, staged types)`.
- Playwright (`uitests`): comment on Mission Objective 3 → card appears beside
  that row → click card scrolls to the row → reply stages → submit shows the
  right verb and consequence line.
- Run against real local state per the founder's UI review loop; stop for review
  per UI change.
