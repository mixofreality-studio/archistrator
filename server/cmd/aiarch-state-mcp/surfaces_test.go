package main

// surfaces_test.go — the delivery-surface design rules at the in-loop gate (FPC Plan 5, Task 5.2):
// SYS-SURFACE-REQUIRED, SYS-SURFACE-ENTRY and GEN-SURFACE-SUPPORTED refuse a System that lacks a
// surface the mission committed to, and the app attributes them to the System slot.

import (
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

func surfaceMissionProject() projectstate.Project {
	p := deadlockFixtureProject(true)
	p.Mission = projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.MissionStatement{
			Vision:     "A platform for capturing and clarifying commitments.",
			Objectives: []projectstate.Objective{{Number: 1, Statement: "Quick turnaround of new workflows."}},
			Mission:    "Design and build components the team assembles into GTD applications.",
			DeliverySurfaces: []projectstate.DeliverySurface{
				{Kind: projectstate.SurfaceWeb, Audience: projectstate.AudienceHuman, Source: "an ordinary web app", Objective: 1},
				{Kind: projectstate.SurfaceREST, Audience: projectstate.AudienceMachine, Source: "reachable both as an ordinary web API", Objective: 1},
			},
		},
	}
	return p
}

const surfaceDraftTail = `
		{"id":"m1","name":"GtdManager","kind":"manager","layer":"manager","encapsulates":"workflow volatility"},
		{"id":"ra1","name":"TaskAccess","kind":"resourceAccess","layer":"resourceAccess","encapsulates":"task storage volatility","atomicBusinessVerbs":["capture"]},
		{"id":"ra2","name":"AgentAccess","kind":"resourceAccess","layer":"resourceAccess","encapsulates":"agent substrate volatility","atomicBusinessVerbs":["delegate"]},
		{"id":"r1","name":"TaskDB","kind":"resource","layer":"resource","encapsulates":"durable task store"}`

const surfaceDraftRels = `
		{"from":"m1","to":"ra1","mode":"sync","label":"store"},
		{"from":"m1","to":"ra2","mode":"sync","label":"delegate"},
		{"from":"ra1","to":"r1","mode":"sync","label":"persist"},
		{"from":"ra2","to":"r1","mode":"sync","label":"persist"}`

func TestPutDraftModel_RefusesSystemMissingRequiredSurface(t *testing.T) {
	web := `{"id":"c1","name":"WebClient","kind":"client","layer":"client","surface":"web","encapsulates":"presentation volatility"},`
	missing := []byte(`{"components":[` + web + surfaceDraftTail + `],
		"relationships":[{"from":"c1","to":"m1","mode":"sync","label":"capture"},` + surfaceDraftRels + `],
		"dynamicViews":[]}`)
	s, _ := seedProject(t, surfaceMissionProject(), jobModeDraft, projectstate.KindSystem)
	err := s.putDraftModel(missing)
	if err == nil {
		t.Fatal("a System without a rest Client must be refused when the mission commits to rest")
	}
	for _, want := range []string{"SYS-SURFACE-REQUIRED", "rest"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}

	api := `{"id":"api","name":"TodoApi","kind":"client","layer":"client","surface":"rest","encapsulates":"machine API volatility"},`
	whole := []byte(`{"components":[` + web + api + surfaceDraftTail + `],
		"relationships":[{"from":"c1","to":"m1","mode":"sync","label":"capture"},{"from":"api","to":"m1","mode":"sync","label":"capture"},` + surfaceDraftRels + `],
		"dynamicViews":[]}`)
	s2, _ := seedProject(t, surfaceMissionProject(), jobModeDraft, projectstate.KindSystem)
	if err := s2.putDraftModel(whole); err != nil {
		t.Fatalf("a System with a Client for each committed surface must be accepted: %v", err)
	}
}

func TestAttributeRule_SurfaceRulesBelongToTheSystem(t *testing.T) {
	for _, id := range []string{"GEN-SURFACE-SUPPORTED", "SYS-SURFACE-ENTRY", "SYS-SURFACE-REQUIRED"} {
		kind, class := projectstate.AttributeRule(id)
		if kind != projectstate.KindSystem || class != projectstate.AttribSlot {
			t.Errorf("AttributeRule(%s) = (%v, %v), want (KindSystem, AttribSlot)", id, kind, class)
		}
	}
}
