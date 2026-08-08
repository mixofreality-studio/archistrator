package operations

import (
	"encoding/json"
	"fmt"
	"strings"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/autoscaler"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedruntime"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedsystemstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// ===========================================================================
// DeployWorkflow — op 2.1 entry (operator deploy / scale / policy republish).
// ===========================================================================

// deployInput is the start payload for DeployWorkflow.
type deployInput struct {
	OperatedAppID operatedAppID
	Change        DesiredStateChange
}

// DeployWorkflow drives UC4 deploy (operationsManager.md §6.3):
//  1. ReadOperatedSystemActivity → head-state (desiredState, deployableBundleRef).
//  2. (first deploy, full bundle) RetrieveDeployableBundleActivity.
//  3. PublishDesiredStateActivity (the git commit).
//  4. RecordPublishDesiredStateActivity (head-state transition, reason=operator|deploy).
func (wf *workflows) DeployWorkflow(ctx workflow.Context, in deployInput) (DeployResult, error) {
	logger := workflow.GetLogger(ctx)

	op, err := wf.readOperatedSystem(ctx, in.OperatedAppID)
	if err != nil {
		return DeployResult{}, err
	}

	// Deploy pre-condition (§2.1): the operated system has a deployableBundleRef for a
	// first take-live (full bundle). FailedPrecondition is a terminal façade-class
	// error surfaced from the workflow.
	//
	// desired starts as the zero value: a non-full-bundle republish (operator
	// scale, autoscale, delinquency) has no fresh bundle to re-derive images from,
	// so it republishes this placeholder — matching reconcile.go's and
	// delinquencyenforcement.go's own republish call sites — until incremental
	// desired-state patching lands (the plan's later removal of
	// DesiredStateChange.RenderedDesiredState).
	var desired operatedruntime.RuntimeDesiredState
	if in.Change.Reason == ReasonDeployAfterConstruction && in.Change.PatchKind == PatchFullBundle {
		if op.DeployableBundleRef == "" {
			return DeployResult{}, temporal.NewNonRetryableApplicationError(
				"operated system has no deployableBundleRef (no constructed output to deploy)",
				fwmgr.ErrType(fwmgr.FailedPrecondition), nil)
		}
		// Retrieve the deployable bundle + the operated app's own committed
		// deployment model, then fold them (with head-state) into the typed
		// desired state the render step (Task 4+) turns into Kubernetes YAML.
		bundle, berr := wf.retrieveBundle(ctx, op.DeployableBundleRef)
		if berr != nil {
			return DeployResult{}, berr
		}
		proj, perr := wf.readProject(ctx, op.ProjectRef)
		if perr != nil {
			return DeployResult{}, perr
		}
		d, aerr := assembleDesiredState(proj, bundle, op)
		if aerr != nil {
			return DeployResult{}, temporal.NewNonRetryableApplicationError(
				"failed to assemble the desired state from the project's deployment model",
				fwmgr.ErrType(fwmgr.FailedPrecondition), aerr)
		}
		desired = d
	}

	// Publish the assembled desired state (git commit; content-idempotent).
	revision := publishRevision(in.OperatedAppID, in.Change.ChangeID)
	if perr := wf.publishDesiredState(ctx, in.OperatedAppID, desired); perr != nil {
		return DeployResult{}, perr
	}

	// Record the head-state desired-state transition (additive; Conflict loop).
	if _, rerr := wf.recordPublishDesiredState(ctx, in.OperatedAppID, op.Version, in.Change.Reason, nil); rerr != nil {
		return DeployResult{}, rerr
	}

	logger.Info("deploy published desired state", "operatedAppId", in.OperatedAppID.String(), "reason", desiredStateReasonName(in.Change.Reason))
	return DeployResult{Published: true, Revision: &revision}, nil
}

// ===========================================================================
// artifactAccess — EXISTS as a Go package (internal/resourceaccess/artifact) but the
// frozen retrieveDeployableBundle verb is NOT yet on it (it has
// RetrieveConstructionOutput). Consumed here via a NARROW seam interface mirroring
// the frozen verb; the composition root adapts the concrete *artifact.Store once the
// verb lands (escalation E-1 in C-MOP.md). The bundle ref is a plain content
// address (a string), matching the package's content-address discipline.
// ===========================================================================

// NOTE: the artifactAccess consumer-seam interface is retired (see the
// operatedSystemStateAccess note above) — reached through the generated invoker
// ArtifactRetrieveConstructionOutput (escalation E-1: the deployable bundle IS a
// construction output until the frozen retrieveDeployableBundle verb lands). The
// deployableBundle mirror below remains as the workflow's retrieve-bundle result.

// deployableBundle mirrors the constructed-output bundle retrieved for a first
// deploy. Re-uses the existing artifact.ConstructionOutput shape as the bundle body
// (the deployable bundle IS a construction output — artifactAccess.md), kept as a
// thin Manager-local wrapper so the seam stays narrow.
type deployableBundle struct {
	Output artifact.ConstructionOutput
}

// ---------------------------------------------------------------------------
// Head-state read + recovering write helpers (§6.5).
// ---------------------------------------------------------------------------

// readOperatedSystem invokes operatedSystemStateAccess.readOperatedSystem. Task 4: the
// former Manager-local operatedSystem mirror is retired — the invoker's contract type
// IS the workflow's internal currency now, so no fold happens here.
// Shared workflow-context helper (used by 4 workflows); lives in its first caller's file per the file-layout standard.
func (wf *workflows) readOperatedSystem(ctx workflow.Context, operatedAppID operatedAppID) (operatedsystemstate.OperatedSystem, error) {
	return wf.Acts.OperatedSystemStateReadOperatedSystem(ctx, operatedAppID)
}

// retrieveBundle invokes artifactAccess.retrieveConstructionOutput (escalation E-1:
// the deployable bundle IS a construction output until the frozen
// retrieveDeployableBundle verb lands).
func (wf *workflows) retrieveBundle(ctx workflow.Context, ref string) (deployableBundle, error) {
	out, err := wf.Acts.ArtifactRetrieveConstructionOutput(ctx, ref)
	if err != nil {
		return deployableBundle{}, err
	}
	return deployableBundle{Output: out}, nil
}

// readProject invokes projectStateAccess.readProject — the edge this task adds
// (operationsManager -> projectStateAccess, project.json .systemDesign
// relationships) so assembleDesiredState can fold the operated app's own
// committed deployment model into the desired state.
func (wf *workflows) readProject(ctx workflow.Context, projectRef string) (projectstate.Project, error) {
	return wf.Acts.ProjectStateReadProject(ctx, projectstate.ProjectID(projectRef))
}

// publishDesiredState invokes operatedRuntimeAccess.publishDesiredState (git commit;
// content-idempotent). Task 4: the former Manager-local runtimeDesiredState mirror is
// retired — desired IS the contract type now.
// Shared workflow-context helper (used by 3 workflows); lives in its first caller's file per the file-layout standard.
func (wf *workflows) publishDesiredState(ctx workflow.Context, appID operatedAppID, desired operatedruntime.RuntimeDesiredState) error {
	return wf.Acts.OperatedRuntimePublishDesiredState(ctx, appID, desired)
}

// recordPublishDesiredState applies the head-state desired-state transition with the
// Conflict loop (§6.5). decision is carried only for reason=autoscale. Task 4: seed/
// return now speak operatedsystemstate.Version directly (the former Manager-local
// version mirror is retired). Task 5: decision is now the published *autoscaler.Decision
// (the seam autoscaleDecisionSeam is retired) — autoscaleDecisionToState (adapters.go)
// bridges it straight to operatedsystemstate.AutoscaleDecision.
// Shared workflow-context helper (used by 2 workflows); lives in its first caller's file per the file-layout standard.
func (wf *workflows) recordPublishDesiredState(ctx workflow.Context, appID operatedAppID, seed operatedsystemstate.Version, reason DesiredStateReason, decision *autoscaler.Decision) (operatedsystemstate.Version, error) {
	return wf.applyRecovering(ctx, appID, seed, func(expected operatedsystemstate.Version) (operatedsystemstate.Version, error) {
		return wf.Acts.OperatedSystemStatePublishDesiredState(ctx, appID,
			expected,
			desiredStateReasonToState(reason),
			autoscaleDecisionToState(decision))
	})
}

// applyRecovering executes one head-state mutation Activity with a workflow-level
// Conflict re-read→re-apply loop (§6.5; identical discipline to construction). On a
// stale-version fwra.Conflict it re-reads the true head Version and re-applies with
// the SAME idempotency key (dedup-first ordering preserves idempotent replay).
// Shared workflow-context helper (used by 4 workflows); lives in its first caller's file per the file-layout standard.
func (wf *workflows) applyRecovering(
	ctx workflow.Context,
	appID operatedAppID,
	seed operatedsystemstate.Version,
	apply func(expected operatedsystemstate.Version) (operatedsystemstate.Version, error),
) (operatedsystemstate.Version, error) {
	expected := seed
	for attempt := 0; ; attempt++ {
		v, err := apply(expected)
		if err == nil {
			return v, nil
		}
		if !isConflict(err) {
			return 0, err
		}
		if attempt+1 >= maxMutateConflictAttempts {
			return 0, temporal.NewNonRetryableApplicationError(
				"head-state conflict did not converge within bounded attempts",
				"MutateConflictExhausted", err)
		}
		op, rerr := wf.readOperatedSystem(ctx, appID)
		if rerr != nil {
			return 0, rerr
		}
		expected = op.Version
		workflow.GetLogger(ctx).Info("head-state conflict; re-read version and retrying",
			"attempt", attempt+1, "nextExpectedVersion", expected)
	}
}

// publishRevision derives a deterministic published-revision token for UI correlation
// (opaque; not a Temporal id).
func publishRevision(appID operatedAppID, changeID string) string {
	return appID.String() + ":" + changeID
}

// ===========================================================================
// assembleDesiredState folds the operated app's typed deployment model
// (projectStateAccess), its constructed images (artifactAccess, via
// deployableBundle above), and its runtime head-state (operatedSystemStateAccess)
// into the ONE typed operatedruntime.RuntimeDesiredState the renderer (Task 4+)
// turns into Kubernetes YAML. The renderer itself is NOT this task's concern —
// assembleDesiredState's job ends at producing a fully-populated,
// internally-consistent struct or a loud error. Lives here (not a separate
// assemble.go) per the layer file-layout standard: one hand-written file per
// workflow, and this fold is DeployWorkflow's own — the only caller.
//
// ---------------------------------------------------------------------------
// Platform-wide conventions (verified against the real production Helm charts at
// software/products/archistrator/helm/*, NOT invented): every operated app is
// reachable at <appID>.capture-gtd.com, authenticates through a same-named Keycloak
// realm/client on the shared keycloak.capture-gtd.com, and gets one CloudNativePG
// instance on the shared do-block-storage class. archistrator's own self-managed
// deployment is the one instance of this convention that exists today (verified
// against archistrator-gateway-routes/archistrator-server's values.yaml); a real
// multi-tenant assembly would derive the Postgres sizing from the operated app's
// tier, not this fixed pair of constants.
// ---------------------------------------------------------------------------
const (
	// selfManagedProjectRef is the well-known project id archistrator uses for its
	// OWN system design (see cmd/gen-systemtests, cmd/gen-uitests-fixtures,
	// cmd/gen-uitests-episodes, all of which default their -project/-id flag to
	// this same literal). A project whose ID matches it is archistrator operating
	// itself, not a customer-operated tenant.
	selfManagedProjectRef = "archistrator"

	// platformDomain is the shared apex domain every operated app is hosted under
	// (archistrator-gateway-routes/values.yaml: "Base domain (same as gtd's
	// hosts). archistrator is served at archistrator.<domain>.").
	platformDomain = "capture-gtd.com"

	// defaultPostgresInstances / defaultPostgresStorageClass mirror the current
	// platform-wide CloudNativePG sizing (archistrator-postgres/values.yaml:
	// instances: 1, storageClass: do-block-storage) — every operated app gets the
	// same shape today; there is no per-app sizing input yet.
	defaultPostgresInstances    = 1
	defaultPostgresStorageClass = "do-block-storage"
)

// bundleManifest is the deployable bundle's structured content: the image
// references assembleDesiredState needs, JSON-encoded into
// deployableBundle.Output.Bytes by the bundle's producer (constructionManager /
// artifactAccess.storeConstructionOutput — escalation E-1, out of this task's
// scope). There is no established wire shape for this yet, so this is a minimal,
// deliberately narrow contract: exactly the two image refs the renderer needs.
type bundleManifest struct {
	ServerImage string `json:"serverImage"`
	WebAppImage string `json:"webAppImage"`
}

// assembleDesiredState folds proj's committed cloud deployment model, bundle's
// constructed images, and op's head-state into a RuntimeDesiredState. op is
// accepted for interface-shape symmetry with the workflow's other head-state reads
// and as the future extension point for a per-op replica/version override; this
// fold does not read it — every field below is either a real, existing datum
// (the deployment model's node keys + declared instance counts, the bundle's
// image refs) or one of the verified platform-wide conventions documented above.
//
// Fails loudly rather than defaulting: a model with no cloud environment, or a
// workload/database node the model does not declare, is a real deployment
// misconfiguration and must surface as an error the operator can read — never a
// silently half-rendered deployment.
func assembleDesiredState(proj projectstate.Project, bundle deployableBundle, _ operatedsystemstate.OperatedSystem) (operatedruntime.RuntimeDesiredState, error) {
	appName := string(proj.ID)
	if appName == "" {
		return operatedruntime.RuntimeDesiredState{}, fmt.Errorf("assembleDesiredState: project has no ID")
	}

	model, ok := proj.OperationalConcepts.Model.(*projectstate.DeploymentOperationsModel)
	if !ok || model == nil {
		return operatedruntime.RuntimeDesiredState{}, fmt.Errorf("assembleDesiredState: project %q has no committed operational-concepts deployment model", appName)
	}

	env, ok := findCloudEnvironment(model.Deployment.Environments)
	if !ok {
		return operatedruntime.RuntimeDesiredState{}, fmt.Errorf("assembleDesiredState: project %q's deployment model has no cloud environment", appName)
	}

	cluster, ok := findNodeByTechnology(env.Nodes, "k8s")
	if !ok {
		return operatedruntime.RuntimeDesiredState{}, fmt.Errorf("assembleDesiredState: project %q's cloud environment has no k8s cluster node", appName)
	}

	serverKey, webAppKey := appName+"-server", appName+"-webapp"

	serverNode, serverNS, ok := findWorkload(cluster.Children, cluster, serverKey)
	if !ok {
		return operatedruntime.RuntimeDesiredState{}, fmt.Errorf("assembleDesiredState: project %q's cloud environment has no workload node for container %q", appName, serverKey)
	}
	webAppNode, webAppNS, ok := findWorkload(cluster.Children, cluster, webAppKey)
	if !ok {
		return operatedruntime.RuntimeDesiredState{}, fmt.Errorf("assembleDesiredState: project %q's cloud environment has no workload node for container %q", appName, webAppKey)
	}
	if serverNS.Key != webAppNS.Key {
		return operatedruntime.RuntimeDesiredState{}, fmt.Errorf("assembleDesiredState: project %q's server (%s) and webapp (%s) workloads are declared in different namespaces", appName, serverNS.Key, webAppNS.Key)
	}
	namespace := serverNS

	// The k8s namespace STRING is not itself a field anywhere on DeploymentNode —
	// only the platform's own "ns-<appID>" node-key convention (verified: real
	// nodes are literally "cloud-node-ns-archistrator", "cloud-node-ns-gtd") names
	// it. Rather than silently ASSUMING the convention holds and using appName
	// regardless of what the resolved namespace node actually says, verify the
	// resolved node's key follows it and fail loudly on disagreement — a model
	// that declared some other namespace (e.g. "ns-foo") must not silently deploy
	// to appName instead.
	if !strings.HasSuffix(namespace.Key, "ns-"+appName) {
		return operatedruntime.RuntimeDesiredState{}, fmt.Errorf("assembleDesiredState: project %q's resolved namespace node %q does not follow the ns-%s naming convention (server/webapp workloads must live under the app's own namespace)", appName, namespace.Key, appName)
	}

	pgNode, err := findDatabaseNode(namespace)
	if err != nil {
		return operatedruntime.RuntimeDesiredState{}, fmt.Errorf("assembleDesiredState: project %q: %w", appName, err)
	}

	if serverNode.Instances < 1 {
		return operatedruntime.RuntimeDesiredState{}, fmt.Errorf("assembleDesiredState: project %q's server workload node %q declares %d instances; want >= 1 (a 0-instance node is a real misconfiguration, not a scale-to-zero request)", appName, serverNode.Key, serverNode.Instances)
	}
	if webAppNode.Instances < 1 {
		return operatedruntime.RuntimeDesiredState{}, fmt.Errorf("assembleDesiredState: project %q's webapp workload node %q declares %d instances; want >= 1 (a 0-instance node is a real misconfiguration, not a scale-to-zero request)", appName, webAppNode.Key, webAppNode.Instances)
	}

	var manifest bundleManifest
	if uerr := json.Unmarshal(bundle.Output.Bytes, &manifest); uerr != nil {
		return operatedruntime.RuntimeDesiredState{}, fmt.Errorf("assembleDesiredState: project %q: deployable bundle is not a valid bundle manifest: %w", appName, uerr)
	}

	return operatedruntime.RuntimeDesiredState{
		AppName:   appName,
		Namespace: appName,
		Host:      appName + "." + platformDomain,
		ModelKey:  namespace.Key,
		Server: operatedruntime.Workload{
			ModelKey: serverNode.Key,
			Image:    manifest.ServerImage,
			Replicas: int64(serverNode.Instances),
		},
		WebApp: operatedruntime.Workload{
			ModelKey: webAppNode.Key,
			Image:    manifest.WebAppImage,
			Replicas: int64(webAppNode.Instances),
		},
		Postgres: operatedruntime.PostgresSpec{
			ModelKey:     pgNode.Key,
			Enabled:      true,
			Instances:    defaultPostgresInstances,
			StorageClass: defaultPostgresStorageClass,
		},
		OIDC: operatedruntime.OIDCSpec{
			Issuer:          "https://keycloak." + platformDomain + "/realms/" + appName,
			ClientID:        appName + "-webapp",
			ClientSecretRef: appName + "-oidc-client-secret",
		},
		SelfManaged: appName == selfManagedProjectRef,
	}, nil
}

// findCloudEnvironment returns the deployment model's cloud-profile environment.
func findCloudEnvironment(envs []projectstate.DeploymentEnvironment) (projectstate.DeploymentEnvironment, bool) {
	for _, e := range envs {
		if e.Profile == projectstate.ProfileCloud {
			return e, true
		}
	}
	return projectstate.DeploymentEnvironment{}, false
}

// findNodeByTechnology returns the first node (searched depth-first) whose
// Technology matches tech — used to find the k8s cluster root structurally
// rather than by a hardcoded node key, so architect-machine/external subtrees
// (which carry no "k8s" node) are never mistaken for the deployed cluster.
func findNodeByTechnology(nodes []projectstate.DeploymentNode, tech string) (projectstate.DeploymentNode, bool) {
	for _, n := range nodes {
		if n.Technology == tech {
			return n, true
		}
		if found, ok := findNodeByTechnology(n.Children, tech); ok {
			return found, true
		}
	}
	return projectstate.DeploymentNode{}, false
}

// findWorkload walks nodes depth-first looking for a ContainerInstance whose
// ContainerKey matches containerKey. ns is the caller's own node — the nearest
// enclosing ancestor scanned so far — so the return value's namespace is the
// direct parent of the matched workload (cluster -> namespace -> workload, the
// convention every namespace node in the model follows).
func findWorkload(nodes []projectstate.DeploymentNode, ns projectstate.DeploymentNode, containerKey string) (workload, namespace projectstate.DeploymentNode, found bool) {
	for _, n := range nodes {
		for _, ci := range n.ContainerInstances {
			if ci.ContainerKey == containerKey {
				return n, ns, true
			}
		}
		if w, wns, ok := findWorkload(n.Children, n, containerKey); ok {
			return w, wns, true
		}
	}
	return projectstate.DeploymentNode{}, projectstate.DeploymentNode{}, false
}

// findDatabaseNode returns the namespace's single database-role infrastructure
// node. Zero or more than one is a real modeling ambiguity (which database backs
// this app?) — surfaced as an error rather than guessed.
func findDatabaseNode(namespace projectstate.DeploymentNode) (projectstate.InfrastructureNode, error) {
	var matches []projectstate.InfrastructureNode
	for _, in := range namespace.InfrastructureNodes {
		if in.Role == projectstate.RoleDatabase {
			matches = append(matches, in)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return projectstate.InfrastructureNode{}, fmt.Errorf("namespace %q declares no database infrastructure node", namespace.Key)
	default:
		return projectstate.InfrastructureNode{}, fmt.Errorf("namespace %q declares %d database infrastructure nodes; expected exactly one", namespace.Key, len(matches))
	}
}
