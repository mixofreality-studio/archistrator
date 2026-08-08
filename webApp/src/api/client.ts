/**
 * The typed UC1 API client (openapi-fetch over the generated schema). This is the
 * sole IO surface of the SPA and the only member of the `api` layer — by the layer
 * gate, ONLY `hooks` may import it. Error contracts live in `contracts/errors`.
 *
 * Auth: the SPA attaches NO token. The Envoy edge authenticates the browser
 * (session cookie) and forwards the validated access token to the server (GTD
 * parity). Same-origin requests carry the edge cookie automatically, so the
 * client just issues plain fetches.
 */
import createClient from 'openapi-fetch';
import type { paths } from '../contracts/schema';
import { config } from '../utilities/config';
import type { Capabilities } from '../utilities/capabilities';

export const apiClient = createClient<paths>({ baseUrl: config.apiBaseUrl });

/**
 * Raw fetch for GET /api/v1/capabilities (operations-argocd-deployment Task
 * 11). A composition-root-only route (server/cmd/server/hooks.go's
 * ExtraMounts), not a generated .serviceContracts op, so it has no entry in
 * the typed `paths` apiClient above understands — hand-typed against
 * Capabilities instead. The api layer is the one place a bare fetch is
 * allowed (spec §8.2); hooks/useCapabilities.ts and routes/router.tsx's
 * operations-route `beforeLoad` guard both call this rather than reaching for
 * fetch themselves.
 */
export async function fetchCapabilities(): Promise<Capabilities> {
  const res = await fetch(`${config.apiBaseUrl}/api/v1/capabilities`, {
    headers: { Accept: 'application/json' },
  });
  if (!res.ok) {
    throw new Error(`Failed to load capabilities: ${String(res.status)} ${res.statusText}`);
  }
  return (await res.json()) as Capabilities;
}
