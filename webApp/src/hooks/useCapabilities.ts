/**
 * TanStack Query wrapper over GET /api/v1/capabilities (operations-argocd-
 * deployment Task 11, spec D9). The fetch itself lives in the api layer
 * (src/api/client.ts's fetchCapabilities — a composition-root-only route with
 * no .serviceContracts entry, so it isn't in the generated typed apiClient);
 * re-exported here so routes/router.tsx's operations-route `beforeLoad` guard
 * can reuse the SAME call without importing the api layer directly (the
 * layer DAG only lets `hooks` reach `api` — see eslint.platform.config.js).
 *
 * Returns `data` directly (not the full UseQueryResult): while loading or on
 * any fetch error, TanStack Query's `data` is `undefined`, which is exactly
 * the SAFE "hidden" input operationsEnabled(undefined) expects — no separate
 * loading/error branch for callers to get wrong.
 */
import { useQuery } from '@tanstack/react-query';
import { fetchCapabilities } from '../api/client';
import type { Capabilities } from '../utilities/capabilities';

export { fetchCapabilities };

export function capabilitiesKey(): readonly unknown[] {
  return ['capabilities'];
}

export function useCapabilities(): Capabilities | undefined {
  const { data } = useQuery<Capabilities>({
    queryKey: capabilitiesKey(),
    queryFn: fetchCapabilities,
    // The active profile never changes for the life of a session — no reason
    // to ever refetch once a good read lands.
    staleTime: Number.POSITIVE_INFINITY,
  });
  return data;
}
