/**
 * Runtime configuration, read once from Vite's import.meta.env.
 *
 * Auth is handled at the Envoy edge (GTD parity): the edge runs the Keycloak
 * OIDC redirect login, sets the session cookie, and forwards the validated
 * access token to the server. The SPA never runs its own OIDC flow — it only
 * probes GET /api/userinfo (see auth/UserContext). `authMode` exists solely to
 * surface a clearly-labelled DEV badge when running without the edge in front:
 *   - "dev":      no edge; the server injects a dev principal (env-gated).
 *   - "keycloak": real edge-OIDC deployment.
 */

export type AuthMode = 'dev' | 'keycloak';

export interface AppConfig {
  readonly apiBaseUrl: string;
  readonly authMode: AuthMode;
}

function readString(value: string | undefined, fallback: string): string {
  const trimmed = value?.trim();
  return trimmed !== undefined && trimmed.length > 0 ? trimmed : fallback;
}

function readAuthMode(value: string | undefined): AuthMode {
  return value?.trim() === 'keycloak' ? 'keycloak' : 'dev';
}

export const config: AppConfig = {
  // Origin prefix BEFORE the "/api/..." paths. Default "" = same origin (dev:
  // Vite proxies /api → server; prod: Envoy routes /api/*). Set to a full origin
  // only for cross-origin dev against a remote server.
  apiBaseUrl: readString(import.meta.env.VITE_API_BASE_URL, ''),
  authMode: readAuthMode(import.meta.env.VITE_AUTH_MODE),
};
