/**
 * The operated app id a project's deployment carries (spec D13, corrected in the
 * 2026-08-08 final review).
 *
 * D13's ruling is that the identity needs NO lookup: one operated app per project,
 * derived from the project id, nothing stored to correlate them. Its first mechanism
 * was literal equality — pass the projectId straight through as the operatedAppId —
 * which the types cannot support: a project id is a free-form string ("archistrator")
 * and an operatedAppId is a uuid, so the overlay fired
 * `GET .../query-deployment-health/archistrator` every 30 seconds and got 400 forever.
 *
 * The derivation is now a real one (UUIDv5 over a fixed namespace and the project id)
 * and lives server-side, in ONE place (server/cmd/server/hooks.go's
 * OperatedAppIDForProject). This hook reads it rather than reproducing it: hand-rolling
 * UUIDv5 in the browser would be a second implementation of a rule with one authority.
 *
 * The read is pure — the same project always answers the same id, and the answer does
 * not depend on anything being deployed yet — so it is cached for the session like
 * useCapabilities, not polled.
 *
 * The route is a composition-root route, bound in OP_BINDINGS as
 * `compositionGetOperatedAppId` (scripts/composition-routes.mjs), so it rides the
 * OpsClient and a preview's fixture transport can answer it.
 */
import { useQuery } from '@tanstack/react-query';
import { useOpsClient } from '../api/opsContext';

/** The wire shape GET /api/v1/projects/{projectId}/operated-app-id answers. */
export interface OperatedAppIdResponse {
  readonly operatedAppId: string;
}

export function operatedAppIdKey(projectId: string): readonly unknown[] {
  return ['operatedAppId', projectId];
}

/**
 * @param projectId The project whose deployment's id is wanted. Empty stays dormant
 *   (no route param yet / still loading) and returns undefined.
 * @returns The derived operated app id, or undefined while loading or on a failed read
 *   — the same "dormant" input every consumer already treats as nothing to query.
 */
export function useOperatedAppId(projectId: string): string | undefined {
  const { ops } = useOpsClient();
  const { data } = useQuery({
    queryKey: operatedAppIdKey(projectId),
    queryFn: () =>
      ops.call<OperatedAppIdResponse>('compositionGetOperatedAppId', {
        path: { projectID: projectId },
      }),
    enabled: projectId !== '',
    staleTime: Number.POSITIVE_INFINITY,
  });
  return data?.operatedAppId;
}
