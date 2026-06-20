/**
 * UC4 operations write mutations: deploy / scale / update-autoscaler-policy /
 * withdraw the operated app. Each mints a fresh changeId (the operator-supplied
 * idempotency token) and invalidates the operations view so the console re-reads
 * fresh server state (never setQueryData). The UC4 twin of
 * useConstructionMutations.ts.
 */
import { useMutation, useQueryClient, type UseMutationResult } from '@tanstack/react-query';
import {
  deployOperatedApp,
  scaleOperatedApp,
  updateAutoscalerPolicy,
  withdrawOperatedApp,
  type DeployResult,
  type WithdrawResult,
} from '../api/operations';
import { operationsViewKey } from './useOperationsView';

/** The kinds of desired-state republish the console can trigger. */
export type OperationActionKind = 'deploy' | 'scale' | 'autoscaler-policy';

function dispatchAction(
  operatedAppId: string,
  kind: OperationActionKind,
  changeId: string
): Promise<DeployResult> {
  switch (kind) {
    case 'deploy':
      return deployOperatedApp(operatedAppId, changeId);
    case 'scale':
      return scaleOperatedApp(operatedAppId, changeId);
    case 'autoscaler-policy':
      return updateAutoscalerPolicy(operatedAppId, changeId);
    default:
      return deployOperatedApp(operatedAppId, changeId);
  }
}

/** Deploy / scale / autoscaler-policy — a desired-state republish. */
export function useOperationAction(
  operatedAppId: string
): UseMutationResult<DeployResult, Error, OperationActionKind> {
  const client = useQueryClient();
  return useMutation<DeployResult, Error, OperationActionKind>({
    mutationFn: (kind) => dispatchAction(operatedAppId, kind, crypto.randomUUID()),
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
    mutationFn: (vars) => withdrawOperatedApp(operatedAppId, crypto.randomUUID(), vars.reason),
    onSuccess: () => client.invalidateQueries({ queryKey: operationsViewKey(operatedAppId) }),
  });
}
