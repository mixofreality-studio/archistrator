# Comment Margin — Stage 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the chat-costumed review rail on the System Design Mission page
with Google-Docs-style margin comment threads, backed by a real utterance-thread
contract, and collapse three stage-dependent action affordances into one submit bar.

**Architecture:** The `ReviewComment` wire type gains `replies[]` (an utterance
list) and `reopened` (a sticky reviewer bit) in place of the single `response`
string; status vocabulary moves from `open/addressed/waived` to
`open/answered/resolved`, which is a drift correction toward what the committed
design already says. Status stays *derived* on the server (`normalizeReviewThread`)
— the rule becomes "answered iff the last reply is agent-authored" — so a queued
reviewer reply reopens a thread for free. On the client, `ChatRail` is replaced by
`CommentMargin`: an `AnchorRegistry` maps a comment's JSONPath to the DOM element
it anchors to, and a pure geometry module stacks cards beside their anchors.

**Tech Stack:** Go 1.26 (server, `GOWORK=off`), Temporal, React 19 + MUI + Vite
(webApp), `node --test` for SPA unit tests, Playwright (uitests).

**Spec:** `docs/superpowers/specs/2026-09-19-design-comment-margin-design.md`

## Scope

This plan covers spec rollout steps 1–3: contract, server, and the **prose**
margin on Mission, plus the chrome cleanup and submit bar. It ends at a founder
review in the running app.

Explicitly **out of scope**, deferred to a Stage 2 plan written after that review:
canvas anchor registration (Architecture / Volatilities / Core Use Cases), the
Phase-2 and Construction Console sweep, and the §7 design-state amendments.

## Global Constraints

- **Build and test Go with `GOWORK=off`.** `go.work` points at stale sibling
  platform checkouts; the app builds against published module versions.
- **Never hand-edit `.aiarch/state/project.json`.** State changes go through the
  `aiarch-state` MCP write tools (`recordServiceContract`). Hand-edits break the
  git-as-DB invariants.
- **`required` in a contract is presence-only.** Non-emptiness belongs in GO
  (guard clauses), never `minLength`.
- Server tests: `cd server && GOWORK=off go test ./...`
- SPA unit tests: `cd webApp && npm test` — this is `node --test` over
  `src/**/*.test.ts`. **It cannot import `.tsx`.** Any logic that needs a unit
  test must live in a `.ts` module; the `.tsx` is a thin renderer over it.
- SPA typecheck: `cd webApp && npm run typecheck`
- Playwright: `cd uitests && npx playwright test <spec>`
- Do not weaken an existing gate to make a test pass.
- `webApp` has ~71 files of pre-existing prettier drift on main; `npm run
  format:check` failing on files you did not touch is expected and not yours to fix.

---

### Task 1: Contract — utterance threads

**Files:**
- Modify (via MCP `recordServiceContract`, never by hand): `.aiarch/state/project.json`
  `.serviceContracts.{designSessionAccess, projectStateAccess, systemDesignManager,
  projectDesignManager, constructionManager}`
- Regenerated (do not edit): `server/internal/resourceaccess/projectstate/contract.gen.go`,
  `server/internal/manager/systemdesign/contract.gen.go`,
  `server/internal/manager/projectdesign/contract.gen.go`,
  `server/internal/manager/construction/contract.gen.go`,
  `systemtests/internal/sdk/types_shared.gen.go`

**Interfaces:**
- Produces: Go types `projectstate.ReviewCommentReply{ID, AuthorRole, Text, At string}`,
  `projectstate.ReviewComment.Replies []ReviewCommentReply`,
  `projectstate.ReviewComment.Reopened bool`,
  `systemdesign.ReviewCommentView.Replies []ReviewCommentReply`,
  `systemdesign.AnchoredComment.ReplyTo string`. Every later task consumes these.

- [ ] **Step 1: Record the new `ReviewCommentReply` def on the four thread-carrying contracts**

Add this `$defs` entry to `designSessionAccess`, `projectStateAccess`,
`systemDesignManager`, and `projectDesignManager`:

```json
{
  "ReviewCommentReply": {
    "type": "object",
    "properties": {
      "id": { "type": "string", "x-go-name": "ID" },
      "authorRole": { "type": "string" },
      "text": { "type": "string" },
      "at": { "type": "string" }
    },
    "required": ["id", "authorRole", "text", "at"],
    "additionalProperties": false
  }
}
```

- [ ] **Step 2: Replace `response` with `replies` + `reopened` on `ReviewComment` / `ReviewCommentView`**

On `designSessionAccess.$defs.ReviewComment` and
`projectStateAccess.$defs.ReviewComment` (and the identically-shaped
`ReviewCommentView` on the two manager contracts): delete the `response` property
and its `required` entry, then add:

```json
{
  "replies": { "type": "array", "items": { "$ref": "#/$defs/ReviewCommentReply" } },
  "reopened": { "type": "boolean" }
}
```

and add `"replies"` and `"reopened"` to `required`.

`reopened` is the sticky reviewer bit: it keeps a thread `open` even when the last
reply is agent-authored, and the agent clears it when it replies again (Task 3).

- [ ] **Step 3: Add `replyTo` to `AnchoredComment` on all three manager contracts**

On `systemDesignManager`, `projectDesignManager`, and `constructionManager`, add to
`$defs.AnchoredComment.properties`:

```json
{ "replyTo": { "type": "string" } }
```

and add `"replyTo"` to its `required` list (presence-only; empty string means
"open a new thread").

- [ ] **Step 4: Regenerate and confirm the diff is exactly the five contracts**

```bash
cd server && make gen-models
git -C .. status --porcelain
```

Expected: `.aiarch/state/project.json` plus the four `contract.gen.go` files and
`systemtests/internal/sdk/types_shared.gen.go` modified. Nothing else.

- [ ] **Step 5: Confirm the build breaks where it should**

```bash
cd server && GOWORK=off go build ./... 2>&1 | head -40
```

Expected: FAIL, with `c.Response undefined` at `systemdesignmanager.go:4463`,
`projectstateaccess.go:9225`, `:9253`, `:9280`, and `state.go` in
`cmd/aiarch-state-mcp`. These are exactly the call sites Tasks 2–4 rewrite. A
build that *passes* here means the contract edit did not land — go back to Step 1.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "contract: review comments become utterance threads

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 2: ResourceAccess — status vocabulary and the derive rule

**Files:**
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go:9100-9300`
- Test: `server/internal/resourceaccess/projectstate/access_test.go`

**Interfaces:**
- Consumes: `ReviewComment.Replies`, `.Reopened` from Task 1.
- Produces: consts `ReviewCommentAnswered = "answered"`, `ReviewCommentResolved = "resolved"`;
  `normalizeReviewThread([]ReviewComment) []ReviewComment`;
  `applyReviewCommentStatus([]ReviewComment, id, status string) ([]ReviewComment, error)`;
  `appendReviewReply(thread []ReviewComment, id, authorRole, text, at string) ([]ReviewComment, error)`.

- [ ] **Step 1: Write the failing tests**

Add to `access_test.go`:

```go
func TestNormalizeReviewThreadDerivesFromLastReply(t *testing.T) {
	agent := ReviewCommentReply{ID: "r1", AuthorRole: "architect", Text: "Split it.", At: "2026-09-19T00:00:00Z"}
	human := ReviewCommentReply{ID: "r2", AuthorRole: "architect-user", Text: "Still wrong.", At: "2026-09-19T01:00:00Z"}

	cases := []struct {
		name string
		in   ReviewComment
		want string
	}{
		{"no replies is open", ReviewComment{ID: "c1", Status: ReviewCommentOpen}, ReviewCommentOpen},
		{"agent reply last is answered", ReviewComment{ID: "c2", Replies: []ReviewCommentReply{agent}}, ReviewCommentAnswered},
		{"reviewer reply last reopens", ReviewComment{ID: "c3", Replies: []ReviewCommentReply{agent, human}}, ReviewCommentOpen},
		{"reopened bit beats an agent reply", ReviewComment{ID: "c4", Replies: []ReviewCommentReply{agent}, Reopened: true}, ReviewCommentOpen},
		{"resolved is sticky", ReviewComment{ID: "c5", Status: ReviewCommentResolved}, ReviewCommentResolved},
		{"staleAck is sticky", ReviewComment{ID: "c6", Type: ReviewCommentTypeStaleAck, Status: ReviewCommentAnswered}, ReviewCommentAnswered},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeReviewThread([]ReviewComment{tc.in})
			if got[0].Status != tc.want {
				t.Fatalf("status = %q, want %q", got[0].Status, tc.want)
			}
		})
	}
}

func TestApplyReviewCommentStatusTransitions(t *testing.T) {
	agent := ReviewCommentReply{ID: "r1", AuthorRole: "architect", Text: "done", At: "2026-09-19T00:00:00Z"}
	legal := []struct{ from, to string }{
		{ReviewCommentOpen, ReviewCommentResolved},
		{ReviewCommentAnswered, ReviewCommentResolved},
		{ReviewCommentResolved, ReviewCommentOpen},
	}
	for _, tc := range legal {
		thread := []ReviewComment{{ID: "c1", Status: tc.from, Replies: []ReviewCommentReply{agent}}}
		got, err := applyReviewCommentStatus(thread, "c1", tc.to)
		if err != nil {
			t.Fatalf("%s->%s: unexpected error %v", tc.from, tc.to, err)
		}
		if got[0].Status != tc.to {
			t.Fatalf("%s->%s: status = %q", tc.from, tc.to, got[0].Status)
		}
	}

	// A reopen sets the sticky bit and KEEPS the reply history.
	thread := []ReviewComment{{ID: "c1", Status: ReviewCommentResolved, Replies: []ReviewCommentReply{agent}}}
	got, err := applyReviewCommentStatus(thread, "c1", ReviewCommentOpen)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if !got[0].Reopened {
		t.Fatal("reopen must set the sticky Reopened bit")
	}
	if len(got[0].Replies) != 1 {
		t.Fatalf("reopen must preserve replies, got %d", len(got[0].Replies))
	}

	// Illegal: answered -> open is the manager's derived business, not a human verb.
	if _, err := applyReviewCommentStatus(
		[]ReviewComment{{ID: "c1", Status: ReviewCommentAnswered}}, "c1", ReviewCommentAnswered,
	); err == nil {
		t.Fatal("expected an error for a no-op transition")
	}
}

func TestNormalizeReviewThreadReadCompat(t *testing.T) {
	legacy := []ReviewComment{
		{ID: "c1", Status: "addressed", Response: "Reworded it."},
		{ID: "c2", Status: "waived"},
	}
	got := normalizeReviewThread(migrateLegacyReviewThread(legacy, "architect", "2026-01-01T00:00:00Z"))
	if got[0].Status != ReviewCommentAnswered {
		t.Fatalf("legacy addressed should read as answered, got %q", got[0].Status)
	}
	if len(got[0].Replies) != 1 || got[0].Replies[0].Text != "Reworded it." {
		t.Fatalf("legacy response should synthesize one reply, got %+v", got[0].Replies)
	}
	if got[1].Status != ReviewCommentResolved {
		t.Fatalf("legacy waived should read as resolved, got %q", got[1].Status)
	}
}
```

Note: `TestNormalizeReviewThreadReadCompat` references a legacy `Response` field
that Task 1 deleted from the contract. Keep a package-private
`legacyResponse string` with a `json:"response"` tag on a small
`legacyReviewComment` struct used only by the shim (Step 3) — do **not** re-add
`Response` to the generated type.

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ -run 'TestNormalizeReviewThread|TestApplyReviewCommentStatus' -v
```

Expected: FAIL — `undefined: ReviewCommentAnswered`, `undefined: migrateLegacyReviewThread`.

- [ ] **Step 3: Implement**

At `projectstateaccess.go:9100-9130`, keep `ReviewCommentOpen` and add:

```go
	// ReviewCommentAnswered — the drafting agent has appended a reply. A waypoint,
	// not a close: only the reviewer resolves (design §3.3).
	ReviewCommentAnswered = "answered"
	// ReviewCommentResolved — the reviewer closed the thread. Sticky; normalization
	// never reconsiders it. Subsumes the retired "waived" (resolving an unanswered
	// thread IS dismissing it).
	ReviewCommentResolved = "resolved"
```

Keep `ReviewCommentAddressed` and `ReviewCommentWaived` as consts, marked
deprecated, used ONLY by the read-compat shim.

Replace `normalizeReviewThread`:

```go
// normalizeReviewThread derives every non-sticky entry's Status from its reply
// history: the thread is ANSWERED iff its last utterance is agent-authored, and
// OPEN otherwise (no replies at all, or the reviewer got the last word). That one
// rule also implements the answered->open reopen for free — a queued reviewer
// reply landing on an answered thread makes the last utterance human, so the
// thread re-opens and blocks approve until the agent answers again.
//
// Sticky and never reconsidered: RESOLVED (a reviewer decision), staleAck entries
// (audit records), and the Reopened bit (an explicit reviewer reopen, which the
// agent clears when it replies — see respondToReviewComment).
func normalizeReviewThread(thread []ReviewComment) []ReviewComment {
	for i := range thread {
		c := &thread[i]
		if c.Status == ReviewCommentResolved || c.Type == ReviewCommentTypeStaleAck {
			continue
		}
		if c.Reopened || len(c.Replies) == 0 || isReviewerRole(c.Replies[len(c.Replies)-1].AuthorRole) {
			c.Status = ReviewCommentOpen
			continue
		}
		c.Status = ReviewCommentAnswered
	}
	return thread
}

// isReviewerRole reports whether an utterance came from the human reviewer rather
// than an agent. Anything that is not a known agent role is treated as the
// reviewer, so an unrecognised author can never silently satisfy a change request.
func isReviewerRole(role string) bool {
	switch role {
	case "architect", "pm":
		return false
	default:
		return true
	}
}
```

Replace `applyReviewCommentStatus`:

```go
// applyReviewCommentStatus applies a REVIEWER status transition. Legal:
// open->resolved and answered->resolved (close), and resolved->open (reopen).
// A reopen sets the sticky Reopened bit and PRESERVES the reply history — the
// thread is the record of the conversation, so reopening must not erase it.
func applyReviewCommentStatus(thread []ReviewComment, id, status string) ([]ReviewComment, error) {
	for i := range thread {
		if thread[i].ID != id {
			continue
		}
		from := thread[i].Status
		switch {
		case (from == ReviewCommentOpen || from == ReviewCommentAnswered) && status == ReviewCommentResolved:
			thread[i].Status = ReviewCommentResolved
			thread[i].Reopened = false
		case from == ReviewCommentResolved && status == ReviewCommentOpen:
			thread[i].Status = ReviewCommentOpen
			thread[i].Reopened = true
		default:
			return nil, fwra.New(fwra.ContractMisuse, fmt.Sprintf(
				"projectstate.SetReviewCommentStatus: illegal transition %q -> %q for comment %s (allowed: open->resolved, answered->resolved, resolved->open)", from, status, id))
		}
		return thread, nil
	}
	return nil, fwra.New(fwra.NotFound, fmt.Sprintf("projectstate.SetReviewCommentStatus: comment %s not found in review thread", id))
}

// validReviewCommentStatus reports whether s is one of the closed wire values.
func validReviewCommentStatus(s string) bool {
	switch s {
	case ReviewCommentOpen, ReviewCommentAnswered, ReviewCommentResolved:
		return true
	default:
		return false
	}
}
```

Add `appendReviewReply` (used by Task 3 and Task 4):

```go
// appendReviewReply appends one utterance to the thread entry with id. Appending an
// AGENT reply clears the sticky Reopened bit, so an explicitly reopened thread
// settles back to answered once the agent has actually answered again.
func appendReviewReply(thread []ReviewComment, id, authorRole, text, at string) ([]ReviewComment, error) {
	for i := range thread {
		if thread[i].ID != id {
			continue
		}
		thread[i].Replies = append(thread[i].Replies, ReviewCommentReply{
			ID:         fmt.Sprintf("%s-u%d", id, len(thread[i].Replies)+1),
			AuthorRole: authorRole,
			Text:       text,
			At:         at,
		})
		if !isReviewerRole(authorRole) {
			thread[i].Reopened = false
		}
		return thread, nil
	}
	return nil, fwra.New(fwra.NotFound, fmt.Sprintf("projectstate.appendReviewReply: comment %s not found in review thread", id))
}
```

Add the read-compat shim:

```go
// legacyReviewComment carries ONLY the retired fields, so a pre-thread ledger
// committed to git still renders. Nothing writes this shape.
type legacyReviewComment struct {
	Response string `json:"response"`
	Status   string `json:"status"`
}

// migrateLegacyReviewThread render-on-reads a pre-thread ledger: a non-empty
// legacy response becomes one synthesized agent reply, and the retired statuses
// map onto the new vocabulary. Nothing is written back — git history stays as
// committed (design §3.5).
func migrateLegacyReviewThread(thread []ReviewComment, draftedBy, at string) []ReviewComment {
	for i := range thread {
		c := &thread[i]
		if c.Response != "" && len(c.Replies) == 0 {
			role := draftedBy
			if role == "" {
				role = "architect"
			}
			c.Replies = []ReviewCommentReply{{ID: c.ID + "-u1", AuthorRole: role, Text: c.Response, At: at}}
		}
		switch c.Status {
		case ReviewCommentAddressed:
			c.Status = ReviewCommentAnswered
		case ReviewCommentWaived:
			c.Status = ReviewCommentResolved
		}
	}
	return thread
}
```

Finally, update `appendReviewComments` (`:9207`): drop `Response: ""` and set
`Replies: nil, Reopened: false` instead.

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ -run 'TestNormalizeReviewThread|TestApplyReviewCommentStatus' -v
```

Expected: PASS, all subtests.

- [ ] **Step 5: Run the whole RA package**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/
```

Expected: PASS. Any failure here is an existing test asserting the old vocabulary
— update the assertion to the new status names; do not re-introduce `addressed`
or `waived` as live values.

- [ ] **Step 6: Commit**

```bash
git add server/internal/resourceaccess/projectstate/
git commit -m "feat(review): derive thread status from reply history

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 3: Agent MCP tool — respond by appending

**Files:**
- Modify: `server/cmd/aiarch-state-mcp/state.go:136-175`
- Modify: `server/cmd/aiarch-state-mcp/tools.go:210-260`
- Test: `server/cmd/aiarch-state-mcp/state_test.go:74-100`

**Interfaces:**
- Consumes: `projectstate.appendReviewReply` is RA-private, so this binary reuses
  the same append logic locally against `slot.ReviewThread`.
- Produces: `(*Session).respondToReviewComment(id, response string) error` — same
  signature as today; the behaviour changes from set-field to append-utterance.

- [ ] **Step 1: Write the failing test**

Replace `TestRespondToReviewComment` in `state_test.go`:

```go
// TestRespondToReviewComment appends an agent utterance and clears a reopen.
func TestRespondToReviewComment(t *testing.T) {
	s := newTestSession(t)
	seedThread(t, s, projectstate.ReviewComment{ID: "r1c1", Status: projectstate.ReviewCommentOpen, Reopened: true})

	if err := s.respondToReviewComment("r1c1", "Reworded the rationale."); err != nil {
		t.Fatalf("respond: %v", err)
	}

	got := readThread(t, s)
	if len(got[0].Replies) != 1 {
		t.Fatalf("want 1 reply, got %d", len(got[0].Replies))
	}
	if got[0].Replies[0].Text != "Reworded the rationale." {
		t.Fatalf("reply text = %q", got[0].Replies[0].Text)
	}
	if got[0].Replies[0].AuthorRole == "" {
		t.Fatal("reply must carry an author role")
	}
	if got[0].Reopened {
		t.Fatal("an agent reply must clear the sticky Reopened bit")
	}

	// A second response appends rather than overwriting — this is a thread.
	if err := s.respondToReviewComment("r1c1", "Also split objective 3."); err != nil {
		t.Fatalf("second respond: %v", err)
	}
	if got := readThread(t, s); len(got[0].Replies) != 2 {
		t.Fatalf("want 2 replies after a second response, got %d", len(got[0].Replies))
	}

	if err := s.respondToReviewComment("nope", "x"); err == nil {
		t.Fatal("expected an error for an unknown comment id")
	}
}
```

Reuse whatever session/seed helpers the existing `state_test.go` already has; if
`seedThread` / `readThread` do not exist, write them as three-line helpers over
`s.readProject()` and `slotFor`.

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd server && GOWORK=off go test ./cmd/aiarch-state-mcp/ -run TestRespondToReviewComment -v
```

Expected: FAIL — `slot.ReviewThread[i].Response undefined`.

- [ ] **Step 3: Implement the append**

In `state.go`, replace the mutation body:

```go
	for i := range slot.ReviewThread {
		if slot.ReviewThread[i].ID != id {
			continue
		}
		if strings.TrimSpace(response) == "" {
			return fmt.Errorf("respondToReviewComment requires a non-empty response for comment %s", id)
		}
		c := &slot.ReviewThread[i]
		c.Replies = append(c.Replies, projectstate.ReviewCommentReply{
			ID:         fmt.Sprintf("%s-u%d", c.ID, len(c.Replies)+1),
			AuthorRole: s.Role,
			Text:       response,
			At:         time.Now().UTC().Format(time.RFC3339),
		})
		// An agent answer settles an explicit reviewer reopen. The server's
		// normalizeReviewThread stays authoritative on read-back; this only
		// proposes.
		c.Reopened = false
		c.Status = projectstate.ReviewCommentAnswered
		found = true
		break
	}
```

If `Session` has no `Role` field, use the session's drafting role however
`session.go` already exposes it; fall back to the literal `"architect"` only if
there is genuinely no role on the session.

- [ ] **Step 4: Update the tool description to demand a change summary**

In `tools.go`, the `respondToReviewComment` tool `Description` becomes:

```
Append your reply to one open review-comment thread. For a CHANGE REQUEST, state in one line WHAT YOU CHANGED in the draft — not that you read the comment, and not what you intend to do. For a QUESTION, answer it directly. Your reply is appended to the thread the reviewer reads; it never replaces an earlier utterance.
```

And at `tools.go:215`, extend the existing instruction:

```
You MUST respond to every OPEN comment before publishing — use respondToReviewComment. On a change request, your response must say what you changed.
```

- [ ] **Step 5: Run the tests to verify they pass**

```bash
cd server && GOWORK=off go test ./cmd/aiarch-state-mcp/
```

Expected: PASS, including `rig_test.go` and `promptsurface_test.go`. If
`promptsurface_test.go` asserts the old prompt text verbatim, update the
expectation to the new wording.

- [ ] **Step 6: Commit**

```bash
git add server/cmd/aiarch-state-mcp/
git commit -m "feat(review): agent responses append to the thread

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 4: Manager — transitions, approve gate, bulk-resolve, replyTo

**Files:**
- Modify: `server/internal/manager/systemdesign/systemdesignmanager.go:676-760`, `:4441-4470`
- Modify: `server/internal/manager/systemdesign/coauthorartifact.go:3180-3350`
- Test: `server/internal/manager/systemdesign/manager_test.go`

**Interfaces:**
- Consumes: `projectstate.ReviewCommentAnswered`, `.ReviewCommentResolved`,
  `appendReviewReply` semantics from Task 2.
- Produces: `SetReviewCommentStatus` accepting only `"resolved"` and `"open"`;
  `openReviewCommentViewIDs` unchanged in name and meaning;
  `bulkResolveAnswered(thread []ReviewCommentView) []string`.

- [ ] **Step 1: Write the failing tests**

```go
func TestCheckCommentTransitionNewVocabulary(t *testing.T) {
	thread := []ReviewCommentView{
		{ID: "c1", Status: projectstate.ReviewCommentOpen},
		{ID: "c2", Status: projectstate.ReviewCommentAnswered},
		{ID: "c3", Status: projectstate.ReviewCommentResolved},
	}
	legal := []struct{ id, to string }{
		{"c1", projectstate.ReviewCommentResolved},
		{"c2", projectstate.ReviewCommentResolved},
		{"c3", projectstate.ReviewCommentOpen},
	}
	for _, tc := range legal {
		if err := checkCommentTransition(thread, tc.id, tc.to); err != nil {
			t.Fatalf("%s -> %s should be legal: %v", tc.id, tc.to, err)
		}
	}
	if err := checkCommentTransition(thread, "c1", projectstate.ReviewCommentAnswered); err == nil {
		t.Fatal("a human may not set answered — that is the agent's derived status")
	}
}

func TestOpenChangeRequestsBlockApproveAnsweredDoesNot(t *testing.T) {
	thread := []ReviewCommentView{
		{ID: "c1", Status: projectstate.ReviewCommentOpen},
		{ID: "c2", Status: projectstate.ReviewCommentAnswered},
		{ID: "c3", Status: projectstate.ReviewCommentResolved},
		{ID: "c4", Status: projectstate.ReviewCommentOpen, Type: projectstate.ReviewCommentTypeQuestion},
	}
	got := openReviewCommentViewIDs(thread)
	if len(got) != 1 || got[0] != "c1" {
		t.Fatalf("only open change requests block approve, got %v", got)
	}
}

func TestBulkResolveAnsweredOnApprove(t *testing.T) {
	thread := []ReviewCommentView{
		{ID: "c1", Status: projectstate.ReviewCommentAnswered},
		{ID: "c2", Status: projectstate.ReviewCommentAnswered, Type: projectstate.ReviewCommentTypeQuestion},
		{ID: "c3", Status: projectstate.ReviewCommentResolved},
		{ID: "c4", Status: projectstate.ReviewCommentOpen, Type: projectstate.ReviewCommentTypeQuestion},
	}
	got := bulkResolveAnswered(thread)
	if len(got) != 2 {
		t.Fatalf("approve resolves every answered thread, got %v", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd server && GOWORK=off go test ./internal/manager/systemdesign/ -run 'TestCheckCommentTransition|TestOpenChangeRequests|TestBulkResolve' -v
```

Expected: FAIL — `undefined: bulkResolveAnswered`, and the transition test fails
on the old `open->waived` rule.

- [ ] **Step 3: Implement**

`SetReviewCommentStatus` guard (`:696-701`) becomes:

```go
	switch status {
	case projectstate.ReviewCommentResolved, projectstate.ReviewCommentOpen:
		// close (open|answered -> resolved) or reopen (resolved -> open) — the only
		// reviewer-authored transitions. "answered" is derived by the server from the
		// reply history and is never set by a human.
	default:
		return newError(fwmanager.ContractMisuse, "status must be \"resolved\" (to close a thread) or \"open\" (to reopen a resolved thread)")
	}
```

`checkCommentTransition` (`:741`) becomes:

```go
		switch {
		case (c.Status == projectstate.ReviewCommentOpen || c.Status == projectstate.ReviewCommentAnswered) &&
			status == projectstate.ReviewCommentResolved:
			return nil
		case c.Status == projectstate.ReviewCommentResolved && status == projectstate.ReviewCommentOpen:
			return nil
		default:
			return newError(fwmanager.FailedPrecondition,
				fmt.Sprintf("cannot change comment %s from %q to %q (allowed: open->resolved, answered->resolved, resolved->open)", id, c.Status, status))
		}
```

Add:

```go
// bulkResolveAnswered returns the ids of every ANSWERED thread on the slot. Approve
// resolves them all in one gesture, so accepting a redraft that answered eight change
// requests does not cost eight Resolve clicks (design §3.4). Threads the reviewer
// explicitly reopened are OPEN, not answered, so they are excluded and keep blocking.
func bulkResolveAnswered(thread []ReviewCommentView) []string {
	var ids []string
	for _, c := range thread {
		if c.Status == projectstate.ReviewCommentAnswered {
			ids = append(ids, c.ID)
		}
	}
	return ids
}
```

In the approve path of `SubmitReviewDecision`, after the approve precondition
passes and before the commit signal, signal one `setCommentStatusSignal{CommentID:
id, Status: projectstate.ReviewCommentResolved}` per id from `bulkResolveAnswered`.

At `:4463`, replace `Response: c.Response` with `Replies: toViewReplies(c.Replies)`
and add `Reopened: c.Reopened`, writing the obvious element-wise `toViewReplies`
converter beside it.

In `coauthorartifact.go`, update the `setCommentStatusSignal` doc comment at
`:3182` from `(open->waived / addressed->open)` to `(open|answered->resolved /
resolved->open)`.

- [ ] **Step 4: Thread `replyTo` through the submit path**

Without this, a staged reply opens a brand-new thread instead of answering the one
it was written against — the whole point of §3.7.

Write the failing test first:

```go
func TestSubmitRoutesReplyToAnExistingThread(t *testing.T) {
	existing := []projectstate.ReviewComment{{ID: "r1c0", Text: "Objective 3 is vague", Status: projectstate.ReviewCommentAnswered,
		Replies: []projectstate.ReviewCommentReply{{ID: "r1c0-u1", AuthorRole: "architect", Text: "Reworded it.", At: "2026-09-19T00:00:00Z"}}}}
	incoming := []AnchoredComment{
		{Text: "Still vague", ReplyTo: "r1c0"},
		{Text: "And objective 4 overlaps it", JSONPath: "$.objectives[3]"},
	}

	got, err := applyIncomingComments(existing, 2, incoming, "architect-user", "2026-09-19T02:00:00Z")
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("a reply must NOT open a thread; want 2 entries, got %d", len(got))
	}
	if len(got[0].Replies) != 2 {
		t.Fatalf("the reply must append to r1c0; got %d replies", len(got[0].Replies))
	}
	if got[0].Replies[1].AuthorRole != "architect-user" {
		t.Fatalf("reply author = %q, want architect-user", got[0].Replies[1].AuthorRole)
	}
	if got[1].Text != "And objective 4 overlaps it" {
		t.Fatalf("the unaddressed comment must open a new thread, got %q", got[1].Text)
	}

	// An unknown replyTo is a caller error, not a silent new thread.
	if _, err := applyIncomingComments(existing, 2, []AnchoredComment{{Text: "x", ReplyTo: "nope"}}, "architect-user", "t"); err == nil {
		t.Fatal("expected an error for a replyTo naming no thread")
	}
}
```

Run it:

```bash
cd server && GOWORK=off go test ./internal/manager/systemdesign/ -run TestSubmitRoutesReplyTo -v
```

Expected: FAIL — `undefined: applyIncomingComments`, `AnchoredComment.ReplyTo undefined`
if Task 1 Step 3 was skipped.

Implement it beside `appendReviewComments`' caller:

```go
// applyIncomingComments splits one submitted batch into the two things it can be:
// utterances answering an EXISTING thread (replyTo non-empty), which append in
// place, and fresh anchored comments, which open new threads for this round. A
// replyTo naming no thread is a ContractMisuse rather than a silent new thread —
// silently reinterpreting a reply as a new comment would lose the reviewer's
// place in the conversation.
func applyIncomingComments(
	thread []projectstate.ReviewComment,
	round int64,
	incoming []AnchoredComment,
	authorRole string,
	at string,
) ([]projectstate.ReviewComment, error) {
	var fresh []AnchoredComment
	for _, c := range incoming {
		if c.ReplyTo == "" {
			fresh = append(fresh, c)
			continue
		}
		idx := -1
		for i := range thread {
			if thread[i].ID == c.ReplyTo {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil, newError(fwmanager.ContractMisuse,
				"replyTo names no thread on this artifact: "+c.ReplyTo)
		}
		e := &thread[idx]
		e.Replies = append(e.Replies, projectstate.ReviewCommentReply{
			ID:         fmt.Sprintf("%s-u%d", e.ID, len(e.Replies)+1),
			AuthorRole: authorRole,
			Text:       c.Text,
			At:         at,
		})
	}
	return appendAnchoredComments(thread, round, fresh), nil
}
```

`appendAnchoredComments` is the existing seed path that turns fresh
`AnchoredComment`s into new `ReviewComment` entries — reuse it under whatever name
`systemdesignmanager.go` already gives it. Call `applyIncomingComments` wherever
the reject/amend path currently hands its comments straight to the RA seed verb.

- [ ] **Step 5: Run to verify pass**

```bash
cd server && GOWORK=off go test ./internal/manager/systemdesign/ -run 'TestCheckCommentTransition|TestOpenChangeRequests|TestBulkResolve|TestSubmitRoutesReplyTo' -v
```

Expected: PASS.

- [ ] **Step 6: Run the full server suite**

```bash
cd server && GOWORK=off go test ./...
```

Expected: PASS. Apply the same vocabulary change to
`internal/manager/projectdesign` if its tests fail — it carries the same
`ReviewCommentView`.

- [ ] **Step 7: Commit**

```bash
git add server/internal/manager/
git commit -m "feat(review): reviewer-only resolve, bulk-resolve on approve

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 5: SPA contract + queued replies

**Files:**
- Modify: `webApp/src/contracts/types.ts`
- Modify: `webApp/src/components/comments/commentContextTypes.ts`
- Modify: `webApp/src/components/comments/CommentContext.tsx`
- Modify: `webApp/src/components/comments/pendingCommentsStore.ts`
- Test: `webApp/src/components/comments/commentContext.test.ts`

**Interfaces:**
- Produces: `ReviewCommentReply { id, authorRole, text, at }`;
  `ReviewCommentView.replies: ReviewCommentReply[]` and `.reopened: boolean`;
  `PostedComment.replyTo?: string`;
  `post(text, { commentType, addressee, replyTo })`.

- [ ] **Step 1: Regenerate the wire types**

```bash
cd webApp && npm run gen:api && npm run typecheck 2>&1 | head -30
```

Expected: typecheck FAILS in `ChatRail.tsx` on `entry.response`. That is the
signal the new contract landed; Task 8 deletes that file.

- [ ] **Step 2: Write the failing test**

Add to `commentContext.test.ts`:

```ts
test('a staged reply carries replyTo and survives a persist round-trip', () => {
  const store = memoryStore();
  const staged: PostedComment[] = [
    { text: 'Still too vague', anchor: null, commentType: 'changeRequest', replyTo: 'r1c1' },
  ];
  savePending(store, 'proj:mission', staged, 7);
  const loaded = loadPending(store, 'proj:mission', 7);
  assert.equal(loaded.length, 1);
  assert.equal(loaded[0].replyTo, 'r1c1');
});

test('a new thread stages with no replyTo', () => {
  const store = memoryStore();
  savePending(store, 'proj:mission', [
    { text: 'Objective 3 is vague', anchor: null, commentType: 'changeRequest' },
  ], 7);
  assert.equal(loadPending(store, 'proj:mission', 7)[0].replyTo, undefined);
});
```

Reuse the existing in-memory storage double in that file; if there is none, write
`memoryStore()` as a `Map`-backed object matching the `Storage` shape
`pendingCommentsStore` expects.

- [ ] **Step 3: Run to verify failure**

```bash
cd webApp && npm test 2>&1 | tail -20
```

Expected: FAIL — `replyTo` is not a property of `PostedComment`.

- [ ] **Step 4: Implement**

In `commentContextTypes.ts` add to `PostedComment` and `PostOptions`:

```ts
  /**
   * The durable review-ledger thread this utterance answers. Absent means the
   * comment opens a NEW thread. A reply stages exactly like a new comment — it
   * does not dispatch — and rides the next batch verb (design §3.7).
   */
  replyTo?: string;
```

In `CommentContext.tsx`, thread `replyTo` from `PostOptions` through `post()` into
the stored `PostedComment`. `pendingCommentsStore` needs no shape change (it
serializes `PostedComment` wholesale) — verify by reading `savePending`.

- [ ] **Step 5: Run to verify pass**

```bash
cd webApp && npm test 2>&1 | tail -20
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add webApp/src/contracts webApp/src/components/comments
git commit -m "feat(spa): stage replies against existing review threads

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 6: Margin geometry — the pure module

**Files:**
- Create: `webApp/src/components/comments/commentMarginLayout.ts`
- Test: `webApp/src/components/comments/commentMarginLayout.test.ts`

**Interfaces:**
- Produces:
  ```ts
  export interface MarginCard { id: string; desiredTop: number; height: number }
  export interface PlacedCard { id: string; top: number }
  export function stackCards(cards: readonly MarginCard[], gap: number): PlacedCard[]
  ```

This is a `.ts` module precisely so `node --test` can reach it; the `.tsx` in Task 8
is a thin renderer over it.

- [ ] **Step 1: Write the failing test**

```ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { stackCards } from './commentMarginLayout.ts';

test('cards that do not collide keep their desired position', () => {
  const got = stackCards(
    [
      { id: 'a', desiredTop: 0, height: 40 },
      { id: 'b', desiredTop: 200, height: 40 },
    ],
    8
  );
  assert.deepEqual(got, [
    { id: 'a', top: 0 },
    { id: 'b', top: 200 },
  ]);
});

test('a colliding card is pushed down by the gap', () => {
  const got = stackCards(
    [
      { id: 'a', desiredTop: 0, height: 40 },
      { id: 'b', desiredTop: 10, height: 40 },
    ],
    8
  );
  assert.deepEqual(got, [
    { id: 'a', top: 0 },
    { id: 'b', top: 48 },
  ]);
});

test('a cascade pushes every subsequent card', () => {
  const got = stackCards(
    [
      { id: 'a', desiredTop: 0, height: 40 },
      { id: 'b', desiredTop: 0, height: 40 },
      { id: 'c', desiredTop: 0, height: 40 },
    ],
    8
  );
  assert.deepEqual(got.map((c) => c.top), [0, 48, 96]);
});

test('input is sorted by desiredTop regardless of argument order', () => {
  const got = stackCards(
    [
      { id: 'late', desiredTop: 300, height: 40 },
      { id: 'early', desiredTop: 0, height: 40 },
    ],
    8
  );
  assert.deepEqual(got.map((c) => c.id), ['early', 'late']);
});

test('an empty list places nothing', () => {
  assert.deepEqual(stackCards([], 8), []);
});
```

- [ ] **Step 2: Run to verify failure**

```bash
cd webApp && npm test 2>&1 | tail -20
```

Expected: FAIL — cannot find `./commentMarginLayout.ts`.

- [ ] **Step 3: Implement**

```ts
/**
 * Margin-card placement: the Google-Docs stacking pass.
 *
 * Every comment thread wants to sit level with the content it anchors to. When
 * two anchors are close enough that their cards would overlap, the later card
 * slides down just far enough to clear the earlier one — so cards never overlap,
 * never reorder, and stay as close to their anchor as the neighbours allow.
 *
 * Pure and DOM-free so `node --test` can reach it; the renderer measures anchors
 * and hands the numbers here.
 */
export interface MarginCard {
  id: string;
  /** Anchor offset within the scroll container, in px. */
  desiredTop: number;
  /** Measured card height, in px. */
  height: number;
}

export interface PlacedCard {
  id: string;
  top: number;
}

export function stackCards(cards: readonly MarginCard[], gap: number): PlacedCard[] {
  const sorted = [...cards].sort((a, b) => a.desiredTop - b.desiredTop);
  const placed: PlacedCard[] = [];
  let floor = Number.NEGATIVE_INFINITY;
  for (const card of sorted) {
    const top = Math.max(card.desiredTop, floor);
    placed.push({ id: card.id, top });
    floor = top + card.height + gap;
  }
  return placed;
}
```

- [ ] **Step 4: Run to verify pass**

```bash
cd webApp && npm test 2>&1 | tail -20
```

Expected: PASS, five tests.

- [ ] **Step 5: Commit**

```bash
git add webApp/src/components/comments/commentMarginLayout.ts webApp/src/components/comments/commentMarginLayout.test.ts
git commit -m "feat(spa): margin card stacking pass

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 7: Anchor registry

**Files:**
- Create: `webApp/src/components/comments/AnchorRegistry.tsx`
- Modify: `webApp/src/components/comments/CommentableList.tsx`
- Modify: `webApp/src/utilities/constants/UIIdentifiers.ts`

**Interfaces:**
- Consumes: `Anchor` from `CommentContext`.
- Produces:
  ```tsx
  export function AnchorRegistryProvider({ children }: { children: ReactNode }): ReactNode
  export function useRegisterAnchor(jsonPath: string): (el: HTMLElement | null) => void
  export function useAnchorOffsets(jsonPaths: readonly string[], scrollRoot: HTMLElement | null): Map<string, number>
  ```

- [ ] **Step 1: Implement the registry**

```tsx
/**
 * Maps a comment anchor's typed-model JSONPath to the DOM element that renders it,
 * so a margin card can find the row or node it belongs beside — and so clicking a
 * card can scroll that content into view. Registration is a ref callback, so a row
 * enrols itself by rendering and un-enrols by unmounting; nothing has to be
 * declared centrally.
 */
import { createContext, useCallback, useContext, useMemo, useRef, type ReactNode } from 'react';

interface Registry {
  register: (jsonPath: string, el: HTMLElement | null) => void;
  lookup: (jsonPath: string) => HTMLElement | null;
}

const Ctx = createContext<Registry | null>(null);

export function AnchorRegistryProvider({ children }: { children: ReactNode }): ReactNode {
  const map = useRef(new Map<string, HTMLElement>());
  const value = useMemo<Registry>(
    () => ({
      register: (jsonPath, el) => {
        if (el === null) map.current.delete(jsonPath);
        else map.current.set(jsonPath, el);
      },
      lookup: (jsonPath) => map.current.get(jsonPath) ?? null,
    }),
    []
  );
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

/** Ref callback that enrols this element under `jsonPath`. No-op with no provider. */
export function useRegisterAnchor(jsonPath: string): (el: HTMLElement | null) => void {
  const reg = useContext(Ctx);
  return useCallback(
    (el: HTMLElement | null) => {
      reg?.register(jsonPath, el);
    },
    [reg, jsonPath]
  );
}

/** Offset of each anchor within `scrollRoot`, in px. Missing anchors are omitted. */
export function useAnchorOffsets(
  jsonPaths: readonly string[],
  scrollRoot: HTMLElement | null
): Map<string, number> {
  const reg = useContext(Ctx);
  return useMemo(() => {
    const out = new Map<string, number>();
    if (reg === null || scrollRoot === null) return out;
    const rootTop = scrollRoot.getBoundingClientRect().top - scrollRoot.scrollTop;
    for (const p of jsonPaths) {
      const el = reg.lookup(p);
      if (el !== null) out.set(p, el.getBoundingClientRect().top - rootTop);
    }
    return out;
    // Re-measure whenever the anchor set changes; the renderer additionally
    // re-runs this on scroll/resize via its own state bump.
  }, [reg, jsonPaths, scrollRoot]);
}
```

- [ ] **Step 2: Register every CommentableList row**

In `CommentableList.tsx`, inside the row render, add the registry ref alongside the
existing `rowRefs` assignment. Both must run — compose them:

```tsx
const registerAnchor = useRegisterAnchor(getAnchor(item, index).jsonPath);
// ...on the row Box:
ref={(el: HTMLDivElement | null) => {
  rowRefs.current[index] = el;
  registerAnchor(el);
}}
```

Hooks may not be called inside a `.map()` callback, so extract the row into a
`CommentableRow` child component and call `useRegisterAnchor` there. Keep the
roving-tabindex behaviour exactly as it is — this task adds registration and
changes nothing else.

- [ ] **Step 3: Add the test ids**

In `UIIdentifiers.ts`, add a `Margin` block beside `Chat`:

```ts
  Margin: {
    ROOT: 'comment-margin',
    UNPLACED: 'comment-margin-unplaced',
    RESOLVED_DISCLOSURE: 'comment-margin-resolved',
    card: (id: string) => `margin-card-${id}`,
    reply: (id: string) => `margin-reply-${id}`,
    resolve: (id: string) => `margin-resolve-${id}`,
    reopen: (id: string) => `margin-reopen-${id}`,
  },
```

- [ ] **Step 4: Typecheck**

```bash
cd webApp && npm run typecheck
```

Expected: the only errors are the pre-existing `ChatRail.tsx` `entry.response`
errors from Task 5. Nothing new.

- [ ] **Step 5: Commit**

```bash
git add webApp/src/components/comments webApp/src/utilities/constants/UIIdentifiers.ts
git commit -m "feat(spa): anchor registry for margin placement

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 8: CommentMargin replaces ChatRail on Mission

**Files:**
- Create: `webApp/src/components/design/CommentMargin.tsx`
- Create: `webApp/src/components/design/MarginThreadCard.tsx`
- Delete: `webApp/src/components/design/ChatRail.tsx`
- Modify: `webApp/src/containers/SystemDesignContainer.tsx:373`
- Modify: `webApp/src/components/design/ExperienceChrome.tsx:25-52`
- Modify: `webApp/src/routes/ProjectDesignExperience.tsx:447`, `webApp/src/routes/ConstructionConsole.tsx:1121`

**Interfaces:**
- Consumes: `stackCards` (Task 6), `useAnchorOffsets` / `useRegisterAnchor` (Task 7),
  `PostedComment.replyTo` (Task 5), `ReviewCommentView.replies` (Task 1).
- Produces: `<CommentMargin thread committed onResolve onReopen scrollRoot />`;
  `ExperienceChrome` prop `margin?: ReactNode` replacing `chat`.

Phase-2 and the Construction Console are **not** being redesigned here — they are
only re-pointed at `CommentMargin` so the build stays green. Their own review pass
is Stage 2.

- [ ] **Step 1: Build `MarginThreadCard`**

The anchor reference is a **button**, not a `Tooltip` — clicking it scrolls its
content into view. That is complaint #5, and it is the one thing in this component
that must not be got wrong.

```tsx
export function MarginThreadCard({
  entry, active, onActivate, onJumpToAnchor, onResolve, onReopen, statusPending,
}: {
  entry: ReviewCommentView;
  active: boolean;
  onActivate: () => void;
  onJumpToAnchor: () => void;
  onResolve: (id: string) => void;
  onReopen: (id: string) => void;
  statusPending: boolean;
}): ReactNode {
  const t = useTokens();
  const { post } = useComments();
  const [reply, setReply] = useState('');
  const resolved = entry.status === 'resolved';

  if (resolved && !active) {
    return (
      <Box data-testid={UI_IDENTIFIERS.Margin.card(entry.id)} onClick={onActivate}
        sx={{ p: 1, opacity: 0.6, fontSize: 12, cursor: 'pointer',
              overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
        {entry.text}
      </Box>
    );
  }

  return (
    <Paper data-testid={UI_IDENTIFIERS.Margin.card(entry.id)} onClick={onActivate}
      sx={{ p: 1.25, border: `1.5px solid ${active ? t.accent : t.line}`, bgcolor: t.paper }}>
      {/* the anchor reference — a real button back to the content */}
      {entry.anchorText.length > 0 ? (
        <Button size="small" startIcon={<PlaceIcon sx={{ fontSize: 13 }} />}
          sx={{ textTransform: 'none', fontSize: 11.5, color: t.accent, minWidth: 0, p: 0.25 }}
          onClick={(e) => { e.stopPropagation(); onJumpToAnchor(); }}>
          {entry.anchorText}
        </Button>
      ) : null}

      <Box sx={{ display: 'flex', gap: 0.5, my: 0.5 }}>
        <Chip label={entry.type === 'question' ? `question → ${entry.addressee}` : 'change request'}
          size="small" sx={{ height: 17, fontSize: 9.5, fontFamily: t.mono }} variant="outlined" />
        <Chip label={entry.status} size="small"
          sx={{ height: 17, fontSize: 9.5, fontFamily: t.mono }} variant="outlined" />
      </Box>

      <Typography sx={{ fontSize: 13, lineHeight: 1.45 }}>{entry.text}</Typography>

      {entry.replies.map((r) => (
        <Box key={r.id} sx={{ mt: 0.75, pl: 1, borderLeft: `2px solid ${t.line}` }}>
          <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted }}>{r.authorRole}</Typography>
          <Typography sx={{ fontSize: 12.5, lineHeight: 1.45 }}>{r.text}</Typography>
        </Box>
      ))}

      {active ? (
        <Box sx={{ mt: 1, display: 'flex', gap: 0.5, alignItems: 'flex-end' }}>
          <InputBase multiline data-testid={UI_IDENTIFIERS.Margin.reply(entry.id)}
            maxRows={4} placeholder="Reply…" value={reply}
            sx={{ flexGrow: 1, fontSize: 12.5, border: `1.5px solid ${t.line}`, borderRadius: 1, px: 1 }}
            onChange={(e) => { setReply(e.target.value); }} />
          <IconButton disabled={reply.trim().length === 0} size="small"
            onClick={() => { post(reply, { commentType: entry.type, replyTo: entry.id }); setReply(''); }}>
            <SendIcon sx={{ fontSize: 15 }} />
          </IconButton>
        </Box>
      ) : null}

      <Button data-testid={resolved ? UI_IDENTIFIERS.Margin.reopen(entry.id) : UI_IDENTIFIERS.Margin.resolve(entry.id)}
        disabled={statusPending} size="small"
        sx={{ mt: 0.5, fontSize: 11, textTransform: 'none', color: t.muted }}
        onClick={(e) => { e.stopPropagation(); (resolved ? onReopen : onResolve)(entry.id); }}>
        {resolved ? 'Reopen' : 'Resolve'}
      </Button>
    </Paper>
  );
}
```

- [ ] **Step 2: Build `CommentMargin`**

```tsx
const GAP = 8;

export function CommentMargin({
  thread, scrollRoot, statusPending, onResolve, onReopen,
}: {
  thread: readonly ReviewCommentView[];
  /** The artifact's scroll container — anchor offsets are measured against it. */
  scrollRoot: HTMLElement | null;
  statusPending: boolean;
  onResolve: (id: string) => void;
  onReopen: (id: string) => void;
}): ReactNode {
  const [activeId, setActiveId] = useState<string | null>(null);
  const [heights, setHeights] = useState<Map<string, number>>(new Map());
  const [tick, setTick] = useState(0);

  // Re-measure on scroll and resize, one rAF per frame at most: measurement reads
  // layout, so an unthrottled listener would thrash it on every scroll event.
  useEffect(() => {
    if (scrollRoot === null) return;
    let queued = false;
    const bump = (): void => {
      if (queued) return;
      queued = true;
      requestAnimationFrame(() => { queued = false; setTick((n) => n + 1); });
    };
    scrollRoot.addEventListener('scroll', bump, { passive: true });
    window.addEventListener('resize', bump);
    return () => {
      scrollRoot.removeEventListener('scroll', bump);
      window.removeEventListener('resize', bump);
    };
  }, [scrollRoot]);

  const paths = useMemo(() => thread.map((e) => e.anchor), [thread]);
  const offsets = useAnchorOffsets(paths, scrollRoot, tick);

  const { placed, unplaced } = useMemo(() => {
    const anchored: MarginCard[] = [];
    const orphans: ReviewCommentView[] = [];
    for (const e of thread) {
      const top = offsets.get(e.anchor);
      if (top === undefined) orphans.push(e);
      else anchored.push({ id: e.id, desiredTop: top, height: heights.get(e.id) ?? 96 });
    }
    return { placed: stackCards(anchored, GAP), unplaced: orphans };
  }, [thread, offsets, heights]);

  const byId = new Map(thread.map((e) => [e.id, e]));
  const measure = (id: string) => (el: HTMLDivElement | null) => {
    if (el === null) return;
    const h = el.getBoundingClientRect().height;
    setHeights((prev) => (prev.get(id) === h ? prev : new Map(prev).set(id, h)));
  };

  return (
    <Box data-testid={UI_IDENTIFIERS.Margin.ROOT} sx={{ position: 'relative', width: 300, flexShrink: 0 }}>
      {unplaced.length > 0 ? (
        <Box data-testid={UI_IDENTIFIERS.Margin.UNPLACED} sx={{ mb: 1 }}>
          {unplaced.map((e) => (
            <Box key={e.id} ref={measure(e.id)}>
              <MarginThreadCard active={activeId === e.id} entry={e} statusPending={statusPending}
                onActivate={() => { setActiveId(e.id); }} onJumpToAnchor={() => undefined}
                onReopen={onReopen} onResolve={onResolve} />
            </Box>
          ))}
        </Box>
      ) : null}

      {placed.map((p) => {
        const e = byId.get(p.id);
        if (e === undefined) return null;
        return (
          <Box key={p.id} ref={measure(p.id)} sx={{ position: 'absolute', top: p.top, left: 0, right: 0 }}>
            <MarginThreadCard active={activeId === p.id} entry={e} statusPending={statusPending}
              onActivate={() => { setActiveId(p.id); }}
              onJumpToAnchor={() => { scrollAnchorIntoView(e.anchor); }}
              onReopen={onReopen} onResolve={onResolve} />
          </Box>
        );
      })}
    </Box>
  );
}
```

`useAnchorOffsets` gains a third `tick: number` parameter, added to its `useMemo`
deps, so the scroll/resize bump forces a re-measure. `scrollAnchorIntoView` is a
registry helper: `lookup(jsonPath)?.scrollIntoView({ block: 'center', behavior:
'smooth' })`. Export it from `AnchorRegistry.tsx` as a hook,
`useScrollAnchorIntoView()`, returning `(jsonPath: string) => void`.

- [ ] **Step 3: Delete `ChatRail.tsx` and re-point all three consumers**

`ExperienceChrome`'s `chat` prop becomes `margin`; the collapse affordance stays for
the narrow-viewport drawer (<1100px).

- [ ] **Step 4: Typecheck and lint**

```bash
cd webApp && npm run typecheck && npm run lint
```

Expected: PASS, zero errors. Any surviving reference to `ChatRail` or
`entry.response` is a missed consumer.

- [ ] **Step 5: Run the SPA unit tests**

```bash
cd webApp && npm test
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add webApp/src
git commit -m "feat(spa): margin comment threads replace the chat rail

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 9: Chrome cleanup

**Files:**
- Modify: `webApp/src/components/design/SystemDesignView.tsx:330-335`
- Modify: `webApp/src/routes/ProjectDesignExperience.tsx:507-512`
- Modify: `webApp/src/components/design/CommittedArtifactPanel.tsx:150-200`
- Modify: `webApp/src/components/design/ArtifactIntro.tsx:56-100`

- [ ] **Step 1: Delete the slot-path subtitle**

Remove the `<Typography>` rendering `{meta.stateAddress} · step {safeIndex + 1} of
{spine.length}` from both files. Step position is already in the `SlimSpine`.

- [ ] **Step 2: Move the slot path into the `(?)` popover**

In `ArtifactInfoButton`, accept a `stateAddress?: string` prop and render it as a
muted mono line beneath the existing copy. Pass `meta.stateAddress` at both call
sites. Change the `if (copy === undefined) return null` early-return so the button
still renders when there is a `stateAddress` but no framing copy — otherwise the
prose artifacts (Mission included) lose the path entirely.

- [ ] **Step 3: Collapse the committed strip into a chip**

In `CommittedArtifactPanel`, delete the `<Paper>` header strip and the
`provLine` `<Typography>` beneath it. Export instead:

```tsx
export function CommittedChip({ revisions, provenance }: {
  revisions?: number | undefined;
  provenance?: ArtifactProvenance | undefined;
}): ReactNode
```

rendering `committed` / `committed · r{n}` with `provenanceSummary(provenance)` as
its `Tooltip` title, styled to match `StageChip`. Render it in the
`SystemDesignView` header where `showsCommittedPanel` currently suppresses
`StageChip`.

- [ ] **Step 4: Delete the header Amend button**

Remove the `Amend` `<Button>` from `CommittedArtifactPanel`. **Keep** the amend
`<Dialog>`, the composer state, and the `onAmend` prop — Task 10's submit bar opens
that same dialog. Export an imperative opener or lift `open` to the caller.

- [ ] **Step 5: Verify in the running app**

```bash
cd webApp && npm run typecheck && npm run lint
```

Then load `http://localhost:5199/project/archistrator/design/system/mission` and
confirm: no slot-path line, no full-width committed strip, a `committed · r2` chip
beside the title, and the mission's Vision heading visibly higher than before.

- [ ] **Step 6: Commit**

```bash
git add webApp/src
git commit -m "feat(spa): recover the design header's vertical space

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 10: The submit bar

**Files:**
- Create: `webApp/src/components/design/submitVerb.ts`
- Create: `webApp/src/components/design/SubmitBar.tsx`
- Test: `webApp/src/components/design/submitVerb.test.ts`
- Modify: `webApp/src/components/design/GatePanel.tsx`
- Modify: `webApp/src/components/design/SystemDesignView.tsx`

**Interfaces:**
- Produces:
  ```ts
  export type SubmitAction = 'sendBack' | 'approve' | 'amend' | 'ask' | 'none';
  export interface SubmitVerb { action: SubmitAction; label: string; consequence: string; disabled: boolean }
  export function resolveSubmitVerb(input: {
    committed: boolean;
    stage: 'drafted' | 'awaitingReview' | 'other';
    stagedChangeRequests: number;
    stagedQuestions: number;
    openThreads: number;
  }): SubmitVerb
  ```

- [ ] **Step 1: Write the failing test**

```ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { resolveSubmitVerb } from './submitVerb.ts';

const base = { committed: false, stage: 'awaitingReview' as const, stagedChangeRequests: 0, stagedQuestions: 0, openThreads: 0 };

test('staged change requests send the draft back', () => {
  const v = resolveSubmitVerb({ ...base, stagedChangeRequests: 2, stagedQuestions: 1 });
  assert.equal(v.action, 'sendBack');
  assert.equal(v.label, 'Send back (3)');
  assert.equal(v.consequence, '2 change requests → redraft · 1 question → PM');
});

test('questions alone ask without a redraft', () => {
  const v = resolveSubmitVerb({ ...base, stagedQuestions: 1 });
  assert.equal(v.action, 'ask');
  assert.equal(v.label, 'Ask (1) — no redraft');
});

test('nothing staged and nothing open approves', () => {
  const v = resolveSubmitVerb(base);
  assert.equal(v.action, 'approve');
  assert.equal(v.disabled, false);
});

test('open threads block approve and say so', () => {
  const v = resolveSubmitVerb({ ...base, openThreads: 3 });
  assert.equal(v.action, 'approve');
  assert.equal(v.disabled, true);
  assert.equal(v.label, 'Resolve 3 threads to approve');
});

test('a committed slot amends', () => {
  const v = resolveSubmitVerb({ ...base, committed: true, stage: 'other', stagedChangeRequests: 1 });
  assert.equal(v.action, 'amend');
  assert.equal(v.label, 'Amend (1)');
});

test('a committed slot with nothing staged offers no primary verb', () => {
  assert.equal(resolveSubmitVerb({ ...base, committed: true, stage: 'other' }).action, 'none');
});
```

- [ ] **Step 2: Run to verify failure**

```bash
cd webApp && npm test 2>&1 | tail -20
```

Expected: FAIL — cannot find `./submitVerb.ts`.

- [ ] **Step 3: Implement `resolveSubmitVerb`**

```ts
/**
 * The single review verb, resolved from what is staged and where the slot is in
 * its lifecycle. One function so the bar, its tests, and any future surface agree
 * on what pressing it will do — the scattering of Amend / Ask / Send back across
 * three affordances is the thing this replaces.
 */
export type SubmitAction = 'sendBack' | 'approve' | 'amend' | 'ask' | 'none';

export interface SubmitVerb {
  action: SubmitAction;
  label: string;
  /** Always shown beneath the verb: what pressing it actually dispatches. */
  consequence: string;
  disabled: boolean;
}

export function resolveSubmitVerb(input: {
  committed: boolean;
  stage: 'drafted' | 'awaitingReview' | 'other';
  stagedChangeRequests: number;
  stagedQuestions: number;
  openThreads: number;
}): SubmitVerb {
  const { committed, stagedChangeRequests: crs, stagedQuestions: qs, openThreads } = input;
  const staged = crs + qs;
  const consequence = describeConsequence(crs, qs, committed);

  // Questions alone never redraft — that is the whole point of the ask path.
  if (staged > 0 && crs === 0) {
    return { action: 'ask', label: `Ask (${String(qs)}) — no redraft`, consequence, disabled: false };
  }
  if (staged > 0) {
    return committed
      ? { action: 'amend', label: `Amend (${String(staged)})`, consequence, disabled: false }
      : { action: 'sendBack', label: `Send back (${String(staged)})`, consequence, disabled: false };
  }
  if (committed) {
    return { action: 'none', label: '', consequence: '', disabled: true };
  }
  if (openThreads > 0) {
    return {
      action: 'approve',
      label: `Resolve ${String(openThreads)} thread${openThreads === 1 ? '' : 's'} to approve`,
      consequence: 'Open change requests block approval',
      disabled: true,
    };
  }
  return { action: 'approve', label: 'Approve', consequence: 'Commits the artifact and advances', disabled: false };
}

function describeConsequence(crs: number, qs: number, committed: boolean): string {
  const parts: string[] = [];
  if (crs > 0) {
    parts.push(`${String(crs)} change request${crs === 1 ? '' : 's'} → ${committed ? 'amend' : 'redraft'}`);
  }
  if (qs > 0) parts.push(`${String(qs)} question${qs === 1 ? '' : 's'} → PM`);
  return parts.join(' · ');
}
```

Note the test expects `'2 change requests → redraft · 1 question → PM'` for an
uncommitted slot — `describeConsequence` says `amend` instead of `redraft` only when
`committed` is true.

- [ ] **Step 4: Run to verify pass**

```bash
cd webApp && npm test 2>&1 | tail -20
```

Expected: PASS, six tests.

- [ ] **Step 5: Build `SubmitBar` and move the gate's buttons into it**

`SubmitBar` renders the verb, the consequence line beneath it, and an overflow
`IconButton` menu carrying Withdraw and Retry. Mount it sticky at the bottom of the
`SystemDesignView` scroll column. Remove the action buttons from `GatePanel`,
**keeping its findings and diagnostics rendering untouched**, and delete the rail's
`Ask` button along with `UI_IDENTIFIERS.Chat.ASK`.

- [ ] **Step 6: Verify and commit**

```bash
cd webApp && npm run typecheck && npm run lint && npm test
git add webApp/src
git commit -m "feat(spa): one submit bar for every review verb

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 11: Playwright acceptance

**Files:**
- Create: `uitests/tests/comment-margin.spec.ts`
- Modify: `uitests/tests/support/testids.ts`
- Modify: `uitests/tests/support/designStubs.ts`

- [ ] **Step 1: Add the margin test ids and a stub**

Export `UI_IDENTIFIERS.Margin` entries through `TESTID`, and add
`stubCommittedMissionWithThread(page)` to `designStubs.ts` — a committed mission
slot whose `reviewThread` holds one `open` change request anchored to
`$.objectives[2]`, and one `answered` question addressed to `pm` with a single
agent reply.

- [ ] **Step 2: Write the acceptance spec**

Under the existing `dispatchGuard` over a stubbed project, so it creates nothing:

```ts
import { test, expect } from './support/dispatchGuard.js';
import { TESTID } from './support/testids.js';
import { requireServer, gotoApp } from './support/gating.js';
import { stubCommittedMissionWithThread } from './support/designStubs.js';

const CR_ID = 'r1c0'; // the open change request anchored to $.objectives[2]

test.beforeEach(async ({ page }) => {
  await requireServer();
  const id = await stubCommittedMissionWithThread(page);
  await gotoApp(page, `/project/${id}/design/system/mission`);
  await expect(page.getByTestId(TESTID.margin.ROOT)).toBeVisible();
});

test('a thread card sits beside the objective it anchors to', async ({ page }) => {
  const card = await page.getByTestId(TESTID.margin.card(CR_ID)).boundingBox();
  const row = await page.getByRole('listitem').nth(2).boundingBox();
  expect(card).not.toBeNull();
  expect(row).not.toBeNull();
  // Level with its anchor, within one card's stacking slack.
  expect(Math.abs(card!.y - row!.y)).toBeLessThan(24);
});

test('clicking a card scrolls its anchored objective into view', async ({ page }) => {
  const row = page.getByRole('listitem').nth(2);
  await page.getByTestId(TESTID.designScroll).evaluate((el) => { el.scrollTop = el.scrollHeight; });
  await expect(row).not.toBeInViewport();
  await page.getByTestId(TESTID.margin.card(CR_ID)).getByRole('button', { name: /objective/i }).click();
  await expect(row).toBeInViewport();
});

test('the submit bar names the consequence of what is staged', async ({ page }) => {
  await page.getByTestId(TESTID.margin.card(CR_ID)).click();
  await page.getByTestId(TESTID.margin.reply(CR_ID)).fill('Still too vague');
  await page.getByTestId(TESTID.margin.card(CR_ID)).getByLabel('post comment').click();
  await expect(page.getByTestId(TESTID.submitBar)).toContainText('Amend (1)');
  await expect(page.getByTestId(TESTID.submitBar)).toContainText('1 change request → amend');
});

test('the design header shows no slot path and no committed strip', async ({ page }) => {
  await expect(page.getByText('project.json → slots.mission')).toHaveCount(0);
  await expect(page.getByTestId(TESTID.committedRevision)).toHaveCount(0);
  await expect(page.getByText(/committed · r\d+/)).toBeVisible();
});
```

`TESTID.designScroll` and `TESTID.submitBar` are new ids — add
`DESIGN_SCROLL: 'design-scroll'` and `SUBMIT_BAR: 'submit-bar'` to
`UI_IDENTIFIERS.DesignExperience` in Tasks 8 and 10 respectively, and export them
through `testids.ts` here. The stubbed mission's slot is committed, which is why the
verb reads `Amend`, not `Send back`.

- [ ] **Step 3: Run the spec**

```bash
cd uitests && npx playwright test comment-margin --reporter=line
```

Expected: PASS, four tests.

- [ ] **Step 4: Run the design-experience regression specs**

```bash
cd uitests && npx playwright test design-experience glossary volatility-map --reporter=line
```

Expected: PASS. Failures naming `chat-rail` are stale selectors — update them to
the `Margin` ids rather than restoring the rail.

- [ ] **Step 5: Commit**

```bash
git add uitests
git commit -m "test(uitests): comment margin acceptance

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

## Founder review gate

Stop here. Rebuild and restart the local server (Go changed, so `go run`/rebuild is
required), reload the Mission page against real state, and walk the founder
through: the recovered header space, a margin card beside Objective 3, click-to-jump,
a staged reply, and the submit bar's consequence line.

Stage 2 — canvas anchors, the Phase-2 and Construction sweep, and the §7
design-state amendments — is planned only after that review.
