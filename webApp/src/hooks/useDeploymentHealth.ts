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
 * Runtime health can change between polls (a rollout settling, a pod restarting),
 * so this refetches on the same cadence useOperationsView.ts's runtime poll uses
 * rather than caching forever like useCapabilities does.
 */
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { apiClient } from '../api/client';
import { toApiError } from '../contracts/errors';
import type { HealthState } from '../contracts/types';

/** Reconcile cadence — matches useOperationsView's runtime-observation poll. */
const POLL_INTERVAL_MS = 30_000;

// OperationsHealthState ordinals (schema.ts's OperationsNodeHealth.Health):
// HealthStateNeutral = 0, HealthStateHealthy = 1, HealthStateUnhealthy = 2.
const HEALTH_ORDINAL_HEALTHY = 1;
const HEALTH_ORDINAL_UNHEALTHY = 2;

export function deploymentHealthKey(operatedAppId: string): readonly unknown[] {
  return ['deploymentHealth', operatedAppId];
}

export function useDeploymentHealth(
  operatedAppId: string,
  enabled = true
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
    enabled: enabled && operatedAppId.length > 0,
    refetchInterval: POLL_INTERVAL_MS,
  });
}
