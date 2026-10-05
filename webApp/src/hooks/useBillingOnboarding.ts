/**
 * Payment onboarding (use case onboard-a-new-customer, spec §12 B7): the two client
 * entries billingManager exposes for it.
 *
 *   - billing/onboard-payment-integration — OnboardPaymentIntegration(deployedAppId):
 *     resolves the deployed app to its billing customer, binds the gateway and
 *     registers the settlement-cycle schedule;
 *   - billing/register-customer — RegisterCustomer(customerId): validates the stored
 *     payment instrument (a zero-amount authorization) and opens the customer's
 *     billing record.
 *
 * Both are idempotent on their id server-side (a repeat joins the run already under
 * way) and both answer the customer's BillingRef.
 */
import { useMutation, type UseMutationResult } from '@tanstack/react-query';
import { useOpsClient } from '../api/opsContext';
import type { OpResult } from '../api/opTypes';

/** The customer a billing write resolved to. */
export interface BillingRef {
  customerId: string;
}

export function useOnboardPaymentIntegration(): UseMutationResult<BillingRef, Error, string> {
  const { ops } = useOpsClient();
  return useMutation<BillingRef, Error, string>({
    mutationFn: async (deployedAppId) => {
      const data = await ops.callForBody<OpResult<'billingOnboardPaymentIntegration'>>(
        'billingOnboardPaymentIntegration',
        { path: { deployedAppID: deployedAppId } }
      );
      return { customerId: data.customerId };
    },
  });
}

export function useRegisterCustomer(): UseMutationResult<BillingRef, Error, string> {
  const { ops } = useOpsClient();
  return useMutation<BillingRef, Error, string>({
    mutationFn: async (customerId) => {
      const data = await ops.callForBody<OpResult<'billingRegisterCustomer'>>(
        'billingRegisterCustomer',
        { path: { customerID: customerId } }
      );
      return { customerId: data.customerId };
    },
  });
}
