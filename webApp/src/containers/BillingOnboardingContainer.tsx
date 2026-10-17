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
import { useOperatedAppId } from '../hooks/useOperatedAppId';
import { BillingOnboarding, type OnboardingStepResult } from '../components/BillingOnboarding';

function stepResult<V>(
  m: UseMutationResult<BillingRef, Error, V>
): OnboardingStepResult | undefined {
  if (m.isPending) return { kind: 'pending' };
  if (m.data !== undefined) return { kind: 'done', customerId: m.data.customerId };
  if (m.error !== null) return { kind: 'failed', message: m.error.message };
  return undefined;
}

/**
 * @param projectId The project this billing page belongs to: step 2's "billed for"
 *   project, and — through its derived operated-app id — step 1's deployed app, so
 *   neither has to be typed from memory (both stay editable).
 */
export function BillingOnboardingContainer({ projectId }: { projectId: string }): ReactNode {
  const onboard = useOnboardPaymentIntegration();
  const register = useRegisterCustomer();
  const operatedAppId = useOperatedAppId(projectId);
  return (
    <BillingOnboarding
      defaultAppId={operatedAppId}
      defaultProjectId={projectId}
      onboard={stepResult(onboard)}
      register={stepResult(register)}
      onOnboard={(deployedAppId) => {
        onboard.mutate(deployedAppId);
      }}
      onRegister={(customerId, projectId) => {
        register.mutate({ customerId, projectId });
      }}
    />
  );
}
