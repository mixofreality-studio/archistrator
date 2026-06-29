/**
 * App-facing UC4 operations view types (formerly api/operations.ts wire DTOs).
 *
 * These are the SPA's stable view shapes — string runtime phases, camelCase fields.
 * The wire boundary (hooks/wire.ts) maps the generated OperationsOperatedSystemView
 * / OperationsCostProjectionSeam (PascalCase, integer enums) into these.
 */
import type { Money } from './models';

export type { Money };

/** The operated RuntimePhase palette the console renders. */
export type RuntimePhase =
  | 'Unknown'
  | 'Pending'
  | 'Running'
  | 'Degraded'
  | 'Paused'
  | 'Withdrawn';

/** One SLO posture row. */
export interface OperationsSlo {
  component: string;
  objective: string;
  sloMet: boolean;
  healthy: boolean;
}

/** The runtime health rollup. */
export interface OperationsHealth {
  sloMet: boolean;
  detail: string;
  /** RuntimePhase string. */
  phase: string;
}

/** One health-timeline event. */
export interface OperationsEvent {
  /** RFC3339 timestamp. */
  at: string;
  from: string;
  to: string;
  note: string;
}

/** One autoscaler decision. */
export interface AutoscalerDecision {
  /** RFC3339 timestamp. */
  at: string;
  action: string;
  reason: string;
  published: boolean;
}

/** The autoscaler view. */
export interface AutoscalerView {
  /** "Auto" | "Manual" | "Unknown". */
  mode: string;
  decisions: AutoscalerDecision[];
}

/** The whole operations view — one operated app, poll-read. */
export interface OperationsView {
  operatedAppId: string;
  /** RuntimePhase rollup string. */
  phase: string;
  inFlight: boolean;
  health: OperationsHealth;
  slos: OperationsSlo[];
  recentEvents: OperationsEvent[];
  autoscaler: AutoscalerView;
  currentRunRate: Money;
}

/** One what-if scale level + its projected monthly cost. */
export interface WhatIfPoint {
  replicas: number;
  projectedMonthlyCost: Money;
}

/** The read-only cost projection. */
export interface CostProjection {
  operatedAppId: string;
  currentRunRate: Money;
  projectedMonthlyCost: Money;
  scaleWhatIfCurve: WhatIfPoint[];
}

export interface DeployResult {
  operatedAppId: string;
  published: boolean;
  revision?: string;
}

export interface WithdrawResult {
  operatedAppId: string;
  withdrawn: boolean;
}
