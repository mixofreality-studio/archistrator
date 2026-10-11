/**
 * Containers-layer wiring for the billing-period lookup (use case
 * view-a-billing-cycle): owns which period was asked for and its read
 * (useBillingCycle — billingManager.GetBillingCycle), and hands the pure
 * `BillingCycleLookup` (components layer) a settled result to render.
 */
import { useState, type ReactNode } from 'react';
import { useBillingCycle } from '../hooks/useBillingCycle';
import {
  BillingCycleLookup,
  type BillingCycleLookupResult,
} from '../components/BillingCycleLookup';
import { ApiError } from '../contracts/errors';

export function BillingCycleLookupContainer(): ReactNode {
  const [asked, setAsked] = useState<{ customerId: string; cycleId: string } | undefined>();
  const cycle = useBillingCycle(asked?.customerId, asked?.cycleId);

  let result: BillingCycleLookupResult | undefined;
  if (asked === undefined) {
    result = undefined;
  } else if (cycle.data !== undefined) {
    result = { kind: 'read', cycle: cycle.data };
  } else if (cycle.isError) {
    result =
      cycle.error instanceof ApiError && cycle.error.status === 404
        ? { kind: 'notFound' }
        : { kind: 'failed', message: cycle.error.message };
  } else {
    result = { kind: 'pending' };
  }

  return (
    <BillingCycleLookup
      result={result}
      onLookUp={(customerId, cycleId) => {
        setAsked({ customerId, cycleId });
      }}
    />
  );
}
