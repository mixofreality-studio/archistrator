package main

// staleness.go — the STALENESS-AWARE severity policy for CROSS-ARTIFACT Method rules
// (founder-ratified 2026-07-06; the cross-artifact validation deadlock fix).
//
// THE DEADLOCK: the System slot carries components/relationships/dynamicViews only; the
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
// This policy is applied at EVERY app-side enforcement point of the methodcheck rules —
// putDraftModel's in-loop gate, applyConstructionMutation's gate, and the `validate`
// one-shot subcommand the seated design workflow runs as the REQUIRED PR check — so the
// in-loop and CI verdicts can never disagree about staleness.

import (
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

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
