// derive_deltas.go enforces and applies the authored delta vocabulary.
//
// The vocabulary is CLOSED and deliberately narrow — numbers plus additive activities:
//
//   - an OVERRIDE may replace effortDays / riskBucket on a DERIVED activity, and must
//     carry a written justification;
//   - an ADDITIVE may append an activity that maps to NO single component, declaring its
//     own incident edges.
//
// There is no exclusion and no derived-to-derived edge override, on purpose. An
// exclusion asserts that a committed component requires no work — which is either false
// or an admission that it should not be a component. A wrong exclusion is SILENT where a
// wrong derivation is LOUD, and the silent form is exactly how C-HE, C-WIA and R-WIT
// survived in the committed plan against components that no longer exist. If a derived
// edge is wrong, the System relationship is wrong: fix the architecture.

package estimation

import (
	"sort"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

// workerRoster is the fixed Method team. An unknown class silently rides default token
// rates in the cost engines and misclassifies in every downstream view, so it is
// rejected rather than defaulted.
var workerRoster = map[string]bool{
	"system-architect": true, "product-manager": true, "project-manager": true,
	"senior-developer": true, "junior-developer": true, "ui-designer": true,
	"ux-reviewer": true, "qa-engineer": true, "test-engineer": true, "software-tester": true,
}

var fibonacciBuckets = map[int64]bool{1: true, 2: true, 3: true, 5: true, 8: true, 13: true}

// legalEffort enforces App C §4.4: a 5-day quantum, no god activity.
func legalEffort(d float64) bool {
	return d > 0 && d <= 35 && float64(int(d)) == d && int(d)%5 == 0
}

// applyOverrides applies the ActivityOverride deltas onto acts (indexed by index) in
// place. An override may replace effortDays/riskBucket on a DERIVED activity only, and
// must carry a written justification.
func applyOverrides(acts []DerivedActivity, index map[string]int, overrides []ActivityOverride) error {
	for _, o := range overrides {
		i, ok := index[o.Activity]
		if !ok {
			return fweng.New(fweng.ContractMisuse,
				"DerivePlan: override names activity "+o.Activity+" which the System does not derive; "+
					"if the work is real the architecture is missing a component, and if the component is gone the override is a zombie")
		}
		if o.Justification == "" {
			return fweng.New(fweng.ContractMisuse,
				"DerivePlan: override of "+o.Activity+" carries no justification; the delta document is the entire human-review surface and every line must defend itself")
		}
		if o.EffortDays != nil {
			if !legalEffort(*o.EffortDays) {
				return fweng.New(fweng.ContractMisuse,
					"DerivePlan: override of "+o.Activity+" breaks the 5-day quantum or the 35-day god-activity cap")
			}
			acts[i].EffortDays = *o.EffortDays
		}
		if o.RiskBucket != nil {
			if !fibonacciBuckets[*o.RiskBucket] {
				return fweng.New(fweng.ContractMisuse,
					"DerivePlan: override of "+o.Activity+" sets a non-Fibonacci risk bucket")
			}
			acts[i].RiskBucket = *o.RiskBucket
		}
	}
	return nil
}

// validateAdditive checks one AdditiveActivity against the closed vocabulary. It does
// NOT check incident edges — those are validated only after every additive has been
// added to the index (see appendAdditives / validateAdditiveEdges), so two additives
// may legally depend on each other.
func validateAdditive(a AdditiveActivity, index map[string]int) error {
	if _, clash := index[a.Name]; clash {
		return fweng.New(fweng.ContractMisuse,
			"DerivePlan: additive activity "+a.Name+" shadows a derived activity; that is an exclusion in disguise")
	}
	if a.ComponentID != nil {
		return fweng.New(fweng.ContractMisuse,
			"DerivePlan: additive activity "+a.Name+" carries a componentId; additive is for genuinely componentless work, "+
				"and a component-bound additive is a covert exclusion/replacement channel")
	}
	if a.Justification == "" {
		return fweng.New(fweng.ContractMisuse,
			"DerivePlan: additive activity "+a.Name+" carries no justification")
	}
	if !legalEffort(a.EffortDays) {
		return fweng.New(fweng.ContractMisuse,
			"DerivePlan: additive activity "+a.Name+" breaks the 5-day quantum or the 35-day god-activity cap")
	}
	if !fibonacciBuckets[a.RiskBucket] {
		return fweng.New(fweng.ContractMisuse,
			"DerivePlan: additive activity "+a.Name+" sets a non-Fibonacci risk bucket")
	}
	if !workerRoster[a.WorkerClass] {
		return fweng.New(fweng.ContractMisuse,
			"DerivePlan: additive activity "+a.Name+" names worker class "+a.WorkerClass+
				", which is not on the fixed Method roster; an unknown class silently rides default token rates")
	}
	return nil
}

// appendAdditives validates and appends deltas.Additive onto acts, registering each new
// name in index as it goes, and returns the extra dependency edges the additives declare
// for themselves (not yet validated against the post-additive index).
func appendAdditives(acts []DerivedActivity, index map[string]int, additive []AdditiveActivity) ([]DerivedActivity, []NetworkDependency, error) {
	extraDeps := make([]NetworkDependency, 0, len(additive))
	for _, a := range additive {
		if err := validateAdditive(a, index); err != nil {
			return acts, nil, err
		}
		acts = append(acts, DerivedActivity{
			Name: a.Name, Title: a.Title, EffortDays: a.EffortDays, RiskBucket: a.RiskBucket,
			WorkerClass: a.WorkerClass, Coding: a.Coding, Derived: false,
		})
		index[a.Name] = len(acts) - 1
		if len(a.DependsOn) > 0 {
			preds := make([]string, len(a.DependsOn))
			copy(preds, a.DependsOn)
			sort.Strings(preds)
			extraDeps = append(extraDeps, NetworkDependency{Activity: a.Name, DependsOn: preds})
		}
	}
	return acts, extraDeps, nil
}

// validateAdditiveEdges checks additive incident edges only AFTER every additive has
// been added to index (C3: an additive declares its OWN incident edges only — the
// target must exist in the plan, or it would inject a dangling node into the CPM
// solve).
func validateAdditiveEdges(extraDeps []NetworkDependency, index map[string]int) error {
	for _, d := range extraDeps {
		for _, p := range d.DependsOn {
			if _, ok := index[p]; !ok {
				return fweng.New(fweng.ContractMisuse,
					"DerivePlan: additive activity "+d.Activity+" depends on "+p+
						", which is not an activity in the plan; an additive declares its OWN incident edges only")
			}
		}
	}
	return nil
}

// applyAdditiveMilestones validates and appends the AdditiveMilestone deltas (C4: M5 "v1
// Production Live" depends entirely on additive noncoding and therefore cannot derive)
// onto ms, and returns the combined, sorted milestone list. A derived milestone may
// still ACQUIRE predecessors from additive activities, which is why dependsOn is
// validated against the full post-additive activity set in index.
func applyAdditiveMilestones(ms []NetworkMilestone, index map[string]int, additive []AdditiveMilestone) ([]NetworkMilestone, error) {
	milestones := make([]NetworkMilestone, len(ms))
	copy(milestones, ms)
	derivedMilestone := make(map[string]bool, len(ms))
	for _, m := range ms {
		derivedMilestone[m.Id] = true
	}
	for _, am := range additive {
		if derivedMilestone[am.Id] {
			return nil, fweng.New(fweng.ContractMisuse,
				"DerivePlan: additive milestone "+am.Id+" shadows a derived milestone")
		}
		if am.Justification == "" {
			return nil, fweng.New(fweng.ContractMisuse,
				"DerivePlan: additive milestone "+am.Id+" carries no justification")
		}
		for _, p := range am.DependsOn {
			if _, ok := index[p]; !ok {
				return nil, fweng.New(fweng.ContractMisuse,
					"DerivePlan: additive milestone "+am.Id+" depends on "+p+", which is not an activity in the plan")
			}
		}
		preds := make([]string, len(am.DependsOn))
		copy(preds, am.DependsOn)
		sort.Strings(preds)
		milestones = append(milestones, NetworkMilestone{Id: am.Id, DependsOn: preds})
		derivedMilestone[am.Id] = true
	}
	sort.Slice(milestones, func(i, j int) bool { return milestones[i].Id < milestones[j].Id })
	return milestones, nil
}

// applyDeltas overlays the authored deltas on the derived baseline.
func applyDeltas(base []DerivedActivity, deps []NetworkDependency, ms []NetworkMilestone, deltas ActivityListDeltas) (DerivedPlan, error) {
	index := make(map[string]int, len(base))
	for i, a := range base {
		index[a.Name] = i
	}
	acts := make([]DerivedActivity, len(base))
	copy(acts, base)

	if err := applyOverrides(acts, index, deltas.Overrides); err != nil {
		return DerivedPlan{}, err
	}

	acts, extraDeps, err := appendAdditives(acts, index, deltas.Additive)
	if err != nil {
		return DerivedPlan{}, err
	}

	// Additive edges are validated only AFTER every additive exists, so two additives may
	// legally depend on each other.
	if err := validateAdditiveEdges(extraDeps, index); err != nil {
		return DerivedPlan{}, err
	}

	milestones, err := applyAdditiveMilestones(ms, index, deltas.AdditiveMilestones)
	if err != nil {
		return DerivedPlan{}, err
	}

	allDeps := make([]NetworkDependency, 0, len(deps)+len(extraDeps))
	allDeps = append(allDeps, deps...)
	allDeps = append(allDeps, extraDeps...)
	sort.Slice(allDeps, func(i, j int) bool { return allDeps[i].Activity < allDeps[j].Activity })
	sort.Slice(acts, func(i, j int) bool { return acts[i].Name < acts[j].Name })

	return DerivedPlan{Activities: acts, Dependencies: allDeps, Milestones: milestones}, nil
}
