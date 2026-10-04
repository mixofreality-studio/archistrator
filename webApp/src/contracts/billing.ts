/**
 * The billing-cycle view: billingManager.GetBillingCycle (use case
 * view-a-billing-cycle, web + MCP entry) — the recorded settlement of one billing
 * period. Pure; no React.
 */
import type { components } from './schema.ts';
import { ROUTING_DIRECTIVE_ORDINAL_TO_APP, type RoutingDirective } from './enums.gen.ts';

export type BillingCycleWire = components['schemas']['BillingBillingCycleView'];

export interface BillingCycle {
  customerId: string;
  cycleId: string;
  /** The period has been closed and its outcome recorded. */
  closed: boolean;
  /** How the period's net routed; undefined for an ordinal this build cannot name. */
  routed: RoutingDirective | undefined;
  /** A declined charge escalated the customer to delinquency (invoice unpaid). */
  escalated: boolean;
}

/** The outcome the view returns: the period is still open, or how it settled. */
export type BillingCycleOutcome = 'open' | 'charged' | 'nothingOwed' | 'unpaid' | 'unrecognised';

/** Maps the wire view; `routed` is a sparse ordinal ([0, 2]) read by VALUE. */
export function mapBillingCycle(w: BillingCycleWire): BillingCycle {
  return {
    customerId: w.customerId,
    cycleId: w.cycleId,
    closed: w.closed,
    routed: ROUTING_DIRECTIVE_ORDINAL_TO_APP[w.routed],
    escalated: w.escalated,
  };
}

export function billingCycleOutcome(c: BillingCycle): BillingCycleOutcome {
  if (!c.closed) {
    return 'open';
  }
  if (c.escalated) {
    return 'unpaid';
  }
  switch (c.routed) {
    case 'charge':
      return 'charged';
    case 'noAction':
      return 'nothingOwed';
    case undefined:
      return 'unrecognised';
  }
}
