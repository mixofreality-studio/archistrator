/**
 * Polls the UC4 operated-app runtime view (operations/query-operated-system-view).
 * Runtime status is infra-OBSERVED at a reconcile tick (not pushed), so the view is
 * refetched on an interval (~the reconcile cadence). Each poll mints a fresh
 * requestID (the read's short-lived continuity token). A 404 (no such operated app
 * / not deployed) is surfaced WITHOUT retry storms so the console can render its
 * awaiting state.
 */
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { apiClient, ApiError, toApiError } from '../api/client';
import { mapOperationsView } from '../api/wire';
import type { OperationsView } from '../api/operationsTypes';

/** Reconcile cadence — runtime status is observed roughly every ~30s. */
const POLL_INTERVAL_MS = 30_000;

export function operationsViewKey(operatedAppId: string): readonly unknown[] {
  return ['operationsView', operatedAppId];
}

export function useOperationsView(
  operatedAppId: string,
  enabled = true
): UseQueryResult<OperationsView> {
  return useQuery<OperationsView>({
    queryKey: operationsViewKey(operatedAppId),
    queryFn: async () => {
      const { data, error, response } = await apiClient.GET(
        '/api/v1/operations/query-operated-system-view/{operatedAppID}',
        {
          params: {
            path: { operatedAppID: operatedAppId },
            query: { requestID: crypto.randomUUID() },
          },
        }
      );
      if (error !== undefined) throw toApiError(response.status, error);
      return mapOperationsView(data);
    },
    enabled: enabled && operatedAppId.length > 0,
    // A 404 means "no such operated app yet" — surface without retries.
    retry: (count, error) => !(error instanceof ApiError && error.status === 404) && count < 1,
    refetchInterval: POLL_INTERVAL_MS,
  });
}
