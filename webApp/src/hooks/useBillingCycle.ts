/**
 * Reads one billing period's recorded settlement (billing/get-billing-cycle —
 * billingManager.GetBillingCycle, use case view-a-billing-cycle). A plain read: no
 * polling, because a period only changes when the billing schedule closes it. A 404
 * (no such customer or period) is surfaced without retries.
 */
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { useOpsClient } from '../api/opsContext';
import type { OpResult } from '../api/opTypes';
import { ApiError } from '../contracts/errors';
import { mapBillingCycle, type BillingCycle } from '../contracts/billing';

export function billingCycleKey(customerId: string, cycleId: string): readonly unknown[] {
  return ['billingCycle', customerId, cycleId];
}

/** Enabled only once both ids are given; `undefined` ids mean "nothing asked yet". */
export function useBillingCycle(
  customerId: string | undefined,
  cycleId: string | undefined
): UseQueryResult<BillingCycle> {
  const { ops } = useOpsClient();
  return useQuery<BillingCycle>({
    queryKey: billingCycleKey(customerId ?? '', cycleId ?? ''),
    queryFn: async () => {
      const data = await ops.callForBody<OpResult<'billingGetBillingCycle'>>(
        'billingGetBillingCycle',
        {
          path: { customerID: customerId ?? '' },
          query: { cycleID: cycleId ?? '' },
        }
      );
      return mapBillingCycle(data);
    },
    enabled: (customerId ?? '') !== '' && (cycleId ?? '') !== '',
    retry: (count, error) => !(error instanceof ApiError && error.status === 404) && count < 1,
  });
}
