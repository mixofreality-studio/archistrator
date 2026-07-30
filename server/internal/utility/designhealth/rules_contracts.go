package designhealth

import (
	"fmt"
	"sort"
	"strings"

	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// rules_contracts.go — the service-contract App-C and facet rules over the
// projectmodel.Contracts slice.
//
//	DH-CONTRACT-OPCOUNT-REJECT (Error)   a contract with ≥20 operations — App-C's
//	                                     hard reject (a god interface).
//	DH-CONTRACT-OPCOUNT-MAX    (Warning) a contract past the max-12 guideline.
//	DH-CONTRACT-FACET          (Error)   the ratified facet-doctrine join (the
//	                                     family-D gate promoted to a live finding):
//	                                     a contract key that names no component is
//	                                     valid only as a FACET whose `component`
//	                                     field resolves to a component of the same
//	                                     layer; otherwise it is a fossil.
//	DH-CONTRACT-DEADOP         (Warning) the same operation name published by two
//	                                     contracts of one facet group (the D1-D4
//	                                     duplicate class).
func contractFindings(in Input) []methodcheck.Finding {
	if in.Model == nil {
		return nil
	}
	var out []methodcheck.Finding
	out = append(out, opCountFindings(in.Model)...)
	out = append(out, facetJoinFindings(in.Model)...)
	out = append(out, deadOpFindings(in.Model)...)
	return out
}

// opCountFindings applies the App-C operation-count metric per contract.
func opCountFindings(model *projectmodel.Model) []methodcheck.Finding {
	var out []methodcheck.Finding
	for i, key := range sortedContractKeys(model) {
		c := model.Contracts[key]
		if c.Doc == nil {
			continue
		}
		n := len(c.Doc.Interface.Operations)
		switch {
		case n >= 20:
			out = append(out, finding(RuleContractOpReject, methodcheck.SeverityError, i, "contract "+key,
				fmt.Sprintf("contract %q has %d operations — App-C rejects a contract at ≥20 ops (a god interface); split it by cohesion", key, n)))
		case n > 12:
			out = append(out, finding(RuleContractOpMax, methodcheck.SeverityWarning, i, "contract "+key,
				fmt.Sprintf("contract %q has %d operations — past the App-C max of 12 (sweet spot 3–5); factor down or sideways", key, n)))
		}
	}
	return out
}

// facetJoinFindings promotes the ratified family-D facet doctrine to a live
// finding. A contract whose key does not join a component directly is valid only
// as a FACET: its `component` field must resolve to a component, and the facet must
// declare that component's layer. A contract that resolves to nothing is a fossil.
func facetJoinFindings(model *projectmodel.Model) []methodcheck.Finding {
	if model.System == nil {
		return nil
	}
	var out []methodcheck.Finding
	for i, key := range sortedContractKeys(model) {
		c := model.Contracts[key]
		if _, ok := model.System.ComponentByContractKey(key); ok {
			continue // direct component contract
		}
		owner, ok := model.System.ComponentByContractKey(c.Component)
		if !ok {
			out = append(out, finding(RuleContractFacet, methodcheck.SeverityError, i, "contract "+key,
				fmt.Sprintf("service contract %q resolves to no component: its key is not a component contract key and its component field %q names no component — a stale/fossil entry", key, c.Component)))
			continue
		}
		if !strings.EqualFold(c.Layer, owner.Layer) {
			out = append(out, finding(RuleContractFacet, methodcheck.SeverityError, i, "contract "+key,
				fmt.Sprintf("contract facet %q declares layer %q but its owning component %q is layer %q — a facet must share its owner's layer", key, c.Layer, owner.ID, owner.Layer)))
		}
	}
	return out
}

// deadOpFindings detects the D1-D4 duplicate class: one operation name published by
// two DIFFERENT contracts that share an owning `component` (a facet group). The
// facets share one Go method set, so a duplicated op is worth review. Severity is
// signature-sensitive (architect ruling 2026-07-22):
//
//   - same NAME, DIFFERING param signature → SeverityWarning. This is a name
//     COLLISION, not proof of a dead op: a distinct-signature variant (e.g. a
//     cred-threaded git-substrate read alongside the plain read) can legitimately
//     coexist, and deciding one is redundant needs LIVENESS evidence (call sites),
//     not the name alone. (The reconciled state's ReadProject case — base {projectID}
//     vs facet {projectID, cred} — was exactly this: prunable only after verifying
//     both hand call sites passed an empty credential and no invoker needed the facet
//     read. The rule flags it for that judgment; it does not block on it.)
//   - same NAME, SAME param signature → SeverityError. An exact duplicate carries no
//     signature difference to justify coexistence; the facets share one method set,
//     so it is unambiguous documentation drift to collapse to one facet.
//
// (The tool-naming ruling prefixes generated tool names by contract key, so a
// same-name facet op no longer collides at the MCP surface; this rule governs the
// CONTRACT corpus, where the shared method set still makes an exact dup drift.)
func deadOpFindings(model *projectmodel.Model) []methodcheck.Finding {
	// group contract keys by owner component.
	groups := map[string][]string{}
	for _, key := range sortedContractKeys(model) {
		c := model.Contracts[key]
		if c.Component == "" {
			continue
		}
		groups[c.Component] = append(groups[c.Component], key)
	}

	var out []methodcheck.Finding
	ordinal := 0
	owners := make([]string, 0, len(groups))
	for owner := range groups {
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	for _, owner := range owners {
		keys := groups[owner]
		if len(keys) < 2 {
			continue // not a facet group
		}
		// op name -> the contracts publishing it and the signatures they publish.
		type pub struct{ contract, sig string }
		opPubs := map[string][]pub{}
		for _, key := range keys {
			c := model.Contracts[key]
			if c.Doc == nil {
				continue
			}
			for _, op := range c.Doc.Interface.Operations {
				opPubs[op.Name] = append(opPubs[op.Name], pub{key, paramSignature(op)})
			}
		}
		opNames := make([]string, 0, len(opPubs))
		for name := range opPubs {
			opNames = append(opNames, name)
		}
		sort.Strings(opNames)
		for _, name := range opNames {
			pubs := opPubs[name]
			contractSet := map[string]bool{}
			sigSet := map[string]bool{}
			for _, p := range pubs {
				contractSet[p.contract] = true
				sigSet[p.sig] = true
			}
			if len(contractSet) < 2 {
				continue // published by only one contract of the group
			}
			publishers := make([]string, 0, len(contractSet))
			for k := range contractSet {
				publishers = append(publishers, k)
			}
			sort.Strings(publishers)
			if len(sigSet) == 1 {
				out = append(out, finding(RuleContractDeadOp, methodcheck.SeverityError, ordinal, "facet "+owner,
					fmt.Sprintf("operation %q is published with the SAME signature by %d contracts of the %q facet group (%s) — an exact duplicate; the facets share one Go method set, so collapse it to a single facet", name, len(contractSet), owner, strings.Join(publishers, ", "))))
			} else {
				out = append(out, finding(RuleContractDeadOp, methodcheck.SeverityWarning, ordinal, "facet "+owner,
					fmt.Sprintf("operation %q is published by %d contracts of the %q facet group (%s) with DIFFERING param signatures — a name collision, not a proven dead op; a distinct-signature variant may legitimately coexist, so verify call-site liveness before pruning either", name, len(contractSet), owner, strings.Join(publishers, ", "))))
			}
			ordinal++
		}
	}
	return out
}

// paramSignature renders an operation's param signature — the ordered param names —
// as the key the dead-op rule compares to tell an exact duplicate (Error) from a
// name collision with a distinct shape (Warning).
func paramSignature(op projectmodel.Operation) string {
	names := make([]string, len(op.Params))
	for i, p := range op.Params {
		names[i] = p.Name
	}
	return strings.Join(names, "|")
}

// sortedContractKeys returns the contract keys in deterministic order.
func sortedContractKeys(model *projectmodel.Model) []string {
	keys := make([]string, 0, len(model.Contracts))
	for k := range model.Contracts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
