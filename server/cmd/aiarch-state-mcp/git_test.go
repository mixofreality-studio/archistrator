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
