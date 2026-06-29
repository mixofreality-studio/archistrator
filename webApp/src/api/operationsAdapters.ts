/**
 * Pure adapters mapping the UC4 operations wire view (api/operations.ts) into
 * render-ready view models the Operations console consumes. No React here. Every
 * function is total and resilient to an absent / awaiting view (the operated app
 * may not be deployed yet, or the read may be quiet) — it returns a safe value
 * rather than throwing. The UC4 twin of constructionAdapters.ts.
 */
import type { Money, OperationsView } from './operationsTypes';

/**
 * The operated RuntimePhase palette — the closed set the read view emits
 * (operatedRuntimeAccess §3). Unknown is the NORMAL just-published transient
 * (converging), visually distinct from Degraded.
 */
export type RuntimePhase =
  | 'Unknown'
  | 'Pending'
  | 'Running'
  | 'Degraded'
  | 'Paused'
  | 'Withdrawn';

const KNOWN_PHASES: ReadonlySet<string> = new Set<RuntimePhase>([
  'Unknown',
  'Pending',
  'Running',
  'Degraded',
  'Paused',
  'Withdrawn',
]);

/** Coerce an arbitrary wire phase string into the known set (defaulting to Unknown). */
export function normalizePhase(phase: string | undefined): RuntimePhase {
  return phase !== undefined && KNOWN_PHASES.has(phase) ? (phase as RuntimePhase) : 'Unknown';
}

/** A converging phase is "working on it", NOT danger — drives the pulsing dot. */
export function phaseIsConverging(phase: RuntimePhase): boolean {
  return phase === 'Unknown' || phase === 'Pending';
}

/** The at-a-glance SLO rollup over the view's slos. */
export interface SloSummary {
  total: number;
  healthy: number;
  breaching: number;
}

export function sloSummary(view: OperationsView | undefined): SloSummary {
  const slos = view?.slos ?? [];
  const total = slos.length;
  const breaching = slos.filter((s) => !s.sloMet).length;
  return { total, healthy: total - breaching, breaching };
}

/** Format a Money value (minor → major units) as a localized currency string. */
export function formatMoney(m: Money | undefined): string {
  if (m === undefined) return '—';
  const major = m.minorUnits / 100;
  try {
    return new Intl.NumberFormat(undefined, {
      style: 'currency',
      currency: m.currency.length > 0 ? m.currency : 'USD',
      maximumFractionDigits: major % 1 === 0 ? 0 : 2,
    }).format(major);
  } catch {
    return `${major.toFixed(2)} ${m.currency}`;
  }
}

/**
 * Format an RFC3339 timestamp as a compact local time, falling back to the raw
 * string if it is not a parseable date (the read is honest about absent data).
 */
export function formatEventTime(at: string): string {
  const d = new Date(at);
  if (Number.isNaN(d.getTime())) return at;
  return d.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

/**
 * Whether the view carries any live runtime data at all (vs the awaiting state).
 * A 404 surfaces as an undefined view upstream; an empty view with no SLOs / no
 * events / Unknown phase is the quiet answer the console renders as awaiting.
 */
export function viewIsLive(view: OperationsView | undefined): boolean {
  if (view === undefined) return false;
  return (
    view.slos.length > 0 ||
    view.recentEvents.length > 0 ||
    view.autoscaler.decisions.length > 0 ||
    normalizePhase(view.phase) !== 'Unknown'
  );
}
