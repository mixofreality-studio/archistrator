package operations

import (
	"encoding/json"
	"fmt"
	"sort"
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
	// desired-state patching lands. (Task 8 removed the caller-supplied
	// DesiredStateChange.RenderedDesiredState this comment used to forward-reference:
	// the client never sent it, and the server renders desired state itself, below.)
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

	// defaultWebAppReplicas mirrors the current production webapp chart
	// (archistrator-webapp/values.yaml: replicaCount: 2). The model has no
	// per-node instance count for the webapp — it is served by an
	// InfrastructureNode (nginx, role "other"; D11), and InfrastructureNode
	// carries no Instances field (only workload DeploymentNodes do) — so this is
	// a verified platform-wide constant, the same class as the Postgres sizing
	// above, not an invented one.
	defaultWebAppReplicas = 2
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
// SELECTS BY ROLE, NEVER BY TECHNOLOGY (spec D11, founder ruling 2026-08-08). The
// deployment model's deployable elements are `infrastructureNodes` carrying
// machine-readable `role` values (gateway/identityProvider/database/other),
// alongside workload DeploymentNodes matched by their ContainerInstance's
// ContainerKey. The free-text `technology` field (e.g. "k8s", "k8s-namespace",
// "Kubernetes Deployment") is never read: it puts Kubernetes vocabulary in the
// Manager layer and couples assembly to an unconstrained string. Node structure
// (children/containerInstances/infrastructureNodes) is what's walked; role/
// relationships are what's SELECTED on.
//
// Fails loudly rather than defaulting: a model with no cloud environment, or a
// required role/workload with no matching node, is a real deployment
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

	// Server: the workload DeploymentNode carrying the server ContainerInstance.
	// Its PARENT is the app's own namespace — found structurally (no "cluster"
	// pre-filter needed: the search is depth-first over the WHOLE environment, and
	// the server container instance only legitimately exists once, under
	// namespace -> workload).
	serverKey := appName + "-server"
	serverNode, namespace, ok := findWorkload(env.Nodes, projectstate.DeploymentNode{}, serverKey)
	if !ok {
		return operatedruntime.RuntimeDesiredState{}, fmt.Errorf("assembleDesiredState: project %q's cloud environment has no workload node for container %q", appName, serverKey)
	}
	if serverNode.Instances < 1 {
		return operatedruntime.RuntimeDesiredState{}, fmt.Errorf("assembleDesiredState: project %q's server workload node %q declares %d instances; want >= 1 (a 0-instance node is a real misconfiguration, not a scale-to-zero request)", appName, serverNode.Key, serverNode.Instances)
	}

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

	// WebApp: `archistrator-webapp`'s ContainerInstance legitimately sits on the
	// architect's browser node (the SPA executes client-side) — that is correct,
	// not a gap, and is never treated as a workload to deploy. The IN-CLUSTER
	// thing is the static-asset server (nginx) that DELIVERS it, identified by
	// its relationship to the webapp's ContainerInstance (D11 trap #1: role
	// "other" alone is ambiguous — nginx and cloud-infra-temporal share it — so
	// role is a filter, never the sole selector). The browser's ContainerInstance
	// is itself the target of MORE than one relationship in the real committed
	// model (e.g. "the architect uses the SPA") — only the one sourced from an
	// infrastructure node actually in the app's own namespace is the serving
	// node; a person or any other non-infra source is not a candidate.
	webAppKey := appName + "-webapp"
	webAppCIKey, ok := findContainerInstanceKey(env.Nodes, webAppKey)
	if !ok {
		return operatedruntime.RuntimeDesiredState{}, fmt.Errorf("assembleDesiredState: project %q's cloud environment has no containerInstance for %q anywhere (not even the architect's browser, where the SPA legitimately executes)", appName, webAppKey)
	}
	webAppNode, err := findServingInfrastructureNode(env.Relationships, namespace, webAppCIKey)
	if err != nil {
		return operatedruntime.RuntimeDesiredState{}, fmt.Errorf("assembleDesiredState: project %q: %w", appName, err)
	}

	// OIDC: the namespace's identityProvider-role node. Required (an app with no
	// identity provider cannot be assembled). Its key IS carried forward as
	// OIDC.ModelKey (Task 6 enforces that every rendered manifest — including
	// Task 5's Keycloak realm/client CR, spec D12 — carries a non-empty ModelKey;
	// this is what that CR stamps itself with).
	oidcNode, err := findInfrastructureNodeByRole(namespace.InfrastructureNodes, projectstate.RoleIdentityProvider)
	if err != nil {
		return operatedruntime.RuntimeDesiredState{}, fmt.Errorf("assembleDesiredState: project %q: %w", appName, err)
	}

	// Gateway: the namespace's gateway-role node. Carried forward as its OWN key
	// rather than folded into the namespace's, because the HTTPRoutes, Envoy
	// policies and SecurityPolicy the renderer emits are what make the gateway
	// node on the deployment diagram show green or red. Stamping them with the
	// namespace key instead would leave the gateway node permanently uncoloured
	// AND misattribute route health to the namespace. Required, on the same
	// fail-loud terms as every other role lookup here.
	gatewayNode, err := findInfrastructureNodeByRole(namespace.InfrastructureNodes, projectstate.RoleGateway)
	if err != nil {
		return operatedruntime.RuntimeDesiredState{}, fmt.Errorf("assembleDesiredState: project %q: %w", appName, err)
	}

	// Postgres: D11 trap #2 — production runs ONE archistrator-postgres CNPG
	// cluster serving all three logical stores (operatedSystemState/billingState/
	// usageLog), each modeled as its OWN database-role diagram node so the
	// deployment diagram can color each independently. The single rendered
	// Cluster is still correct (matching production's one archistrator-postgres);
	// only the key-tracking must fan out, so PostgresSpec carries every
	// database-role node's key (ModelKeys), sorted for Task 4's byte-deterministic
	// render — not a single collapsed choice that would silently strand two of
	// the three diagram nodes uncoloured.
	dbNodes, err := findDatabaseNodes(namespace)
	if err != nil {
		return operatedruntime.RuntimeDesiredState{}, fmt.Errorf("assembleDesiredState: project %q: %w", appName, err)
	}
	dbKeys := make([]string, len(dbNodes))
	for i, n := range dbNodes {
		dbKeys[i] = n.Key
	}

	var manifest bundleManifest
	if uerr := json.Unmarshal(bundle.Output.Bytes, &manifest); uerr != nil {
		return operatedruntime.RuntimeDesiredState{}, fmt.Errorf("assembleDesiredState: project %q: deployable bundle is not a valid bundle manifest: %w", appName, uerr)
	}

	return operatedruntime.RuntimeDesiredState{
		AppName:         appName,
		Namespace:       appName,
		Host:            appName + "." + platformDomain,
		ModelKey:        namespace.Key,
		GatewayModelKey: gatewayNode.Key,
		Server: operatedruntime.Workload{
			ModelKey: serverNode.Key,
			Image:    manifest.ServerImage,
			Replicas: int64(serverNode.Instances),
		},
		WebApp: operatedruntime.Workload{
			ModelKey: webAppNode.Key,
			Image:    manifest.WebAppImage,
			Replicas: defaultWebAppReplicas,
		},
		Postgres: operatedruntime.PostgresSpec{
			ModelKeys:    dbKeys,
			Enabled:      true,
			Instances:    defaultPostgresInstances,
			StorageClass: defaultPostgresStorageClass,
		},
		OIDC: operatedruntime.OIDCSpec{
			ModelKey:        oidcNode.Key,
			Issuer:          "https://keycloak." + platformDomain + "/realms/" + appName,
			ClientID:        appName + "-webapp",
			ClientSecretRef: appName + "-oidc-client-secret",
		},
		SelfManaged: appName == selfManagedProjectRef,
	}, nil
}

// findCloudEnvironment returns the deployment model's cloud-profile environment.
// Profile is the model's own typed enum (projectstate.DeploymentProfile), not a
// free-text field — selecting on it is not the D11 violation the retired
// technology-string selection was.
func findCloudEnvironment(envs []projectstate.DeploymentEnvironment) (projectstate.DeploymentEnvironment, bool) {
	for _, e := range envs {
		if e.Profile == projectstate.ProfileCloud {
			return e, true
		}
	}
	return projectstate.DeploymentEnvironment{}, false
}

// findWorkload walks nodes depth-first looking for a ContainerInstance whose
// ContainerKey matches containerKey. ns is the caller's own node — the nearest
// enclosing ancestor scanned so far — so the return value's namespace is the
// direct parent of the matched workload (namespace -> workload, the structure
// every namespace node in the model follows — walked structurally, never
// filtered by a technology string first).
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

// findContainerInstanceKey returns the Key (not the ContainerKey) of the
// ContainerInstance anywhere in nodes whose ContainerKey matches containerKey —
// searched over the WHOLE tree, including subtrees that are not deployed
// workloads (the architect-machine/browser subtree, which is exactly where the
// webapp's own instance correctly lives).
func findContainerInstanceKey(nodes []projectstate.DeploymentNode, containerKey string) (string, bool) {
	for _, n := range nodes {
		for _, ci := range n.ContainerInstances {
			if ci.ContainerKey == containerKey {
				return ci.Key, true
			}
		}
		if key, ok := findContainerInstanceKey(n.Children, containerKey); ok {
			return key, true
		}
	}
	return "", false
}

// findServingInfrastructureNode returns the namespace's single infrastructure
// node that a DeploymentRelationship names as the source ("from") of an edge
// targeting ("to") toKey. A ContainerInstance can legitimately be the target of
// MORE than one relationship in the real model (verified: the real committed
// model's webapp containerInstance is ALSO the target of "the architect uses
// the SPA", sourced from a DeploymentPerson, not an infrastructure node) — so
// candidates are filtered to relationship sources that actually resolve to an
// infrastructure node IN THE APP'S OWN NAMESPACE before counting. Zero or more
// than one surviving candidate is a real modeling ambiguity, surfaced as an
// error rather than guessed — the same discipline findInfrastructureNodeByRole
// and findDatabaseNodes apply to role counts.
func findServingInfrastructureNode(rels []projectstate.DeploymentRelationship, namespace projectstate.DeploymentNode, toKey string) (projectstate.InfrastructureNode, error) {
	seen := map[string]bool{}
	var matches []projectstate.InfrastructureNode
	for _, r := range rels {
		if r.To != toKey || seen[r.From] {
			continue
		}
		for _, n := range namespace.InfrastructureNodes {
			if n.Key == r.From {
				matches = append(matches, n)
				seen[r.From] = true
				break
			}
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return projectstate.InfrastructureNode{}, fmt.Errorf("no relationship delivers %q from an infrastructure node in namespace %q", toKey, namespace.Key)
	default:
		return projectstate.InfrastructureNode{}, fmt.Errorf("%d relationships deliver %q from distinct infrastructure nodes in namespace %q; expected exactly one", len(matches), toKey, namespace.Key)
	}
}

// findInfrastructureNodeByRole returns the namespace's single infrastructure
// node carrying role. Zero or more than one is a real modeling ambiguity,
// surfaced as an error rather than guessed.
func findInfrastructureNodeByRole(nodes []projectstate.InfrastructureNode, role projectstate.ElementRole) (projectstate.InfrastructureNode, error) {
	var matches []projectstate.InfrastructureNode
	for _, n := range nodes {
		if n.Role == role {
			matches = append(matches, n)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return projectstate.InfrastructureNode{}, fmt.Errorf("no infrastructure node with role %q", role)
	default:
		return projectstate.InfrastructureNode{}, fmt.Errorf("%d infrastructure nodes with role %q; expected exactly one", len(matches), role)
	}
}

// findDatabaseNodes returns every database-role infrastructure node declared in
// namespace, sorted by Key for a deterministic pick (D11 trap #2: several
// diagram nodes can legitimately share one production resource). At least one
// is required — zero is a real misconfiguration (which database backs this
// app?).
func findDatabaseNodes(namespace projectstate.DeploymentNode) ([]projectstate.InfrastructureNode, error) {
	var matches []projectstate.InfrastructureNode
	for _, in := range namespace.InfrastructureNodes {
		if in.Role == projectstate.RoleDatabase {
			matches = append(matches, in)
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("namespace %q declares no database infrastructure node", namespace.Key)
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].Key < matches[j].Key })
	return matches, nil
}
