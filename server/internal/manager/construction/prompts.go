package construction

import (
	"fmt"
	"strings"
)

// The Manager OWNS the per-step prompt corpus (constructionManager.md top note;
// the 2026-05-29 agent-role rework: the Manager's SEQUENCE owns the per-step prompt
// and asks workerAccess.GenerateTypedData[ConstructionOutput], NOT a per-phase
// drafting Engine). The generic worker holds no Method-specific prompt corpus; the
// prompt + tool choice are the caller's. This file is where the construction
// sequence's prompts live.

const constructorHeader = "You are a Worker constructing a single Method component against its frozen service contract, following Juval Lowy's The Method. Produce the component's code/output. Respond with the construction output only.\n"

const reviewerHeader = "You are a reviewer in the Method construction review set. Review the produced change from your assigned perspective and report your verdict.\n"

// constructionPrompt assembles the worker-role construction prompt for one activity.
func constructionPrompt(activity constructionActivity, class workerClass) string {
	var b strings.Builder
	b.WriteString(constructorHeader)
	fmt.Fprintf(&b, "Activity: %s (kind %s)\n", activity.ActivityID, activity.Kind.String())
	fmt.Fprintf(&b, "Component: %s (layer %s)\n", activity.ComponentID, activity.Layer)
	fmt.Fprintf(&b, "Worker class: %s\n", class.String())
	b.WriteString("Task: construct this component against its frozen contract, one service at a time, with tests.\n")
	return b.String()
}

// reviewPrompt assembles the reviewer-role prompt for one reviewer in the set.
func reviewPrompt(activity constructionActivity, reviewer Reviewer) string {
	var b strings.Builder
	b.WriteString(reviewerHeader)
	fmt.Fprintf(&b, "Activity: %s\n", activity.ActivityID)
	fmt.Fprintf(&b, "Reviewer role: %s; perspective: %s\n", reviewer.Role, reviewer.Perspective)
	if reviewer.ReferenceArtifact != nil && *reviewer.ReferenceArtifact != "" {
		fmt.Fprintf(&b, "Reference artifact: %s\n", *reviewer.ReferenceArtifact)
	}
	b.WriteString("Task: review the produced change; report verdict + findings.\n")
	return b.String()
}
