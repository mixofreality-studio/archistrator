/**
 * TanStack Query wrapper over GET /api/v1/capabilities (operations-argocd-
 * deployment Task 11, spec D9). The route is a composition-root route with no
 * .serviceContracts entry; it is bound in OP_BINDINGS as
 * `compositionGetCapabilities` (scripts/composition-routes.mjs), so it rides the
 * OpsClient like every other op and a preview's fixture transport can answer it.
 *
 * fetchCapabilities is exported for routes/router.tsx's operations-route
 * `beforeLoad` guard, which runs outside React: the shell that builds the router
 * (main.tsx, previewShell) passes it in through the router context with its own
 * OpsClient. The layer DAG only lets `hooks` reach `api` (eslint.platform.config.js).
 *
 * useCapabilities returns `data` directly (not the full UseQueryResult): while
 * loading or on any fetch error, TanStack Query's `data` is `undefined`, which is
 * exactly the SAFE "hidden" input operationsEnabled(undefined) expects — no
 * separate loading/error branch for callers to get wrong.
 */
import { useQuery } from '@tanstack/react-query';
import { useOpsClient } from '../api/opsContext';
import type { OpsClient } from '../api/ops.gen';
import type { Capabilities } from '../utilities/capabilities.ts';

/** Reads the active profile's capabilities through `ops`. */
export function fetchCapabilities(ops: OpsClient): Promise<Capabilities> {
  return ops.call<Capabilities>('compositionGetCapabilities');
}

export function capabilitiesKey(): readonly unknown[] {
  return ['capabilities'];
}

export function useCapabilities(): Capabilities | undefined {
  const { ops } = useOpsClient();
  const { data } = useQuery<Capabilities>({
    queryKey: capabilitiesKey(),
    queryFn: () => fetchCapabilities(ops),
    // The active profile never changes for the life of a session — no reason
    // to ever refetch once a good read lands.
    staleTime: Number.POSITIVE_INFINITY,
  });
  return data;
}
