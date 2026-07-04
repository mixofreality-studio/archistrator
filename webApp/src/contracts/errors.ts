/**
 * App-facing API error contracts. Pure data/types — no IO, no client. Lives in the
 * `contracts` layer so any layer (routes, components, hooks) may consume it, while the
 * IO client itself (`api/client.ts`) stays quarantined to the `hooks` layer.
 */

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

/**
 * The per-manager error envelopes are byte-identical ({ code, error }); the SPA
 * treats them uniformly via this structural shape.
 */
export interface WireError {
  code?: string;
  error?: string;
}

/**
 * Normalizes an openapi-fetch error envelope into an ApiError. Every manager's
 * *ErrorResponse ({ error, code }) is the documented failure shape.
 */
export function toApiError(status: number, error: WireError | undefined): ApiError {
  const code = error?.code ?? 'internal';
  const detail = error?.error ?? `request failed with status ${String(status)}`;
  return new ApiError(status, code, detail);
}
