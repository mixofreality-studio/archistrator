package main

// stepscoping_test.go — the STEP MANIFEST tool gate (buildServer + grantedTools).
//
// Before scoping, every job mode registered every read-only tool in the
// catalog, so a Mission draft carried billing, autoscaler, operated-runtime and
// revenue-ledger tools it can neither use nor legally call. These tests pin the
// three properties that make the gate safe:
//
//  1. a bound step registers ONLY what its manifest grants;
//  2. an unbound session keeps the FULL legacy surface (no silent regression
//     for hand-run sessions or a dispatch not yet stamping AIARCH_COMMAND);
//  3. the manifest NARROWS ONLY — it can never re-admit a verb the job mode
//     already forbids, so the mode gate remains the authorization boundary.

import (
	"strings"
	"testing"

	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// registeredMCPToolNames returns the tool names buildServer registers for s.
func registeredMCPToolNames(t *testing.T, s *Session) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	// Composed verbs: the same mode+grant predicate buildServer applies.
	granted := grantedTools(s)
	for _, v := range composedVerbs(s) {
		if containsStr(v.modes, s.Mode) && granted.allows(v.name) {
			names[v.name] = true
		}
	}
	for _, tool := range projectstate.InternalToolCatalog() {
		if rawToolEligible(tool) && granted.allows(tool.Name) {
			names[tool.Name] = true
		}
	}
	return names
}

// TestStepManifestNarrowsTheToolSurface: a bound Mission draft registers only
// its manifest's tools, and in particular none of the project-design,
// billing or operations surface.
func TestStepManifestNarrowsTheToolSurface(t *testing.T) {
	s, _ := seedProject(t, minimalProject(), jobModeDraft, projectstate.KindMission)
	s.Command = "mission-draft"

	m, ok := methodassets.ManifestFor("mission-draft")
	if !ok {
		t.Fatal("mission-draft has no step manifest")
	}
	allowed := map[string]bool{}
	for _, n := range m.MCPTools() {
		allowed[n] = true
	}

	got := registeredMCPToolNames(t, s)
	if len(got) == 0 {
		t.Fatal("a bound session registered no tools at all")
	}
	for name := range got {
		if !allowed[name] {
			t.Errorf("mission-draft registered %q, which its manifest does not grant", name)
		}
	}

	// The headline case: a system-design step must not carry the project-design
	// estimation surface, nor the operations/billing surface.
	for _, forbidden := range []string{
		"estimationDerivePlan", "estimationComputeNetwork", "estimationEstimateForOption",
		"billingComputeNet", "revenueLedgerReadRange", "autoscalerProposeDesiredState",
		"operatedRuntimeGetSloStatus", "sourceControlGetInstallationToken",
	} {
		if got[forbidden] {
			t.Errorf("mission-draft still registers %q", forbidden)
		}
	}

	// It must still hold what it actually needs.
	for _, needed := range []string{"getDraftSlot", "publishDraft", "putDraftModel", "listResearchSources"} {
		if !got[needed] {
			t.Errorf("mission-draft lost %q, which the step requires", needed)
		}
	}
}

// TestUnboundSessionKeepsFullSurface: no AIARCH_COMMAND (or an unknown one)
// means no manifest, and the legacy mode-gated surface is preserved intact.
func TestUnboundSessionKeepsFullSurface(t *testing.T) {
	base, _ := seedProject(t, minimalProject(), jobModeDraft, projectstate.KindMission)
	full := registeredMCPToolNames(t, base)

	for _, cmdName := range []string{"", "not-a-real-command"} {
		s, _ := seedProject(t, minimalProject(), jobModeDraft, projectstate.KindMission)
		s.Command = cmdName
		got := registeredMCPToolNames(t, s)
		if len(got) != len(full) {
			t.Errorf("command %q: registered %d tools, want the full legacy surface of %d", cmdName, len(got), len(full))
		}
	}

	// Sanity: the full surface really is much wider than a scoped step, or this
	// test would pass vacuously.
	scoped, _ := seedProject(t, minimalProject(), jobModeDraft, projectstate.KindMission)
	scoped.Command = "mission-draft"
	if n := len(registeredMCPToolNames(t, scoped)); n >= len(full) {
		t.Errorf("scoping did not narrow anything: scoped=%d full=%d", n, len(full))
	}
}

// TestManifestCannotWidenBeyondTheModeGate: the manifest is applied ON TOP of
// the mode gate, so a step whose manifest happens to name a verb its mode does
// not serve still does not get it. putDraftModel is draft-only; a critique step
// must never register it however the manifest is written.
func TestManifestCannotWidenBeyondTheModeGate(t *testing.T) {
	s, _ := seedProject(t, minimalProject(), jobModeCritique, projectstate.KindMission)
	s.Command = "mission-critique"
	got := registeredMCPToolNames(t, s)

	if got["putDraftModel"] {
		t.Error("critique mode registered putDraftModel — the mode gate was bypassed")
	}
	if !got["setCritiqueVerdict"] {
		t.Error("critique mode lost setCritiqueVerdict")
	}
}

// TestEveryManifestToolIsRealAndModeLegal walks every step manifest and asserts
// each MCP tool it grants is either a real composed verb legal in that step's
// mode, or a real eligible raw tool. A manifest naming a tool that does not
// exist would silently under-register that step at dispatch time.
func TestEveryManifestToolIsRealAndModeLegal(t *testing.T) {
	rawNames := map[string]bool{}
	for _, tool := range projectstate.InternalToolCatalog() {
		if rawToolEligible(tool) {
			rawNames[tool.Name] = true
		}
	}
	probe := &Session{Mode: jobModeDraft}
	verbModes := map[string][]string{}
	for _, v := range composedVerbs(probe) {
		verbModes[v.name] = v.modes
	}

	for slug, m := range methodassets.Manifests() {
		for _, name := range m.MCPTools() {
			modes, isComposed := verbModes[name]
			switch {
			case isComposed:
				if !containsStr(modes, m.Mode) {
					t.Errorf("%s (mode %s): manifest grants composed verb %q, which that mode does not serve", slug, m.Mode, name)
				}
			case rawNames[name]:
				// a real, eligible raw read tool — always mode-legal
			default:
				t.Errorf("%s: manifest grants %q, which is neither a composed verb nor an eligible raw tool", slug, name)
			}
		}
		if strings.TrimSpace(m.Agent) == "" {
			t.Errorf("%s: manifest names no agent", slug)
		}
	}
}
