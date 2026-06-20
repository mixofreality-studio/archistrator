/**
 * Reads the UC4 read-only op-time cost projection (operateDeliveredSystem
 * {QueryCostProjection}). Pure read — no state mutation. Mints a fresh requestId
 * per fetch (the read's short-lived continuity token) and optionally asks for the
 * scale what-if curve (a replica list). A 404 is surfaced without retry storms.
 */
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { getCostProjection, type CostProjection } from '../api/operations';
import { ApiError } from '../api/client';

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
  return useQuery<CostProjection>({
    queryKey: costProjectionKey(operatedAppId, points),
    queryFn: () => getCostProjection(operatedAppId, crypto.randomUUID(), [...points]),
    enabled: enabled && operatedAppId.length > 0,
    retry: (count, error) => !(error instanceof ApiError && error.status === 404) && count < 1,
    staleTime: 60_000,
  });
}
