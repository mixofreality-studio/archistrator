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

/**
 * Throws the ApiError for any non-2xx response, and does nothing for a 2xx.
 *
 * The STATUS decides, never the parsed body (fix-D review I2). openapi-fetch
 * 0.14.1 sets `error` only when it parsed an error body. For an empty one
 * (Content-Length 0) it returns `error: undefined`, and that is what a proxy's
 * 502/503/504 looks like. Testing `error !== undefined` counted those as success.
 */
export function throwUnlessOk(response: Response, error: WireError | undefined): void {
  if (!response.ok) throw toApiError(response.status, error);
}

/**
 * The parsed body of a 2xx response, or the ApiError for anything else.
 *
 * The status decides, as in throwUnlessOk. A 2xx that carries NO body where the
 * operation owes one is an error too: create-project answered that way used to
 * hand `undefined` on as the new project's id, and the next call went to
 * set-operating-model/undefined and reported success. For an operation that
 * returns nothing, use throwUnlessOk.
 */
export function bodyUnlessError<T>(result: {
  data?: T;
  error?: WireError | undefined;
  response: Response;
}): T {
  throwUnlessOk(result.response, result.error);
  if (result.data === undefined) {
    throw new ApiError(
      result.response.status,
      'empty_body',
      `the response (status ${String(result.response.status)}) carried no body`
    );
  }
  return result.data;
}
