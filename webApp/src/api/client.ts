/**
 * The typed UC1 API client (openapi-fetch over the generated schema). This is the
 * sole IO surface of the browser SPA: main.tsx hands it to restOpsClient
 * (ops.gen.ts), and every call, the composition routes included, goes through
 * that OpsClient. Error contracts live in `contracts/errors`.
 *
 * The composition-root routes (capabilities, the operated-app id, /api/userinfo)
 * used to be raw fetches here and in utilities/auth/UserContext.tsx. They are now
 * bound in OP_BINDINGS (scripts/composition-routes.mjs), so no request bypasses
 * the OpsClient seam and the preview build's fixture transport can answer all of
 * them (design-renderer-data.md §2′.0 P2).
 *
 * Auth: the SPA attaches NO token. The Envoy edge authenticates the browser
 * (session cookie) and forwards the validated access token to the server (GTD
 * parity). Same-origin requests carry the edge cookie automatically, so the
 * client just issues plain fetches.
 */
import createClient from 'openapi-fetch';
import type { paths } from '../contracts/schema';
import { config } from '../utilities/config.ts';

export const apiClient = createClient<paths>({ baseUrl: config.apiBaseUrl });
