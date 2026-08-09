/**
 * Reads the live deployment-diagram health overlay (operations/query-deployment-
 * health, op 2.9, Task 10). Presentation-only: the server has already decided
 * which diagram nodes are even archistrator's to grade (QueryDeploymentHealth
 * walks the cloud environment and marks every node it doesn't deploy Neutral), so
 * this hook does no deriving of its own — it just decodes the wire's three-state
 * OperationsHealthState ordinal into the diagram overlay's two colourable states
 * and DROPS Neutral (ordinal 0) entirely rather than carrying it forward. A
 * modelKey absent from the returned map is exactly the "no colour" input
 * deploymentHealth.ts's healthColor/healthColorName already treat as neutral —
 * whether that's because the server never observed the key or because it
 * explicitly reported Neutral collapses to the same thing here.
 *
 * CONVENTION (founder ruling, 2026-08-08): an operated app's id IS its project's
 * id. An operated app is the deployed instance of one project's system, and
 * today there is exactly one per project, so callers pass the project's own id
 * as `operatedAppId` — there is no separate lookup verb or stored correlation.
 * This is a default that can be relaxed, not a load-bearing identity: if a
 * project ever needs more than one deployment, a real lookup gets added then,
 * the first deployment keeps its id (== the project id, unchanged), and any
 * additional ones take fresh ids. Do not treat the two ids as coincidentally
 * equal — they are equal BY this convention, and a future divergence (a real
 * operatedAppId <> projectId mapping) is the intended relaxation path, not a
 * bug to route around silently.
 *
 * Runtime health can change between polls (a rollout settling, a pod restarting),
 * so this refetches on the same cadence useOperationsView.ts's runtime poll uses
 * rather than caching forever like useCapabilities does.
 */
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { apiClient } from '../api/client';
import { toApiError } from '../contracts/errors';
import type { HealthState } from '../contracts/types';
import { deploymentHealthQueryEnabled } from './deploymentHealthEnabled';

/** Reconcile cadence — matches useOperationsView's runtime-observation poll. */
const POLL_INTERVAL_MS = 30_000;

// OperationsHealthState ordinals (schema.ts's OperationsNodeHealth.Health):
// HealthStateNeutral = 0, HealthStateHealthy = 1, HealthStateUnhealthy = 2.
const HEALTH_ORDINAL_HEALTHY = 1;
const HEALTH_ORDINAL_UNHEALTHY = 2;

export function deploymentHealthKey(operatedAppId: string): readonly unknown[] {
  return ['deploymentHealth', operatedAppId];
}

/**
 * @param operatedAppId The operated app to read live health for — by convention,
 *   the project's own id (see the module doc above). Empty stays dormant.
 * @param capabilityEnabled Whether the operations capability is on (D9); the
 *   caller reads this from useCapabilities().operations.
 */
export function useDeploymentHealth(
  operatedAppId: string,
  capabilityEnabled = true
): UseQueryResult<Record<string, HealthState>> {
  return useQuery<Record<string, HealthState>>({
    queryKey: deploymentHealthKey(operatedAppId),
    queryFn: async () => {
      const { data, error, response } = await apiClient.GET(
        '/api/v1/operations/query-deployment-health/{operatedAppID}',
        { params: { path: { operatedAppID: operatedAppId } } }
      );
      if (error !== undefined) throw toApiError(response.status, error);
      const byKey: Record<string, HealthState> = {};
      for (const node of data.Nodes ?? []) {
        if (node.Health === HEALTH_ORDINAL_HEALTHY) byKey[node.ModelKey] = 'Healthy';
        else if (node.Health === HEALTH_ORDINAL_UNHEALTHY) byKey[node.ModelKey] = 'Unhealthy';
      }
      return byKey;
    },
    enabled: deploymentHealthQueryEnabled(capabilityEnabled, operatedAppId),
    refetchInterval: POLL_INTERVAL_MS,
  });
}
