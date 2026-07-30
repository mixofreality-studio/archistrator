/**
 * TanStack Query wrapper over system-design/get-design-health — the render-on-read
 * Design Health model (Wave-2 reshape 3, step-8 replacement): live methodcheck
 * findings + committed waivers/attestations + the evaluated head-state revision.
 * Disabled until a projectId is present so callers can mount unconditionally.
 *
 * Rides the transport-blind OpsClient (like useProject / useSessionState) rather than
 * the REST apiClient directly, so the same hook serves both the standalone SPA (REST)
 * and the MCP-hosted app (server tool calls over the host bridge).
 */
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { useOpsClient } from '../api/opsContext';
import { mapDesignHealth } from '../contracts/wire';
import type { DesignHealth } from '../contracts/types';
import type { components } from '../contracts/schema';

export function designHealthKey(projectId: string): readonly unknown[] {
  return ['designHealth', projectId];
}

export function useDesignHealth(projectId: string): UseQueryResult<DesignHealth> {
  const { ops } = useOpsClient();
  return useQuery<DesignHealth>({
    queryKey: designHealthKey(projectId),
    queryFn: async () => {
      const data = await ops.call<components['schemas']['SystemDesignDesignHealth']>(
        'systemDesignGetDesignHealth',
        { path: { projectID: projectId } }
      );
      return mapDesignHealth(data);
    },
    enabled: projectId.length > 0,
  });
}
