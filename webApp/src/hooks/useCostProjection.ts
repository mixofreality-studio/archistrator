/**
 * Reads the UC4 read-only op-time cost projection (operations/query-cost-projection,
 * a POST that carries the requestID + the optional scale what-if points). Pure read
 * — no state mutation. Mints a fresh requestID per fetch and asks for the scale
 * what-if curve (a replica list). A 404 is surfaced without retry storms.
 */
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { useOpsClient } from '../api/opsContext';
import type { OpBody, OpResult } from '../api/opTypes';
import { ApiError } from '../contracts/errors';
import { mapCostProjection } from '../contracts/wire';
import type { CostProjection } from '../contracts/operationsTypes';

/** The default scale what-if curve points the console requests. */
export const DEFAULT_WHATIF_POINTS: readonly number[] = [1, 3, 5, 8];

export function costProjectionKey(
  operatedAppId: string,
  points: readonly number[]
): readonly unknown[] {
  return ['costProjection', operatedAppId, points.join(',')];
}

export function useCostProjection(
  operatedAppId: string,
  points: readonly number[] = DEFAULT_WHATIF_POINTS,
  enabled = true
): UseQueryResult<CostProjection> {
  const { ops } = useOpsClient();
  return useQuery<CostProjection>({
    queryKey: costProjectionKey(operatedAppId, points),
    queryFn: async () => {
      const data = await ops.callForBody<OpResult<'operationsQueryCostProjection'>>(
        'operationsQueryCostProjection',
        {
          path: { operatedAppID: operatedAppId },
          body: {
            requestID: crypto.randomUUID(),
            points: { points: points.map((replicas) => ({ replicas })) },
          } satisfies OpBody<'operationsQueryCostProjection'>,
        }
      );
      return mapCostProjection(operatedAppId, data);
    },
    enabled: enabled && operatedAppId.length > 0,
    retry: (count, error) => !(error instanceof ApiError && error.status === 404) && count < 1,
    staleTime: 60_000,
  });
}
