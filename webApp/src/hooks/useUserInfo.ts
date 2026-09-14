/**
 * The session probe's IO: GET /api/userinfo through the OpsClient.
 *
 * /api/userinfo is a composition-root route (framework-go's security handler),
 * bound in OP_BINDINGS as `compositionGetUserinfo` (scripts/composition-routes.mjs).
 * It used to be a raw fetch inside utilities/auth/UserContext.tsx, which the layer
 * DAG classifies as `utilities` and so barred from the api layer; that kept it off
 * the seam, and a preview build could not answer it. UserProvider now takes the
 * probe as a prop, and App.tsx supplies this one.
 *
 * A failed probe rejects with the transport's ApiError, so UserProvider can still
 * tell a 401 (reload into the edge's OIDC redirect) from any other failure. The
 * REST transport sends the same `Accept: application/json` header the raw fetch did.
 */
import { useCallback } from 'react';
import { useOpsClient } from '../api/opsContext';
import type { UserInfo } from '../utilities/auth/userInfo';

/** A stable probe for UserProvider's `fetchUser`. */
export function useUserInfoFetcher(): () => Promise<UserInfo> {
  const { ops } = useOpsClient();
  return useCallback(() => ops.call<UserInfo>('compositionGetUserinfo'), [ops]);
}
