/**
 * UC4 operations write mutations. deploy / scale / autoscaler-policy are now ONE
 * route — operations/deploy-after-construction — distinguished by the desired-state
 * change's (reason, patchKind) discriminator:
 *   - deploy            → reason=DeployAfterConstruction, patchKind=FullBundle
 *   - scale             → reason=Operator,                patchKind=Scale
 *   - autoscaler-policy → reason=Operator,                patchKind=Policy
 * withdraw is operations/withdraw-system. Each mints a fresh changeId and
 * invalidates the operations view so the console re-reads fresh server state.
 */
import { useMutation, useQueryClient, type UseMutationResult } from '@tanstack/react-query';
import { apiClient, toApiError } from '../api/client';
import type { components } from '../api/schema';
import {
  REASON_DEPLOY_AFTER_CONSTRUCTION,
  REASON_OPERATOR,
  PATCH_FULL_BUNDLE,
  PATCH_SCALE,
  PATCH_POLICY,
} from '../api/enums';
import type { DeployResult, WithdrawResult } from '../api/operationsTypes';
import { operationsViewKey } from './useOperationsView';

/** The kinds of desired-state republish the console can trigger. */
export type OperationActionKind = 'deploy' | 'scale' | 'autoscaler-policy';

function changeFor(
  kind: OperationActionKind,
  changeId: string
): components['schemas']['OperationsDesiredStateChange'] {
  switch (kind) {
    case 'scale':
      return { changeId, reason: REASON_OPERATOR, patchKind: PATCH_SCALE };
    case 'autoscaler-policy':
      return { changeId, reason: REASON_OPERATOR, patchKind: PATCH_POLICY };
    case 'deploy':
    default:
      return { changeId, reason: REASON_DEPLOY_AFTER_CONSTRUCTION, patchKind: PATCH_FULL_BUNDLE };
  }
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
