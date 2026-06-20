/**
 * UC4 operations (operateDeliveredSystem) API service — one typed function per
 * server op over the operated-app-scoped routes
 * (server/internal/client/web/operations.go + handlers.go). operatedAppId is
 * always a PATH param. The UC4 TWIN of api/construction.ts.
 *
 * Like the construction routes, the operations routes are NOT in the generated
 * OpenAPI schema (the codegen spec stops at advanceToConstruction). So these
 * functions issue same-origin fetches directly against config.apiBaseUrl and
 * hand-mirror the Go wire DTOs below — the field names match operations.go
 * (deployResultResponse / costProjectionResponse / moneyDTO / whatIfPointDTO) and
 * the operations-view read DTO (operationsViewResponse, built concurrently)
 * EXACTLY.
 *
 * The /view READ is poll-driven, not push: runtime status is infra-observed at a
 * reconcile tick (~30s), so the console refetches on an interval. The WRITE ops
 * (deploy / scale / autoscaler-policy / withdraw) and the read-only
 * cost-projection are already live.
 */
import { ApiError } from './client';
import { config } from '../config';

// --- money (shared wire shape: operations.go moneyDTO) ---------------------

/** operations.go moneyDTO — exact integer minor units + ISO-4217 currency. */
export interface Money {
  /** Signed minor units, e.g. 129900 == 1299.00. */
  minorUnits: number;
  /** ISO-4217, e.g. "USD". */
  currency: string;
}

// --- /view read DTOs (hand-mirrored from the operations-view read endpoint) -

/** One SLO posture row (operationsViewResponse.slos[]). */
export interface OperationsSlo {
  component: string;
  objective: string;
  sloMet: boolean;
  healthy: boolean;
}

/** The runtime health rollup (operationsViewResponse.health). */
export interface OperationsHealth {
  sloMet: boolean;
  detail: string;
  /** RuntimePhase string — Unknown | Pending | Running | Degraded | Paused | Withdrawn. */
  phase: string;
}

/** One RuntimeStatusChanged health-timeline event (operationsViewResponse.recentEvents[]). */
export interface OperationsEvent {
  /** RFC3339 timestamp. */
  at: string;
  from: string;
  to: string;
  note: string;
}

/** One autoscaler decision (operationsViewResponse.autoscaler.decisions[]). */
export interface AutoscalerDecision {
  /** RFC3339 timestamp. */
  at: string;
  action: string;
  reason: string;
  published: boolean;
}

/** The autoscaler view (operationsViewResponse.autoscaler). */
export interface AutoscalerView {
  /** "Auto" | "Manual". */
  mode: string;
  decisions: AutoscalerDecision[];
}

/** The whole operations view (operationsViewResponse) — one operated app, poll-read. */
export interface OperationsView {
  operatedAppId: string;
  /** RuntimePhase rollup string. */
  phase: string;
  /** A desired-state republish is in flight / converging. */
  inFlight: boolean;
  health: OperationsHealth;
  slos: OperationsSlo[];
  recentEvents: OperationsEvent[];
  autoscaler: AutoscalerView;
  /** Extrapolated run-rate at current observed load (same money shape as cost-projection). */
  currentRunRate: Money;
}

// --- cost-projection read DTOs (operations.go costProjectionResponse) -------

/** One what-if scale level + its projected monthly cost (operations.go whatIfPointDTO). */
export interface WhatIfPoint {
  replicas: number;
  projectedMonthlyCost: Money;
}

/** The read-only cost projection (operations.go costProjectionResponse). */
export interface CostProjection {
  operatedAppId: string;
  currentRunRate: Money;
  projectedMonthlyCost: Money;
  scaleWhatIfCurve: WhatIfPoint[];
}

// --- write result DTOs (operations.go deployResultResponse / withdrawResultResponse) ---

export interface DeployResult {
  operatedAppId: string;
  published: boolean;
  revision?: string;
}

export interface WithdrawResult {
  operatedAppId: string;
  withdrawn: boolean;
}

// --- raw transport ---------------------------------------------------------

interface WireErrorResponse {
  error?: string;
  code?: string;
}

async function readError(response: Response): Promise<ApiError> {
  let body: WireErrorResponse | undefined;
  try {
    body = (await response.json()) as WireErrorResponse;
  } catch {
    body = undefined;
  }
  const code = body?.code ?? 'internal';
  const detail = body?.error ?? `request failed with status ${String(response.status)}`;
  return new ApiError(response.status, code, detail);
}

function url(path: string): string {
  return `${config.apiBaseUrl}${path}`;
}

function appPath(operatedAppId: string, suffix: string): string {
  return url(`/api/v1/operations/${encodeURIComponent(operatedAppId)}${suffix}`);
}

// --- read ops --------------------------------------------------------------

/**
 * GET /operations/{operatedAppId}/view — poll the operated app's runtime view
 * (operateDeliveredSystem read). requestId is an operator-supplied continuity
 * token (a fresh UUID per poll cycle is fine — it keys the short-lived read). A
 * 404 means "no such operated app / not deployed yet" — callers surface it as an
 * awaiting state, not a hard error.
 */
export async function getOperationsView(
  operatedAppId: string,
  requestId: string
): Promise<OperationsView> {
  const response = await fetch(
    appPath(operatedAppId, `/view?requestId=${encodeURIComponent(requestId)}`),
    { credentials: 'include', headers: { Accept: 'application/json' } }
  );
  if (!response.ok) throw await readError(response);
  return (await response.json()) as OperationsView;
}

/**
 * GET /operations/{operatedAppId}/cost-projection — the read-only op-time cost
 * projection (operateDeliveredSystem{QueryCostProjection}). requestId is required
 * (the handler 400s on an empty requestId). scaleWhatIfPoints is an optional
 * comma-separated replica list ("1,3,5") asking for the what-if curve.
 */
export async function getCostProjection(
  operatedAppId: string,
  requestId: string,
  scaleWhatIfPoints?: number[]
): Promise<CostProjection> {
  const params = new URLSearchParams({ requestId });
  if (scaleWhatIfPoints !== undefined && scaleWhatIfPoints.length > 0) {
    params.set('scaleWhatIfPoints', scaleWhatIfPoints.join(','));
  }
  const response = await fetch(appPath(operatedAppId, `/cost-projection?${params.toString()}`), {
    credentials: 'include',
    headers: { Accept: 'application/json' },
  });
  if (!response.ok) throw await readError(response);
  return (await response.json()) as CostProjection;
}

// --- write ops -------------------------------------------------------------

/**
 * POST /operations/{operatedAppId}/deploy — operateDeliveredSystem{Deploy}. The
 * changeId is the operator-supplied idempotency/continuity token (the Manager
 * rejects an empty changeId). 202 Accepted with the DeployResult.
 */
export async function deployOperatedApp(
  operatedAppId: string,
  changeId: string
): Promise<DeployResult> {
  const response = await fetch(appPath(operatedAppId, '/deploy'), {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ changeId }),
  });
  if (!response.ok) throw await readError(response);
  return (await response.json()) as DeployResult;
}

/**
 * POST /operations/{operatedAppId}/scale — operateDeliveredSystem{Scale}. A
 * manual desired-state republish (reason=operator). 202 Accepted.
 */
export async function scaleOperatedApp(
  operatedAppId: string,
  changeId: string
): Promise<DeployResult> {
  const response = await fetch(appPath(operatedAppId, '/scale'), {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ changeId }),
  });
  if (!response.ok) throw await readError(response);
  return (await response.json()) as DeployResult;
}

/**
 * POST /operations/{operatedAppId}/autoscaler-policy —
 * operateDeliveredSystem{UpdateAutoscalerPolicy}. An autoscaler-policy change
 * republish (reason=operator). 202 Accepted.
 */
export async function updateAutoscalerPolicy(
  operatedAppId: string,
  changeId: string
): Promise<DeployResult> {
  const response = await fetch(appPath(operatedAppId, '/autoscaler-policy'), {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ changeId }),
  });
  if (!response.ok) throw await readError(response);
  return (await response.json()) as DeployResult;
}

/**
 * POST /operations/{operatedAppId}/withdraw — operateDeliveredSystem{Withdraw}.
 * Terminal withdraw. reason is the operator's free-text rationale. 200 OK with
 * the WithdrawResult (an already-withdrawn app is an idempotent success).
 */
export async function withdrawOperatedApp(
  operatedAppId: string,
  changeId: string,
  reason?: string
): Promise<WithdrawResult> {
  const response = await fetch(appPath(operatedAppId, '/withdraw'), {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ changeId, ...(reason !== undefined ? { reason } : {}) }),
  });
  if (!response.ok) throw await readError(response);
  return (await response.json()) as WithdrawResult;
}
