package main

// staleness.go — the GATE SEVERITY POLICY seam: the two founder-ratified downgrades
// (2026-07-06) that keep the single-writer-per-slot design rail from deadlocking on
// whole-document validation. applyGateSeverityPolicies composes them:
//
//  1. STALENESS-AWARE severity for the CROSS-ARTIFACT System×OperationalConcepts join
//     rules (applyStaleBasisDowngrades — the first half of the deadlock class; below)
//     and, by the same token, for the app-side System×ActivityList join rules
//     (applyActivityListStaleDowngrades over the ACT-* rules of crossartifact.go,
//     which applyGateSeverityPolicies appends before any downgrade runs).
//  2. SLOT-SCOPED severity (applySlotScopeDowngrades — the second half): findings
//     attributable to a specific artifact slot are Errors ONLY when that slot is the
//     one the session is amending (the AMBIENT slot). Findings on OTHER slots are
//     pre-existing committed data this session cannot write — GRANDFATHERED defects
//     (committed under an older gate generation) would otherwise keep every sibling
//     amendment PR red, pairwise-deadlocking amendments on newly-strengthened rules
//     (observed live on gtdapp 5-amend-2: 24 UC-VARIATION-REF errors in the committed
//     CoreUseCases slot blocked the System amendment, which cannot write slot 4 — and
//     a slot-4 session could not fix slot-5 defects either).
//
// Structural failures are NEVER downgraded: decode failures and RequireModelFields
// reject before findings exist, a rule id with no slot attribution keeps full severity
// by construction, and every rule ATTRIBUTED TO THE AMBIENT SLOT keeps full severity —
// including the ambient slot's name-resolution integrity INTO other slots (those rules
// are attributed to the referencing slot, e.g. USECASE-DYNAMIC-MISSING, the DV-*
// family, and the CC-* call-chain family are System-attributed even though they read
// CoreUseCases).
//
// THE ORIGINAL (STALENESS) DEADLOCK: the System slot carries components/relationships/dynamicViews only; the
// deployment topology (containers + environments) is carried by OperationalConcepts. The
// DEP-* deployment-consistency rules JOIN the two artifacts (every environment must
// instance every container-eligible System component). But the design rail amends ONE
// slot per session — a System amendment that ADDS components can never satisfy
// DEP-COVERAGE in the same draft (the deployment update is a LATER OperationalConcepts
// amendment), and that later amendment could not run first either (it would reference
// components that do not exist yet). Observed live on gtdapp 2026-07-06: the amendment
// adding the Webapp/MCP/Agent Clients + Agent Policy Manager hard-failed the Method gate.
//
// THE FIX: make the gate agree with the rail's OWN staleness semantics. Committing an
// upstream amendment flags every already-committed downstream slot StaleBasis=true
// (projectstate commitTransition, F38) — i.e. the cascade fires exactly when downstream
// reconciliation becomes pending. While the joined counterpart slot is flagged stale,
// the cross-artifact rules that READ it are advisory, not blocking: their Error findings
// downgrade to Warning. Sealing the phase with an unreconciled slot stays impossible via
// the STALE-UNACKED AdvancePhase gate (a19a25b) — the downgrade only unblocks the
// amendment TRAFFIC, never the phase seal. Single-artifact and same-artifact rules keep
// full severity: a draft must always be internally coherent.
//
// These policies are applied at EVERY app-side enforcement point of the methodcheck
// rules — putDraftModel's in-loop gate (which knows its ambient slot from the job env),
// applyConstructionMutation's gate (whole-document: a construct session has no design
// ambient slot), and the `validate` one-shot subcommand the seated design workflow runs
// as the REQUIRED PR check (`--slot` threads the job's ambient artifact; absent flag =
// whole-document) — so the in-loop and CI verdicts can never disagree.

import (
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// applyGateSeverityPolicies is the single composition every enforcement point calls.
// It FIRST appends the app-side cross-artifact rules (crossartifact.go — this is the
// one seam every enforcement point already routes through, so appending here is what
// keeps the in-loop and CI verdicts identical for the app-side rules too), then the
// staleness downgrades (their annotation is the more specific), then — when the caller
// has an ambient artifact slot (a design session / a --slot CI run) — the slot-scoped
// downgrade as the general fallback beneath them. hasAmbient=false is the
// whole-document mode: rules + staleness only, exactly the pre-slot-scoping behavior.
func applyGateSeverityPolicies(proj projectstate.Project, ambient projectstate.ArtifactKind, hasAmbient bool, findings []methodcheck.Finding) []methodcheck.Finding {
	findings = appendAppSideCrossArtifactFindings(proj, findings)
	findings = applyStaleBasisDowngrades(proj, findings)
	findings = applyActivityListStaleDowngrades(proj, findings)
	if hasAmbient {
		findings = applySlotScopeDowngrades(ambient, findings)
	}
	return findings
}

// systemOpConceptsJoinRules is the closed set of methodcheck rules that JOIN
// System × OperationalConcepts — the deployment-consistency predicates whose subject is
// the System component graph matched against the OperationalConcepts deployment
// topology (framework-go/methodcheck/rules_deployment.go, deploymentConsistency):
//
//   - DEP-COVERAGE        (Error)   every required environment instances every
//     container-eligible System component
//   - DEP-GRAPH-IDENTITY  (Error)   the deployed component set is identical across
//     profiles / test is a superset of the System-internal set
//   - DEP-MEMBER-EXIST    (Error)   every container member names a System component
//     (fires on the rename/removal direction of the same join)
//   - DEP-RESOURCE-PRESENT (already Warning) every System Resource appears as
//     deployment infrastructure per required profile
//   - DEP-PLANNED-SKIPPED (already Info) the planned-component coverage exemption
//
// The last two never carry Error severity today; they are listed so the set documents
// the FULL System×OperationalConcepts join surface and stays correct if their severity
// ever changes upstream.
//
// Deliberately NOT in the set (same-artifact rules, internal to the OperationalConcepts
// topology, which must hold in every draft regardless of staleness): DEP-CONTAINER-REF,
// DEP-CONTAINER-USED, DEP-MEMBER-EXCLUSIVE, DEP-PROFILE-SET, DEP-NODE-WELLFORMED.
var systemOpConceptsJoinRules = map[methodcheck.RuleID]bool{
	"DEP-COVERAGE":         true,
	"DEP-GRAPH-IDENTITY":   true,
	"DEP-MEMBER-EXIST":     true,
	"DEP-RESOURCE-PRESENT": true,
	"DEP-PLANNED-SKIPPED":  true,
}

// staleDowngradeNote is appended to every downgraded finding so the gate log states
// WHY the rule did not block and WHEN it will again.
const staleDowngradeNote = " [downgraded to warning: the operationalConcepts slot is flagged stale-basis, so its reconciliation with the System is pending by design; this System×OperationalConcepts rule regains Error severity once the slot is re-committed or its staleness acknowledged]"

// applyStaleBasisDowngrades applies the staleness-aware severity policy over a
// methodcheck finding set: when the OperationalConcepts slot of proj carries
// StaleBasis=true, every Error finding of a System×OperationalConcepts join rule is
// downgraded to Warning (annotated with the reason). All other findings — every
// single-artifact rule, every same-artifact deployment rule, and everything when the
// slot is NOT stale — pass through untouched.
func applyStaleBasisDowngrades(proj projectstate.Project, findings []methodcheck.Finding) []methodcheck.Finding {
	if !proj.OperationalConcepts.StaleBasis {
		return findings
	}
	out := make([]methodcheck.Finding, len(findings))
	for i, f := range findings {
		if f.Severity == methodcheck.SeverityError && systemOpConceptsJoinRules[f.RuleID] {
			f.Severity = methodcheck.SeverityWarning
			f.Message += staleDowngradeNote
		}
		out[i] = f
	}
	return out
}

// systemActivityListJoinRules is the closed set of rules that JOIN System ×
// ActivityList — the app-side coding-activity-coverage predicates (crossartifact.go)
// whose subject is the committed activity list matched against the System component
// graph. The EXACT mirror of systemOpConceptsJoinRules for the Phase-1→Phase-2 join:
// a System amendment that adds or renames components can never satisfy
// ACT-COMPONENT-COVERAGE in the same session (the activity-list update is a LATER
// ActivityList amendment), so while the activityList slot is flagged StaleBasis the
// join is advisory, not blocking.
//
//   - ACT-COMPONENT-COVERAGE (Error)   every code-layer System component has a
//     coding activity
//   - ACT-UNKNOWN-COMPONENT  (already Warning) a coding activity derives no System
//     component; listed so the set documents the FULL System×ActivityList join
//     surface and stays correct if its severity ever changes.
//
// Deliberately NOT in the set: PA-RATECARD-* (same-artifact rules internal to
// PlanningAssumptions, which must hold in every draft regardless of staleness).
var systemActivityListJoinRules = map[methodcheck.RuleID]bool{
	"ACT-COMPONENT-COVERAGE": true,
	"ACT-UNKNOWN-COMPONENT":  true,
}

// activityListStaleDowngradeNote is appended to every downgraded finding so the gate
// log states WHY the rule did not block and WHEN it will again.
const activityListStaleDowngradeNote = " [downgraded to warning: the activityList slot is flagged stale-basis, so its reconciliation with the System is pending by design; this System×ActivityList rule regains Error severity once the slot is re-committed or its staleness acknowledged]"

// applyActivityListStaleDowngrades applies the staleness-aware severity policy for the
// System×ActivityList join rules — the exact mirror of applyStaleBasisDowngrades over
// the ActivityList slot: when that slot carries StaleBasis=true, every Error finding
// of a System×ActivityList join rule downgrades to an annotated Warning. Everything
// else passes through untouched.
func applyActivityListStaleDowngrades(proj projectstate.Project, findings []methodcheck.Finding) []methodcheck.Finding {
	if !proj.ActivityList.StaleBasis {
		return findings
	}
	out := make([]methodcheck.Finding, len(findings))
	for i, f := range findings {
		if f.Severity == methodcheck.SeverityError && systemActivityListJoinRules[f.RuleID] {
			f.Severity = methodcheck.SeverityWarning
			f.Message += activityListStaleDowngradeNote
		}
		out[i] = f
	}
	return out
}

// ---------------------------------------------------------------------------------
// SLOT-SCOPED severity (policy 2) — the general fallback beneath the staleness case.
// ---------------------------------------------------------------------------------

// ruleAttribution classifies where a methodcheck rule's findings are FIXED.
type ruleAttribution int

const (
	// attribNone — structural / whole-document / unknown rule id. NEVER downgraded:
	// a rule this table does not know is treated at full severity by construction.
	attribNone ruleAttribution = iota
	// attribSlot — the finding is owned by one Phase-1/2 artifact slot (the kind
	// returned alongside): only that slot's own amendment can fix it.
	attribSlot
	// attribTesting — the finding is owned by .testingState (the STP-* System-Test-Plan
	// family), written by Phase-3 construction jobs, never by a design-slot session. For
	// any design ambient slot it is therefore always an OTHER-slot finding.
	attribTesting
)

// ruleSlotAttributionPrefixes maps a RuleID prefix to the artifact slot whose own
// amendment fixes the finding. Attribution follows the rule's SUBJECT (the slot whose
// content must change), which is also the slot the emitting validator is registered
// for — so a rule that READS other slots still attributes to the referencing slot
// (VOL-GLOSS→Volatilities, USECASE-DYNAMIC-MISSING/DV-*→System, DEP-*→
// OperationalConcepts). Prefixes are disjoint (each ends in a dash; SYSTEM-/USECASE-
// cannot collide with SYS-/UC- because the dash position differs). Rule families with
// no entry (ALIGN-*, CODE-/MODEL-EDGE — the code-walk rules ValidateProjectJSON never
// emits) fall through to attribNone and keep full severity.
var ruleSlotAttributionPrefixes = []struct {
	prefix string
	kind   projectstate.ArtifactKind
	class  ruleAttribution
}{
	{"GLOSS-", projectstate.KindGlossary, attribSlot},
	{"SR-", projectstate.KindScrubbedRequirements, attribSlot},
	{"VOL-", projectstate.KindVolatilities, attribSlot},
	{"CUC-", projectstate.KindCoreUseCases, attribSlot},
	{"UC-", projectstate.KindCoreUseCases, attribSlot},
	{"USECASE-", projectstate.KindSystem, attribSlot},
	{"SYSTEM-", projectstate.KindSystem, attribSlot},
	{"SYS-", projectstate.KindSystem, attribSlot},
	{"DV-", projectstate.KindSystem, attribSlot},
	{"CC-", projectstate.KindSystem, attribSlot},
	{"ARCH-", projectstate.KindSystem, attribSlot},
	{"APPC-", projectstate.KindSystem, attribSlot},
	{"OPC-", projectstate.KindOperationalConcepts, attribSlot},
	{"DEP-", projectstate.KindOperationalConcepts, attribSlot},
	{"STD-", projectstate.KindStandardCheck, attribSlot},
	{"STP-", 0, attribTesting},
	// App-side cross-artifact rules (crossartifact.go). Attribution follows the
	// subject slot exactly like DEP-*: the ACT-* System×ActivityList join is fixed by
	// an ActivityList amendment; the PA-RATECARD-* consistency is fixed by a
	// PlanningAssumptions amendment.
	{"ACT-", projectstate.KindActivityList, attribSlot},
	{"PA-", projectstate.KindPlanningAssumptions, attribSlot},
	// Live design-health tier (internal/engine/designhealth). The DH-* rules are the
	// mechanical systemDesign directives + joins; their subject is the architecture,
	// so a DH-* Error is fixed by a systemDesign amendment. Attributing the whole
	// family to the System slot keeps a DH-* Error from deadlocking a session amending
	// a different slot (the same posture as the DV-*/USECASE-* System-attributed rules
	// that also read sibling slots). This one prefix is ALSO how the
	// Volatilities↔System (slot3↔slot5) join rules — DH-VOL-ENCAP-MISSING, DH-VOL-TRACE,
	// and DH-COMP-VOL-DANGLING (the component-side typed encapsulatesVolatilities
	// dangling-reference direction of the same join) — avoid the amendment deadlock: a
	// Volatilities amendment that renames/removes a volatility sees them downgrade as
	// other-slot findings, while the System amendment that CAN fix the typed lists
	// keeps them at full severity. Finer sub-attribution (DH-OBJ-* →
	// businessAlignment) is a later refinement; System is the dominant owner and the
	// authoring-gate slot these run under.
	{"DH-", projectstate.KindSystem, attribSlot},
}

// attributeRule resolves a rule id to its owning slot (attribSlot + kind), the testing
// state (attribTesting), or no attribution (attribNone — full severity always).
func attributeRule(id methodcheck.RuleID) (projectstate.ArtifactKind, ruleAttribution) {
	for _, e := range ruleSlotAttributionPrefixes {
		if strings.HasPrefix(string(id), e.prefix) {
			return e.kind, e.class
		}
	}
	return 0, attribNone
}

// applySlotScopeDowngrades applies the slot-scoped severity policy for a session whose
// AMBIENT slot is ambient: an Error finding attributed to a DIFFERENT artifact slot (or
// to the construction-owned testing state) is pre-existing committed data this session
// cannot write — downgraded to Warning with the owning slot named, so the reviewer sees
// exactly which slot's own amendment must fix it. Ambient-slot findings and
// unattributed findings keep full severity.
func applySlotScopeDowngrades(ambient projectstate.ArtifactKind, findings []methodcheck.Finding) []methodcheck.Finding {
	out := make([]methodcheck.Finding, len(findings))
	for i, f := range findings {
		out[i] = f
		if f.Severity != methodcheck.SeverityError {
			continue
		}
		kind, class := attributeRule(f.RuleID)
		switch class {
		case attribSlot:
			if kind != ambient {
				out[i].Severity = methodcheck.SeverityWarning
				out[i].Message += slotScopeDowngradeNote(kind.WireName())
			}
		case attribTesting:
			out[i].Severity = methodcheck.SeverityWarning
			out[i].Message += slotScopeDowngradeNote("testingState")
		case attribNone:
			// structural / unknown — full severity.
		}
	}
	return out
}

// slotScopeDowngradeNote names the slot that owns fixing a downgraded finding.
func slotScopeDowngradeNote(owner string) string {
	return " [downgraded to warning: pre-existing on the " + owner + " slot, which this session cannot write — fix via that slot's own amendment]"
}
