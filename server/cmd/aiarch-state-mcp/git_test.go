package main

import (
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// TestPublishDraft_ExactlyOnce proves a second publish is a no-op (no second commit).
func TestPublishDraft_ExactlyOnce(t *testing.T) {
	s, fg := seedProject(t, minimalProject(), jobModeDraft, projectstate.KindVolatilities)
	s.wroteState = true // a draft was recorded this session
	fg.porcelain = " M .aiarch/state/project.json"

	if _, err := s.publishDraft("first"); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	if !fg.didCall("commit") || !fg.didCall("push") {
		t.Fatalf("first publish did not commit+push: %v", fg.calls)
	}
	before := len(fg.calls)
	msg, err := s.publishDraft("second")
	if err != nil {
		t.Fatalf("second publish: %v", err)
	}
	if !strings.Contains(msg, "Already published") {
		t.Fatalf("second publish not a clear no-op: %q", msg)
	}
	if len(fg.calls) != before {
		t.Fatalf("second publish invoked git again: %v", fg.calls[before:])
	}
}

// TestPublishDraft_RefusesEmpty proves the no-empty-publish guard: nothing drafted and a
// clean tree => refuse (the F17c "green job, nothing committed" killer).
func TestPublishDraft_RefusesEmpty(t *testing.T) {
	s, fg := seedProject(t, minimalProject(), jobModeDraft, projectstate.KindVolatilities)
	fg.porcelain = "" // clean tree
	_, err := s.publishDraft("nothing")
	if err == nil {
		t.Fatalf("expected refusal when nothing was drafted and the tree is clean")
	}
	if !strings.Contains(err.Error(), "no draft was recorded") {
		t.Fatalf("refusal message not actionable: %v", err)
	}
	if fg.didCall("commit") {
		t.Fatalf("committed despite the empty guard")
	}
}

// TestPublishDraft_CritiqueEmptyMessage tailors the refusal to critique mode.
func TestPublishDraft_CritiqueEmptyMessage(t *testing.T) {
	s, fg := seedProject(t, minimalProject(), jobModeCritique, projectstate.KindMission)
	fg.porcelain = ""
	_, err := s.publishDraft("x")
	if err == nil || !strings.Contains(err.Error(), "critique verdict") {
		t.Fatalf("expected critique-tailored refusal, got %v", err)
	}
}

// TestPublishDraft_DirtyTreeWithoutFlag publishes when the tree is dirty even if no verb
// ran this process (e.g. a prior process wrote but did not publish).
func TestPublishDraft_DirtyTreeWithoutFlag(t *testing.T) {
	s, fg := seedProject(t, minimalProject(), jobModeDraft, projectstate.KindVolatilities)
	s.wroteState = false
	fg.porcelain = " M .aiarch/state/project.json"
	if _, err := s.publishDraft("carry forward"); err != nil {
		t.Fatalf("publish with dirty tree: %v", err)
	}
	if !fg.didCall("commit") {
		t.Fatalf("did not commit a dirty tree")
	}
}

// TestPublishDraft_NoNetChange publishes an EMPTY re-affirm commit when the add stages
// nothing, so the pipeline's branch-advanced guard records the convergence instead of
// failing a green job (F-QA2-29). The commit must carry --allow-empty and the message
// must say the draft was re-affirmed.
func TestPublishDraft_NoNetChange(t *testing.T) {
	s, fg := seedProject(t, minimalProject(), jobModeDraft, projectstate.KindVolatilities)
	s.wroteState = true
	fg.porcelain = "" // add stages nothing
	msg, err := s.publishDraft("noop")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !strings.Contains(msg, "Re-affirmed") {
		t.Fatalf("expected re-affirm message, got %q", msg)
	}
	if !fg.didCall("commit") {
		t.Fatalf("no re-affirm commit was made on a no-net-change publish")
	}
	if !fg.didCallWith("commit", "--allow-empty") {
		t.Fatalf("re-affirm commit was not --allow-empty")
	}
	if !fg.didCall("push") {
		t.Fatalf("re-affirm commit was not pushed")
	}
}

// THE LOCAL-VENUE REGRESSION. With no `origin` remote configured, the local commit IS
// the publication and publishDraft must SUCCEED. Before this, the push ran regardless and
// git's "fatal: 'origin' does not appear to be a git repository" came back as a hard tool
// error even though the commit had landed — 19 of 30 publishDraft calls on one measured
// run, ~309s of retries, and two episodes that converged only because the agent invented a
// workaround around a verb that had actually worked.
func TestPublishDraft_NoOriginRemote_LocalCommitIsSuccess(t *testing.T) {
	s, fg := seedProject(t, minimalProject(), jobModeDraft, projectstate.KindVolatilities)
	s.wroteState = true
	fg.porcelain = " M .aiarch/state/project.json"
	fg.noOrigin = true

	msg, err := s.publishDraft("local venue")
	if err != nil {
		t.Fatalf("a publish with no origin remote must succeed on the local commit alone, got: %v", err)
	}
	if !fg.didCall("commit") {
		t.Fatalf("the local commit was not made: %v", fg.calls)
	}
	if fg.didCall("push") {
		t.Fatalf("pushed despite there being no origin remote: %v", fg.calls)
	}
	if !s.published {
		t.Fatal("a successful local-only publish must latch the exactly-once flag")
	}
	if !strings.Contains(msg, "local only") {
		t.Fatalf("the result must say the commit was not pushed, got %q", msg)
	}
}

// The real failure path is UNTOUCHED: an origin that EXISTS and fails to push is still a
// hard error. Nothing about the local-venue carve-out may soften a genuine publication
// failure.
func TestPublishDraft_OriginPresentButPushFails_StillHardError(t *testing.T) {
	s, fg := seedProject(t, minimalProject(), jobModeDraft, projectstate.KindVolatilities)
	s.wroteState = true
	fg.porcelain = " M .aiarch/state/project.json"
	fg.failOn = "push"

	if _, err := s.publishDraft("remote venue"); err == nil {
		t.Fatal("a failed push against a configured origin must remain a hard error")
	}
	if s.published {
		t.Fatal("a failed push must NOT latch the exactly-once flag - the retry has to be able to push again")
	}
}

// A checkout whose `git remote` cannot even be listed must NOT be read as "no origin":
// that would convert a genuine push failure into a quiet local-only publish. The push is
// attempted and whatever git says is surfaced.
func TestPublishDraft_RemoteListingFails_StillAttemptsThePush(t *testing.T) {
	s, fg := seedProject(t, minimalProject(), jobModeDraft, projectstate.KindVolatilities)
	s.wroteState = true
	fg.porcelain = " M .aiarch/state/project.json"
	fg.failOn = "remote"

	if _, err := s.publishDraft("unknown remotes"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !fg.didCall("push") {
		t.Fatalf("an unlistable remote set must fall back to attempting the push: %v", fg.calls)
	}
}
