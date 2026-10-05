/**
 * Containers-layer wiring for payment onboarding (use case onboard-a-new-customer,
 * spec §12 B7): owns the two billingManager writes (useBillingOnboarding) and hands
 * the pure `BillingOnboarding` (components layer) each step's settled result.
 */
import type { ReactNode } from 'react';
import type { UseMutationResult } from '@tanstack/react-query';
import {
  useOnboardPaymentIntegration,
  useRegisterCustomer,
  type BillingRef,
} from '../hooks/useBillingOnboarding';
import { BillingOnboarding, type OnboardingStepResult } from '../components/BillingOnboarding';

function stepResult(
  m: UseMutationResult<BillingRef, Error, string>
): OnboardingStepResult | undefined {
  if (m.isPending) return { kind: 'pending' };
  if (m.data !== undefined) return { kind: 'done', customerId: m.data.customerId };
  if (m.error !== null) return { kind: 'failed', message: m.error.message };
  return undefined;
}

export function BillingOnboardingContainer(): ReactNode {
  const onboard = useOnboardPaymentIntegration();
  const register = useRegisterCustomer();
  return (
    <BillingOnboarding
      onboard={stepResult(onboard)}
      register={stepResult(register)}
      onOnboard={(deployedAppId) => {
        onboard.mutate(deployedAppId);
      }}
      onRegister={(customerId) => {
        register.mutate(customerId);
      }}
    />
  );
}
