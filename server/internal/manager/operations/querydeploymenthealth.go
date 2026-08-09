package operations

import (
	"fmt"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedruntime"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// ===========================================================================
// QueryDeploymentHealthWorkflow — op 2.9 (short-lived read-only deployment-diagram
// health overlay). NO mutation. Task 10 of the 2026-08-07 operations/ArgoCD plan.
//
// The join itself — rendered manifest to live per-resource health — lives in
// operatedRuntimeAccess (GetDeploymentResourceHealth), NOT here: an earlier draft of
// this plan put it in the Manager and was rejected on review (task-10-brief's "Why the
// join moved into ResourceAccess") because it would have reintroduced the exact
// Kubernetes-vocabulary leak decision D11 removed. What DOES belong here is the split
// the brief draws:
//
//	ResourceAccess: "Which model keys does this app actually deploy, and how is each
//	                 doing?" — substrate-neutral RuntimeStatus, never an Argo string.
//	Manager:        "Of the keys on this diagram, which are even ours?" — reads the
//	                 cloud environment's full key list from the committed project,
//	                 marks Neutral every key the RA did not return, and collapses
//	                 RuntimeStatus to the three-state HealthState.
//
// The neutrality half (applyDiagramHealth, below) is the load-bearing one: the
// deployment model's cloud environment carries nodes archistrator does not deploy —
// the architect's own laptop, their browser, another app's namespace — and a naive
// join would paint every one of them red.
// ===========================================================================

// queryDeploymentHealthInput is the start payload for QueryDeploymentHealthWorkflow.
type queryDeploymentHealthInput struct {
	OperatedAppID operatedAppID
}

// QueryDeploymentHealthWorkflow drives op 2.9:
//  1. ReadOperatedSystemActivity → head-state (deployableBundleRef, projectRef).
//  2. RetrieveDeployableBundleActivity + ProjectStateReadProject → assembleDesiredState
//     (deploy.go), the SAME fold DeployWorkflow's full-bundle branch uses — the RA verb
//     needs the complete RuntimeDesiredState to re-render and match against, not just
//     the app's identity.
//  3. GetDeploymentResourceHealthActivity → the RA's per-model-key RuntimeStatus.
//  4. applyDiagramHealth against every key the project's cloud environment declares
//     (allDeploymentDiagramKeys) → the diagram's three-state HealthState per node.
//
// A precondition failure mirrors DeployWorkflow's own (FailedPrecondition, non-retryable):
// there is nothing to read live health FOR until something has been deployed at least
// once.
func (wf *workflows) QueryDeploymentHealthWorkflow(ctx workflow.Context, in queryDeploymentHealthInput) (DeploymentHealth, error) {
	op, err := wf.readOperatedSystem(ctx, in.OperatedAppID)
	if err != nil {
		return DeploymentHealth{}, err
	}
	if op.DeployableBundleRef == "" {
		return DeploymentHealth{}, temporal.NewNonRetryableApplicationError(
			"operated system has no deployableBundleRef (nothing has been deployed yet, so there is no live deployment to read health for)",
			fwmgr.ErrType(fwmgr.FailedPrecondition), nil)
	}

	bundle, berr := wf.retrieveBundle(ctx, op.DeployableBundleRef)
	if berr != nil {
		return DeploymentHealth{}, berr
	}
	proj, perr := wf.readProject(ctx, op.ProjectRef)
	if perr != nil {
		return DeploymentHealth{}, perr
	}
	desired, aerr := assembleDesiredState(proj, bundle, op)
	if aerr != nil {
		return DeploymentHealth{}, temporal.NewNonRetryableApplicationError(
			"failed to assemble the desired state from the project's deployment model",
			fwmgr.ErrType(fwmgr.FailedPrecondition), aerr)
	}

	raHealth, herr := wf.getDeploymentResourceHealth(ctx, in.OperatedAppID, desired)
	if herr != nil {
		return DeploymentHealth{}, herr
	}

	model, ok := proj.OperationalConcepts.Model.(*projectstate.DeploymentOperationsModel)
	if !ok || model == nil {
		return DeploymentHealth{}, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("project %q has no committed operational-concepts deployment model", proj.ID),
			fwmgr.ErrType(fwmgr.FailedPrecondition), nil)
	}
	env, ok := findCloudEnvironment(model.Deployment.Environments)
	if !ok {
		return DeploymentHealth{}, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("project %q's deployment model has no cloud environment", proj.ID),
			fwmgr.ErrType(fwmgr.FailedPrecondition), nil)
	}

	return applyDiagramHealth(raHealth, allDeploymentDiagramKeys(env.Nodes)), nil
}

// getDeploymentResourceHealth invokes operatedRuntimeAccess.getDeploymentResourceHealth.
// Shared workflow-context helper; lives in its only caller's file per the file-layout
// standard.
func (wf *workflows) getDeploymentResourceHealth(ctx workflow.Context, appID operatedAppID, desired operatedruntime.RuntimeDesiredState) ([]operatedruntime.ModelKeyHealth, error) {
	return wf.Acts.OperatedRuntimeGetDeploymentResourceHealth(ctx, appID, desired)
}

// allDeploymentDiagramKeys walks nodes depth-first and returns the key of EVERY
// DeploymentNode and every InfrastructureNode nested anywhere within it — the complete
// set of diagram nodes the cloud environment declares, deployed by archistrator or not.
// This is deliberately the WHOLE tree, not just the operated app's own namespace
// subtree: applyDiagramHealth's neutrality rule (below) needs the full key set to know
// which nodes the RA never had an opinion about, including another app's namespace
// (cloud-node-ns-gtd) and the architect's own laptop/browser
// (cloud-node-architect-machine / cloud-node-browser) — nodes archistrator does not
// deploy and must never paint red. ContainerInstance keys are deliberately excluded:
// render() never attributes a manifest to one (assembleDesiredState always resolves a
// ContainerInstance back to the DeploymentNode or InfrastructureNode that serves it —
// deploy.go), so a ContainerInstance key can never appear in the RA's ModelKeyHealth
// either and including it here would only ever manufacture a permanently-Neutral entry.
func allDeploymentDiagramKeys(nodes []projectstate.DeploymentNode) []string {
	var keys []string
	for _, n := range nodes {
		keys = append(keys, n.Key)
		for _, in := range n.InfrastructureNodes {
			keys = append(keys, in.Key)
		}
		keys = append(keys, allDeploymentDiagramKeys(n.Children)...)
	}
	return keys
}

// applyDiagramHealth is the Manager-side half of the split the brief draws: it does
// NOT re-decide health (that already happened in the RA's RuntimeStatus), it only
// answers "of the keys on this diagram, which are even ours?" and collapses the
// substrate-neutral RuntimeStatus to the diagram's three-state HealthState.
//
// allKeys is the complete diagram key set (allDeploymentDiagramKeys); raHealth is what
// operatedRuntimeAccess.GetDeploymentResourceHealth actually returned for THIS app. A
// key present in raHealth collapses RuntimeStatusHealthy → HealthStateHealthy and
// everything else (Degraded/Pending/Unknown/Withdrawn) → HealthStateUnhealthy — the D10
// "only Healthy is green" rule, expressed here over RuntimeStatus rather than an Argo
// string (a diagram concern, not a head-state concern, per the brief's split table). A
// key on the diagram that raHealth never mentions at all — the architect's laptop,
// their browser, another app's namespace — is HealthStateNeutral: the load-bearing
// rule this task adds. Never Unhealthy for a node archistrator does not deploy, and
// never simply omitted (every node in allKeys gets exactly one NodeHealth entry).
// SEVERAL RESOURCES CAN ANSWER TO ONE KEY, and the worst one wins (2026-08-08 final
// review). The gateway node alone collects four HTTPRoutes, four BackendTrafficPolicies
// and a SecurityPolicy; an earlier version of this fold kept whichever entry came LAST
// in the RA's slice, so one degraded route could be masked by a healthy one purely by
// render order. Unhealthy therefore dominates Healthy here — the same fail-closed
// direction every other rule in this path takes.
func applyDiagramHealth(raHealth []operatedruntime.ModelKeyHealth, allKeys []string) DeploymentHealth {
	byKey := make(map[string]HealthState, len(raHealth))
	for _, h := range raHealth {
		health := HealthStateUnhealthy
		if h.Status == operatedruntime.RuntimeStatusHealthy {
			health = HealthStateHealthy
		}
		if prior, seen := byKey[h.ModelKey]; seen && prior == HealthStateUnhealthy {
			continue // already condemned by another resource answering to this key.
		}
		byKey[h.ModelKey] = health
	}

	nodes := make([]NodeHealth, 0, len(allKeys))
	for _, key := range allKeys {
		health, ok := byKey[key]
		if !ok {
			health = HealthStateNeutral
		}
		nodes = append(nodes, NodeHealth{ModelKey: key, Health: health})
	}
	return DeploymentHealth{Nodes: nodes}
}
