// Package operatedruntime is the operatedRuntimeAccess component of the
// ResourceAccess layer — the port over the OPERATED apps' runtime substrate
// (deploy/start/stop/inspect of built-app workloads), as opposed to the
// archistrator server's own runtime.
package operatedruntime

import (
	"github.com/google/uuid"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// operatedruntime.go is the hand-written, unexported surface behind the generated
// NewProfiledOperatedRuntimeAccess constructor (option-1 delegated DI, infra "Profiled").
// operatedRuntimeAccess fronts the tenant runtime — the GitOps/kubernetes deployment
// substrate plus the observability backend the operationsManager publishes desired state
// to and observes convergence from (operatedRuntimeAccess §2). There is no
// framework-go-infrastructure-kubernetes client yet, so this RA ships two PROFILES behind
// the one generated constructor, selected by the composition root:
//
//   - LOCAL / dry-run (RuntimeProfileLocal): a deterministic, side-effect-free impl for
//     dogfood / dev / systemtests. Publishes and withdraws are accepted no-ops; the
//     observe reads report a fixed Healthy / SLO-met snapshot; ReadComputeAttribution
//     invents NO usage facts (empty RuntimeEventID ⇒ the Manager appends nothing). This
//     mirrors the construction DRYRUN profile (cmd/server/construction_dryrun.go).
//
//   - REAL (RuntimeProfileReal): the production impl over the real GitOps/kubernetes +
//     observability backend. That backend is NOT yet built (it pairs with the N-DEP Argo
//     deployment work), so every verb currently returns an EXPLICIT fwra.Unknown naming
//     the follow-up and the dry-run escape hatch — NOT a silent generated stub. The seam
//     is here so the real bodies swap in one file when the kubernetes backend lands; the
//     public surface (constructor + interface) does not change.
//
// The package imports NO Temporal (layer rule): the idempotency key arrives as an
// ordinary parameter on each write.

// RuntimeProfile selects which operatedRuntimeAccess implementation the generated
// NewProfiledOperatedRuntimeAccess constructor builds.
type RuntimeProfile int

const (
	// RuntimeProfileUnknown is the zero value; rejected by the builder (a caller must
	// pick a profile explicitly).
	RuntimeProfileUnknown RuntimeProfile = iota
	// RuntimeProfileLocal selects the deterministic, side-effect-free dry-run impl.
	RuntimeProfileLocal
	// RuntimeProfileReal selects the production impl over the real GitOps/kubernetes
	// backend (skeleton until that backend lands — see the package doc).
	RuntimeProfileReal
)

// RuntimeConfig carries the REAL profile's backing-infrastructure configuration (the
// composition root binds it from env). Empty for the LOCAL profile.
type RuntimeConfig struct {
	// GitOpsRepoURL is the GitOps repository the real profile commits rendered desired
	// state to (ArgoCD watches it). Empty ⇒ unconfigured; the real verbs surface that in
	// their diagnostic. The seam is here so the real impl validates it once the
	// kubernetes backend lands.
	GitOpsRepoURL string
}

// newProfiledOperatedRuntimeAccess is the hand-written builder behind the generated
// NewProfiledOperatedRuntimeAccess constructor. It selects the impl by profile. Only an
// unset/unknown profile is a construction error (programmer misconfiguration); the REAL
// profile constructs successfully and defers its unimplemented-backend diagnostic to the
// verb calls, so the server still boots as it does today.
func newProfiledOperatedRuntimeAccess(profile RuntimeProfile, config RuntimeConfig) (OperatedRuntimeAccess, error) {
	switch profile {
	case RuntimeProfileLocal:
		return dryRunOperatedRuntime{}, nil
	case RuntimeProfileReal:
		return realOperatedRuntime{config: config}, nil
	case RuntimeProfileUnknown:
		fallthrough
	default:
		return nil, fwra.New(fwra.ContractMisuse,
			"operatedruntime.NewProfiledOperatedRuntimeAccess: unset RuntimeProfile (pick RuntimeProfileLocal or RuntimeProfileReal)")
	}
}

// ---------------------------------------------------------------------------
// LOCAL / dry-run profile — deterministic, side-effect-free.
// ---------------------------------------------------------------------------

// dryRunOperatedRuntime is the deterministic local impl. Writes are accepted no-ops;
// reads report a fixed Healthy / SLO-met snapshot; ReadComputeAttribution invents no
// usage facts. No cluster, no GitOps commit, no observability query.
type dryRunOperatedRuntime struct{}

var _ OperatedRuntimeAccess = dryRunOperatedRuntime{}

func (dryRunOperatedRuntime) PublishDesiredState(_ fwra.Context, _ uuid.UUID, _ RuntimeDesiredState, _ fwra.IdempotencyKey) error {
	return nil
}

// Withdraw is an accepted no-op — matching the contract's NotFound⇒success withdraw
// semantics (there is nothing to prune in the dry-run).
func (dryRunOperatedRuntime) Withdraw(_ fwra.Context, _ uuid.UUID, _ fwra.IdempotencyKey) error {
	return nil
}

func (dryRunOperatedRuntime) WirePaymentConfig(_ fwra.Context, _ uuid.UUID, _ GatewayBinding, _ fwra.IdempotencyKey) error {
	return nil
}

// GetApplicationHealth reports a deterministic Healthy — a freshly published dry-run app
// "converges" instantly.
func (dryRunOperatedRuntime) GetApplicationHealth(_ fwra.Context, _ uuid.UUID) (RuntimeStatus, error) {
	return RuntimeStatusHealthy, nil
}

func (dryRunOperatedRuntime) GetSloStatus(_ fwra.Context, _ uuid.UUID) (SloStatus, error) {
	return SloStatus{SloMet: true, Detail: "dry-run: SLO met"}, nil
}

// ReadComputeAttribution returns an empty attribution (zero RuntimeEventID) so the
// reconcile tick appends NO usage — the dry-run does not fabricate billing facts.
func (dryRunOperatedRuntime) ReadComputeAttribution(_ fwra.Context, _ uuid.UUID, _ AttributionWindow) (ComputeAttribution, error) {
	return ComputeAttribution{}, nil
}

// ---------------------------------------------------------------------------
// REAL profile — production skeleton (kubernetes/GitOps backend is the N-DEP follow-up).
// ---------------------------------------------------------------------------

// realOperatedRuntime is the production impl over the real GitOps/kubernetes +
// observability backend. That backend is not yet built, so every verb returns an
// explicit, diagnosable fwra.Unknown (fail-fast, non-retryable — preserving the wire
// behaviour the operationsManager façade maps to a 503) naming the N-DEP follow-up and
// the dry-run escape hatch. When the kubernetes backend lands, the real bodies replace
// these returns in place; the struct already holds the config they will need.
type realOperatedRuntime struct {
	config RuntimeConfig
}

var _ OperatedRuntimeAccess = realOperatedRuntime{}

// notImplemented is the shared, explicit real-profile diagnostic. It names the missing
// backend, the follow-up, and the dry-run escape hatch, and flags an unset GitOps target
// when that is the proximate misconfiguration.
func (r realOperatedRuntime) notImplemented(verb string) error {
	msg := "operatedruntime real profile: " + verb +
		" requires the GitOps/kubernetes backend, which is not yet implemented (follow-up N-DEP, pairs with the Argo deployment work); " +
		"set ARCHISTRATOR_OPERATIONS_DRYRUN=true for the deterministic local profile"
	if r.config.GitOpsRepoURL == "" {
		msg += " (note: ARCHISTRATOR_OPERATED_RUNTIME_GITOPS_REPO_URL is also unset)"
	}
	return fwra.New(fwra.Unknown, msg)
}

func (r realOperatedRuntime) PublishDesiredState(_ fwra.Context, _ uuid.UUID, _ RuntimeDesiredState, _ fwra.IdempotencyKey) error {
	return r.notImplemented("publishDesiredState")
}

func (r realOperatedRuntime) Withdraw(_ fwra.Context, _ uuid.UUID, _ fwra.IdempotencyKey) error {
	return r.notImplemented("withdraw")
}

func (r realOperatedRuntime) WirePaymentConfig(_ fwra.Context, _ uuid.UUID, _ GatewayBinding, _ fwra.IdempotencyKey) error {
	return r.notImplemented("wirePaymentConfig")
}

func (r realOperatedRuntime) GetApplicationHealth(_ fwra.Context, _ uuid.UUID) (RuntimeStatus, error) {
	return RuntimeStatusUnknown, r.notImplemented("getApplicationHealth")
}

func (r realOperatedRuntime) GetSloStatus(_ fwra.Context, _ uuid.UUID) (SloStatus, error) {
	return SloStatus{}, r.notImplemented("getSloStatus")
}

func (r realOperatedRuntime) ReadComputeAttribution(_ fwra.Context, _ uuid.UUID, _ AttributionWindow) (ComputeAttribution, error) {
	return ComputeAttribution{}, r.notImplemented("readComputeAttribution")
}

// variant.go holds the deployment-profile VARIANT CONSTRUCTORS for
// operatedRuntimeAccess — the step-8 A2 composegen seam. The model's
// operatedRuntimeAccess binding selects a variant per profile (cloud -> Real,
// local -> Local); the generated composition root calls the matching no-arg,
// no-error variant constructor. Both are thin wrappers over the generated
// NewProfiledOperatedRuntimeAccess whose only error is an unknown profile enum —
// which the explicit Real/Local selection never produces, so it is panic-guarded
// as a can't-happen.
//
// RESIDUAL (P1): the REAL profile's RuntimeConfig.GitOpsRepoURL
// (ARCHISTRATOR_OPERATED_RUNTIME_GITOPS_REPO_URL) is DROPPED here — the real
// GitOps/kubernetes backend is an unbuilt skeleton (follow-up N-DEP), so the
// empty RuntimeConfig is behavior-identical today (the real verbs surface their
// unimplemented-backend diagnostic regardless). When N-DEP lands, thread the URL
// via a VariantHookArgs hook (mirroring the github variants).

// NewRealOperatedRuntimeAccess builds the REAL-profile operatedRuntimeAccess (the
// production GitOps/kubernetes backend; skeleton until N-DEP). Infra-free at the
// composition root today — the RuntimeConfig is empty (see the file doc).
func NewRealOperatedRuntimeAccess() OperatedRuntimeAccess {
	rt, err := NewProfiledOperatedRuntimeAccess(RuntimeProfileReal, RuntimeConfig{})
	if err != nil {
		panic("operatedruntime.NewRealOperatedRuntimeAccess: " + err.Error())
	}
	return rt
}

// NewLocalOperatedRuntimeAccess builds the LOCAL/dry-run operatedRuntimeAccess: a
// deterministic, side-effect-free impl for the local dogfood profile.
func NewLocalOperatedRuntimeAccess() OperatedRuntimeAccess {
	rt, err := NewProfiledOperatedRuntimeAccess(RuntimeProfileLocal, RuntimeConfig{})
	if err != nil {
		panic("operatedruntime.NewLocalOperatedRuntimeAccess: " + err.Error())
	}
	return rt
}
