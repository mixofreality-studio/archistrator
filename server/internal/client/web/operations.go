package web

import (
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	fwm "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/operations"
)

// operationsCtx builds the Manager-layer call Context for an operationsManager op from
// an HTTP request: the request context plus the authenticated principal (the auth
// middleware puts one on the context; the zero principal is a safe stopgap). Mirrors
// constructionCtx (construction.go).
func operationsCtx(r *http.Request) fwm.Context {
	rc := fwm.Context{Context: r.Context()}
	if p, ok := security.PrincipalFrom(r.Context()); ok {
		rc.Principal = p
	}
	return rc
}

// This file is the HTTP binding for the UC4 operateDeliveredSystem facet (the
// operations console). Each handler is a THIN routing facet (webClient.md §0):
// decode HTTP/JSON → resolve principal + authorize (security Utility) → translate
// to the Manager's typed input → invoke EXACTLY ONE operationsManager op → shape
// the reply. No business logic, no state, no cross-Manager sequencing. Mirrors
// construction.go (UC3) and handlers.go (UC1/UC2).
//
// The five operate intents route to operationsManager only (the one-Manager-per-
// request rule), and fold onto THREE Manager ops (webClient.md §"Factoring
// decisions"):
//   - Deploy / Scale / UpdateAutoscalerPolicy → DeployAfterConstruction (Workflow):
//     a desired-state republish parameterized by patch. Deploy republishes a full
//     bundle with reason=deployAfterConstruction; Scale and UpdateAutoscalerPolicy
//     republish a patch with reason=operator.
//   - Withdraw            → WithdrawSystem      (Workflow): terminal withdraw.
//   - QueryCostProjection → QueryCostProjection (Workflow): read-only, side-effect-free.
//
// The reconcile (Schedule) and delinquency (cross-Manager Signal) Manager ops are NOT
// console-driven and have no route here.

// --- request DTOs ----------------------------------------------------------

// deployRequest republishes the operated app's full desired-state bundle (the first
// take-live after construction). The operatedAppId is the {operatedAppId} path segment,
// not a body field. ChangeId is the operator-supplied continuity token that keys the
// deploy workflow ({operatedAppId}:deploy:{changeId}) — name-as-identity: the operator
// controls it so a retried deploy is idempotent (the Manager rejects an empty changeId
// as ContractMisuse). RenderedDesiredState is the infrastructure-neutral bundle bytes
// (opaque at this boundary); optional, the Manager retrieves the constructed bundle from
// artifactAccess on a first deploy.
type deployRequest struct {
	ChangeID             string `json:"changeId"`
	RenderedDesiredState []byte `json:"renderedDesiredState,omitempty"`
}

// scaleRequest republishes a manual scale patch (reason=operator). ChangeId is the
// operator-supplied continuity token; ScalePatch is the infrastructure-neutral rendered
// patch (opaque at this boundary). The fold onto DeployAfterConstruction is the frozen
// operationsManager §2.6 factor-up — the Client invents no Manager op.
type scaleRequest struct {
	ChangeID   string `json:"changeId"`
	ScalePatch []byte `json:"scalePatch,omitempty"`
}

// updateAutoscalerPolicyRequest republishes an autoscaler-policy change patch
// (reason=operator). Same shape as scaleRequest, different patch kind.
type updateAutoscalerPolicyRequest struct {
	ChangeID    string `json:"changeId"`
	PolicyPatch []byte `json:"policyPatch,omitempty"`
}

// withdrawRequest terminally withdraws the operated app. ChangeId is the operator-
// supplied continuity token that keys the withdraw workflow ({operatedAppId}:withdraw:
// {changeId}) — idempotent on the id (an already-withdrawn app is a no-op success).
// Reason is the operator's free-text withdrawal rationale (opaque to the contract).
type withdrawRequest struct {
	ChangeID string `json:"changeId"`
	Reason   string `json:"reason,omitempty"`
}

// --- response DTOs ---------------------------------------------------------

// deployResultResponse is the wire form of operations.DeployResult. Published is true
// iff the desired state was durably published; Revision is the published revision (for
// UI correlation; opaque). Agent-friendly: no server-derivable id the client must mint.
type deployResultResponse struct {
	OperatedAppID string `json:"operatedAppId"`
	Published     bool   `json:"published"`
	Revision      string `json:"revision,omitempty"`
}

// withdrawResultResponse is the wire form of operations.WithdrawResult. Withdrawn is
// true iff the withdrawal was durably recorded (an already-withdrawn app is an
// idempotent success).
type withdrawResultResponse struct {
	OperatedAppID string `json:"operatedAppId"`
	Withdrawn     bool   `json:"withdrawn"`
}

// costProjectionResponse is the wire form of operations.CostProjection (the read-only
// op-time cost projection). The untagged Manager seam types (Money, WhatIfCurve) are
// re-shaped here onto stable, explicitly-tagged wire fields so the SPA/agent sees a
// documented JSON shape rather than Go field names. NO state mutation produces it.
type costProjectionResponse struct {
	OperatedAppID        string           `json:"operatedAppId"`
	CurrentRunRate       moneyDTO         `json:"currentRunRate"`
	ProjectedMonthlyCost moneyDTO         `json:"projectedMonthlyCost"`
	ScaleWhatIfCurve     []whatIfPointDTO `json:"scaleWhatIfCurve"`
}

// moneyDTO is the wire form of operations.Money — minor units + ISO currency.
type moneyDTO struct {
	MinorUnits int64  `json:"minorUnits"`
	Currency   string `json:"currency"`
}

// whatIfPointDTO is the wire form of operations.WhatIfPoint — one hypothetical scale
// level and its projected monthly cost.
type whatIfPointDTO struct {
	Replicas             int      `json:"replicas"`
	ProjectedMonthlyCost moneyDTO `json:"projectedMonthlyCost"`
}

// operatedSystemViewResponse is the wire form of operations.OperatedSystemView (the
// read-only operator display view, operationsRead-ruling.md §C). It is a HARD CONTRACT
// the SPA (U-SPA-4) is built against — the camelCase JSON field names below are stable.
// The untagged Manager seam types are re-shaped onto explicitly-tagged wire fields, and
// the RuntimeStatusSeam / AutoscalerMode / AutoscaleAction enums are rendered onto
// stable lowercase wire strings via the …Name(...) mappers below. The Money reuses the
// existing moneyDTO the cost-projection route returns. NO state mutation produces it.
type operatedSystemViewResponse struct {
	OperatedAppID  string                  `json:"operatedAppId"`
	Phase          string                  `json:"phase"`
	InFlight       bool                    `json:"inFlight"`
	Health         healthSnapshotDTO       `json:"health"`
	Slos           []sloRowDTO             `json:"slos"`
	RecentEvents   []runtimeStatusEventDTO `json:"recentEvents"`
	Autoscaler     autoscalerDTO           `json:"autoscaler"`
	CurrentRunRate moneyDTO                `json:"currentRunRate"`
}

// healthSnapshotDTO is the wire form of operations.HealthSnapshotView.
type healthSnapshotDTO struct {
	SloMet bool   `json:"sloMet"`
	Detail string `json:"detail"`
	Phase  string `json:"phase"`
}

// sloRowDTO is the wire form of one operations.SloRowView per-component SLO row.
type sloRowDTO struct {
	Component string `json:"component"`
	Objective string `json:"objective"`
	SloMet    bool   `json:"sloMet"`
	Healthy   bool   `json:"healthy"`
}

// runtimeStatusEventDTO is the wire form of one operations.RuntimeStatusEventView
// (newest-first, bounded). At is RFC3339.
type runtimeStatusEventDTO struct {
	At   string `json:"at"`
	From string `json:"from"`
	To   string `json:"to"`
	Note string `json:"note"`
}

// autoscalerDTO is the wire form of operations.AutoscalerView (mode + decision history).
type autoscalerDTO struct {
	Mode      string                 `json:"mode"`
	Decisions []autoscaleDecisionDTO `json:"decisions"`
}

// autoscaleDecisionDTO is the wire form of one operations.AutoscaleDecisionView
// (newest-first, bounded). At is RFC3339.
type autoscaleDecisionDTO struct {
	At        string `json:"at"`
	Action    string `json:"action"`
	Reason    string `json:"reason"`
	Published bool   `json:"published"`
}

// --- handlers --------------------------------------------------------------

// handleDeploy — operateDeliveredSystem{Deploy}. Routes to
// operationsManager.DeployAfterConstruction (Workflow) with reason=deployAfterConstruction
// and a full-bundle patch. 202 Accepted with the DeployResult once the desired state is
// durably published (convergence is observed later via the reconcile Schedule).
func (c *Client) handleDeploy(w http.ResponseWriter, r *http.Request) {
	appID, err := parseOperatedAppID(r.PathValue("operatedAppId"))
	if err != nil {
		writeClientError(w, err)
		return
	}
	var req deployRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !c.authorizeOperatedApp(w, r, "operate-system", appID.String()) {
		return
	}
	change := operations.DesiredStateChange{
		Reason:               operations.ReasonDeployAfterConstruction,
		PatchKind:            operations.PatchFullBundle,
		ChangeID:             req.ChangeID,
		RenderedDesiredState: req.RenderedDesiredState,
	}
	result, err := c.operations.DeployAfterConstruction(operationsCtx(r), appID, change)
	if err != nil {
		writeManagerError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, deployResultResponse{
		OperatedAppID: appID.String(),
		Published:     result.Published,
		Revision:      derefString(result.Revision),
	})
}

// handleScale — operateDeliveredSystem{Scale}. Routes to the SAME
// operationsManager.DeployAfterConstruction (Workflow) — a desired-state republish with
// reason=operator and a scale patch (the frozen §2.6 factor-up).
func (c *Client) handleScale(w http.ResponseWriter, r *http.Request) {
	appID, err := parseOperatedAppID(r.PathValue("operatedAppId"))
	if err != nil {
		writeClientError(w, err)
		return
	}
	var req scaleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !c.authorizeOperatedApp(w, r, "operate-system", appID.String()) {
		return
	}
	change := operations.DesiredStateChange{
		Reason:               operations.ReasonOperator,
		PatchKind:            operations.PatchScale,
		ChangeID:             req.ChangeID,
		RenderedDesiredState: req.ScalePatch,
	}
	result, err := c.operations.DeployAfterConstruction(operationsCtx(r), appID, change)
	if err != nil {
		writeManagerError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, deployResultResponse{
		OperatedAppID: appID.String(),
		Published:     result.Published,
		Revision:      derefString(result.Revision),
	})
}

// handleUpdateAutoscalerPolicy — operateDeliveredSystem{UpdateAutoscalerPolicy}. Routes
// to the SAME operationsManager.DeployAfterConstruction (Workflow) — a desired-state
// republish with reason=operator and a policy patch (the frozen §2.6 factor-up).
func (c *Client) handleUpdateAutoscalerPolicy(w http.ResponseWriter, r *http.Request) {
	appID, err := parseOperatedAppID(r.PathValue("operatedAppId"))
	if err != nil {
		writeClientError(w, err)
		return
	}
	var req updateAutoscalerPolicyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !c.authorizeOperatedApp(w, r, "operate-system", appID.String()) {
		return
	}
	change := operations.DesiredStateChange{
		Reason:               operations.ReasonOperator,
		PatchKind:            operations.PatchPolicy,
		ChangeID:             req.ChangeID,
		RenderedDesiredState: req.PolicyPatch,
	}
	result, err := c.operations.DeployAfterConstruction(operationsCtx(r), appID, change)
	if err != nil {
		writeManagerError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, deployResultResponse{
		OperatedAppID: appID.String(),
		Published:     result.Published,
		Revision:      derefString(result.Revision),
	})
}

// handleWithdraw — operateDeliveredSystem{Withdraw}. Routes to
// operationsManager.WithdrawSystem (Workflow). 200 OK with the WithdrawResult once the
// withdrawal is durably recorded.
func (c *Client) handleWithdraw(w http.ResponseWriter, r *http.Request) {
	appID, err := parseOperatedAppID(r.PathValue("operatedAppId"))
	if err != nil {
		writeClientError(w, err)
		return
	}
	var req withdrawRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !c.authorizeOperatedApp(w, r, "operate-system", appID.String()) {
		return
	}
	result, err := c.operations.WithdrawSystem(operationsCtx(r), appID, req.ChangeID,
		operations.WithdrawReason{Notes: req.Reason})
	if err != nil {
		writeManagerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, withdrawResultResponse{
		OperatedAppID: appID.String(),
		Withdrawn:     result.Withdrawn,
	})
}

// handleQueryCostProjection — operateDeliveredSystem{QueryCostProjection}. Routes to
// operationsManager.QueryCostProjection (Workflow; read-only, side-effect-free). The
// requestId is the operator-supplied continuity token (a GET query param keying the
// short-lived read workflow {operatedAppId}:costProjection:{requestId}); the optional
// scaleWhatIfPoints query param ("r1,r2,...") asks for the what-if cost curve.
func (c *Client) handleQueryCostProjection(w http.ResponseWriter, r *http.Request) {
	appID, err := parseOperatedAppID(r.PathValue("operatedAppId"))
	if err != nil {
		writeClientError(w, err)
		return
	}
	requestID := r.URL.Query().Get("requestId")
	if requestID == "" {
		writeClientError(w, fmt.Errorf("requestId query parameter is required"))
		return
	}
	points, err := parseScaleWhatIfPoints(r.URL.Query().Get("scaleWhatIfPoints"))
	if err != nil {
		writeClientError(w, err)
		return
	}
	if !c.authorizeOperatedApp(w, r, "read-operated-system", appID.String()) {
		return
	}
	projection, err := c.operations.QueryCostProjection(operationsCtx(r), appID, requestID, points)
	if err != nil {
		writeManagerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, costProjectionFromManager(appID, projection))
}

// handleGetOperatedSystemView — readOperatedSystemView (Contract 2, IWebReadModel;
// operationsRead-ruling.md §C). Routes to operationsManager.QueryOperatedSystemView
// (Workflow; read-only, side-effect-free). IDENTICAL in shape to handleQueryCostProjection:
// the requestId is the operator-supplied continuity token (a GET query param keying the
// short-lived read workflow {operatedAppId}:view:{requestId}); the SAME read verb
// "read-operated-system" queryCostProjection uses authorizes it. 200 OK with the
// composed operator view shaped onto the explicitly-tagged operatedSystemViewResponse.
func (c *Client) handleGetOperatedSystemView(w http.ResponseWriter, r *http.Request) {
	appID, err := parseOperatedAppID(r.PathValue("operatedAppId"))
	if err != nil {
		writeClientError(w, err)
		return
	}
	requestID := r.URL.Query().Get("requestId")
	if requestID == "" {
		writeClientError(w, fmt.Errorf("requestId query parameter is required"))
		return
	}
	if !c.authorizeOperatedApp(w, r, "read-operated-system", appID.String()) {
		return
	}
	view, err := c.operations.QueryOperatedSystemView(operationsCtx(r), appID, requestID)
	if err != nil {
		writeManagerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, operatedSystemViewFromManager(appID, view))
}

// --- security gate ---------------------------------------------------------

// authorizeOperatedApp runs the cross-cutting pre-step every operate handler shares:
// read the principal the auth middleware validated onto the context, then authorize the
// intent on the operatedApp resource. Mirrors authorizeProject (handlers.go) but targets
// the "operatedApp" resource kind (operate intents are operated-app-scoped, not
// project-scoped). Returns true to proceed; on failure it writes the response (401
// authN-absent / 403 deny) and returns false. FAIL-CLOSED: a security error is treated
// as deny.
func (c *Client) authorizeOperatedApp(w http.ResponseWriter, r *http.Request, verb, operatedAppID string) bool {
	principal, ok := security.PrincipalFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return false
	}
	decision, err := c.security.Authorize(r.Context(), principal,
		security.Action{Verb: verb},
		security.ResourceRef{Kind: "operatedApp", ID: operatedAppID})
	if err != nil || !decision.Permit {
		writeError(w, http.StatusForbidden, "forbidden", "not permitted")
		return false
	}
	return true
}

// --- transport mappings ----------------------------------------------------

// parseOperatedAppID parses the wire operatedAppId (a UUID string). An empty/invalid
// value is a client error (the Manager would also reject uuid.Nil as ContractMisuse).
func parseOperatedAppID(s string) (operations.OperatedAppID, error) {
	if s == "" {
		return uuid.Nil, fmt.Errorf("operatedAppId is required")
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, fmt.Errorf("operatedAppId is not a valid UUID: %w", err)
	}
	return id, nil
}

// parseScaleWhatIfPoints maps the optional "scaleWhatIfPoints" query param (a
// comma-separated list of replica counts, e.g. "1,3,5") onto the typed ScaleWhatIfPoints.
// An empty value means "no what-if curve requested" (nil). A non-integer entry is a
// transport-level client error (clean 400).
func parseScaleWhatIfPoints(s string) (*operations.ScaleWhatIfPoints, error) {
	if s == "" {
		return nil, nil
	}
	var points []operations.ScalePoint
	cur := 0
	have := false
	sawDigit := false
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if !sawDigit {
				return nil, fmt.Errorf("scaleWhatIfPoints must be a comma-separated list of replica counts, e.g. \"1,3,5\"")
			}
			points = append(points, operations.ScalePoint{Replicas: int64(cur)})
			cur = 0
			sawDigit = false
			have = true
			continue
		}
		d := s[i]
		if d < '0' || d > '9' {
			return nil, fmt.Errorf("scaleWhatIfPoints %q must be a comma-separated list of replica counts, e.g. \"1,3,5\"", s)
		}
		cur = cur*10 + int(d-'0')
		sawDigit = true
	}
	if !have {
		return nil, nil
	}
	return &operations.ScaleWhatIfPoints{Points: points}, nil
}

// derefString returns the pointed-to string, or "" for nil. DeployResult.Revision is
// an optional (`,omitempty` ⇒ *string in the generated contract); the wire DTO carries
// it as a plain omitempty string.
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// costProjectionFromManager shapes the Manager's CostProjection (untagged seam types)
// onto the explicitly-tagged wire DTO.
func costProjectionFromManager(appID operations.OperatedAppID, p operations.CostProjection) costProjectionResponse {
	curve := make([]whatIfPointDTO, 0, len(p.ScaleWhatIfCurve.Points))
	for _, pt := range p.ScaleWhatIfCurve.Points {
		curve = append(curve, whatIfPointDTO{
			Replicas:             int(pt.Replicas),
			ProjectedMonthlyCost: moneyFromManager(pt.ProjectedMonthlyCost),
		})
	}
	return costProjectionResponse{
		OperatedAppID:        appID.String(),
		CurrentRunRate:       moneyFromManager(p.CurrentRunRate),
		ProjectedMonthlyCost: moneyFromManager(p.ProjectedMonthlyCost),
		ScaleWhatIfCurve:     curve,
	}
}

// moneyFromManager shapes the Manager's Money seam onto the tagged wire DTO.
func moneyFromManager(m operations.Money) moneyDTO {
	return moneyDTO{MinorUnits: m.MinorUnits, Currency: m.Currency}
}

// operatedSystemViewFromManager shapes the Manager's OperatedSystemView (untagged seam
// types + seam enums) onto the explicitly-tagged wire DTO (operationsRead-ruling.md §C).
// The enums are rendered onto stable lowercase wire strings via the …Name(...) mappers;
// the time fields onto RFC3339.
func operatedSystemViewFromManager(appID operations.OperatedAppID, v operations.OperatedSystemView) operatedSystemViewResponse {
	slos := make([]sloRowDTO, 0, len(v.Slos))
	for _, s := range v.Slos {
		slos = append(slos, sloRowDTO{
			Component: s.Component,
			Objective: s.Objective,
			SloMet:    s.SloMet,
			Healthy:   s.Healthy,
		})
	}
	events := make([]runtimeStatusEventDTO, 0, len(v.RecentEvents))
	for _, e := range v.RecentEvents {
		events = append(events, runtimeStatusEventDTO{
			At:   e.At.Format(time.RFC3339),
			From: runtimeStatusName(e.From),
			To:   runtimeStatusName(e.To),
			Note: e.Note,
		})
	}
	decisions := make([]autoscaleDecisionDTO, 0, len(v.Autoscaler.Decisions))
	for _, d := range v.Autoscaler.Decisions {
		decisions = append(decisions, autoscaleDecisionDTO{
			At:        d.At.Format(time.RFC3339),
			Action:    autoscaleActionName(d.Action),
			Reason:    d.Reason,
			Published: d.Published,
		})
	}
	return operatedSystemViewResponse{
		OperatedAppID: appID.String(),
		Phase:         runtimeStatusName(v.Phase),
		InFlight:      v.InFlight,
		Health: healthSnapshotDTO{
			SloMet: v.Health.SloMet,
			Detail: v.Health.Detail,
			Phase:  runtimeStatusName(v.Health.Phase),
		},
		Slos:         slos,
		RecentEvents: events,
		Autoscaler: autoscalerDTO{
			Mode:      autoscalerModeName(v.Autoscaler.Mode),
			Decisions: decisions,
		},
		CurrentRunRate: moneyFromManager(v.CurrentRunRate),
	}
}

// runtimeStatusName renders operations.RuntimeStatusSeam onto a stable lowercase wire
// string (mirrors constructionStageName). Unknown ⇒ "unknown".
func runtimeStatusName(s operations.RuntimeStatusSeam) string {
	switch s {
	case operations.RuntimeStatusPending:
		return "pending"
	case operations.RuntimeStatusHealthy:
		return "healthy"
	case operations.RuntimeStatusDegraded:
		return "degraded"
	case operations.RuntimeStatusWithdrawn:
		return "withdrawn"
	default:
		return "unknown"
	}
}

// autoscalerModeName renders operations.AutoscalerMode onto a stable lowercase wire
// string. Unknown ⇒ "unknown".
func autoscalerModeName(m operations.AutoscalerMode) string {
	switch m {
	case operations.AutoscalerModeAuto:
		return "auto"
	case operations.AutoscalerModeManual:
		return "manual"
	default:
		return "unknown"
	}
}

// autoscaleActionName renders operations.AutoscaleAction onto a stable lowercase wire
// string. Unknown ⇒ "unknown".
func autoscaleActionName(a operations.AutoscaleAction) string {
	switch a {
	case operations.AutoscaleNoChange:
		return "noChange"
	case operations.AutoscaleScaleUp:
		return "scaleUp"
	case operations.AutoscaleScaleDown:
		return "scaleDown"
	case operations.AutoscalePause:
		return "pause"
	case operations.AutoscaleResume:
		return "resume"
	default:
		return "unknown"
	}
}
