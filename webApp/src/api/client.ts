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

export const apiClient = createClient<paths>({ baseUrl: config.apiBaseUrl });
