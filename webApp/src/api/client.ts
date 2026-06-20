/**
 * The typed UC1 API client (openapi-fetch over the generated schema).
 *
 * Auth: the SPA attaches NO token. The Envoy edge authenticates the browser
 * (session cookie) and forwards the validated access token to the server (GTD
 * parity). Same-origin requests carry the edge cookie automatically, so the
 * client just issues plain fetches.
 */
import createClient from 'openapi-fetch';
import type { paths, components } from './schema';
import { config } from '../config';

/** Stable, app-facing error raised when the server returns a non-2xx response. */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
  }
}

export const apiClient = createClient<paths>({ baseUrl: config.apiBaseUrl });

/**
 * Normalizes an openapi-fetch error envelope into an ApiError. The server's
 * ErrorResponse ({ error, code }) is the documented failure shape.
 */
export function toApiError(
  status: number,
  error: components['schemas']['ErrorResponse'] | undefined
): ApiError {
  const code = error?.code ?? 'internal';
  const detail = error?.error ?? `request failed with status ${String(status)}`;
  return new ApiError(status, code, detail);
}
