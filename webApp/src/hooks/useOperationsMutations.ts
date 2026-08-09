/**
 * UC4 operations write mutations. deploy is operations/deploy-after-construction
 * (reason=DeployAfterConstruction, patchKind=FullBundle); withdraw is
 * operations/withdraw-system. Each mints a fresh changeId and invalidates the
 * operations view so the console re-reads fresh server state.
 *
 * SCALE AND AUTOSCALER-POLICY ARE NOT SUPPORTED IN THIS SLICE (2026-08-08 final
 * review, fix 3) and are deliberately absent from OperationActionKind rather than
 * present-but-failing. They used to send (reason=Operator, patchKind=Scale|Policy),
 * which the server accepted and then published a zero-value desired state for —
 * inert under the old opaque-bytes contract, but under the renderer either a
 * confusing ContractMisuse from deep inside the ResourceAccess or a half-rendered
 * deployment. The server now rejects both patch kinds by name at the operations
 * façade; the console must not be able to construct the request at all.
 *
 * They are not faked instead: replicas come from the project's deployment model, so
 * an operator scale override needs a real replica-override path threaded through
 * desired-state assembly — the same missing seam that keeps autoscale-pause and
 * delinquency-pause from publishing anything. That is a capability to build, not a
 * button to wire.
 */
import { useMutation, useQueryClient, type UseMutationResult } from '@tanstack/react-query';
import { apiClient } from '../api/client';
import { toApiError } from '../contracts/errors';
import type { components } from '../contracts/schema';
import { REASON_DEPLOY_AFTER_CONSTRUCTION, PATCH_FULL_BUNDLE } from '../contracts/wire';
import type { DeployResult, WithdrawResult } from '../contracts/operationsTypes';
import { operationsViewKey } from './useOperationsView';

/**
 * The kinds of desired-state republish the console can trigger. One today — see the
 * module doc for why scale and autoscaler-policy are absent rather than disabled
 * here.
 */
export type OperationActionKind = 'deploy';

/** The one publishable change: a full-bundle republish of the current bundle. */
export const UNSUPPORTED_ACTION_REASON =
  'Not supported yet: replicas come from the project’s deployment model, and there is no operator override path through desired-state assembly. Deploy republishes the current bundle.';

function changeFor(
  _kind: OperationActionKind,
  changeId: string
): components['schemas']['OperationsDesiredStateChange'] {
  return { changeId, reason: REASON_DEPLOY_AFTER_CONSTRUCTION, patchKind: PATCH_FULL_BUNDLE };
}

/** Deploy / scale / autoscaler-policy — a desired-state republish. */
export function useOperationAction(
  operatedAppId: string
): UseMutationResult<DeployResult, Error, OperationActionKind> {
  const client = useQueryClient();
  return useMutation<DeployResult, Error, OperationActionKind>({
    mutationFn: async (kind) => {
      const { data, error, response } = await apiClient.POST(
        '/api/v1/operations/deploy-after-construction/{operatedAppID}',
        {
          params: { path: { operatedAppID: operatedAppId } },
          body: { change: changeFor(kind, crypto.randomUUID()) },
        }
      );
      if (error !== undefined) throw toApiError(response.status, error);
      return {
        operatedAppId,
        published: data.published,
        ...(data.revision !== undefined && data.revision !== null
          ? { revision: data.revision }
          : {}),
      };
    },
    onSuccess: () => client.invalidateQueries({ queryKey: operationsViewKey(operatedAppId) }),
  });
}

export interface WithdrawVars {
  reason?: string;
}

/** Terminal withdraw of the operated app. */
export function useWithdrawOperatedApp(
  operatedAppId: string
): UseMutationResult<WithdrawResult, Error, WithdrawVars> {
  const client = useQueryClient();
  return useMutation<WithdrawResult, Error, WithdrawVars>({
    mutationFn: async (vars) => {
      const { data, error, response } = await apiClient.POST(
        '/api/v1/operations/withdraw-system/{operatedAppID}',
        {
          params: { path: { operatedAppID: operatedAppId } },
          body: { changeID: crypto.randomUUID(), reason: { notes: vars.reason ?? '' } },
        }
      );
      if (error !== undefined) throw toApiError(response.status, error);
      return { operatedAppId, withdrawn: data.withdrawn };
    },
    onSuccess: () => client.invalidateQueries({ queryKey: operationsViewKey(operatedAppId) }),
  });
}
