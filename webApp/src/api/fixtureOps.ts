/**
 * The FIXTURE transport: the third OpsClient, beside restOpsClient (the browser
 * SPA) and mcpOpsClient (the MCP-hosted app), both in ops.gen.ts. It answers
 * every op from fixture data and never touches the network. Only the preview
 * build (src/previewShell/) uses it; `npm run build` asserts the production
 * bundle never contains it (scripts/check-prod-bundle.mjs).
 *
 * Design: .superpowers/sdd/2026-09-12-deterministic-activity-derivation/
 * design-renderer-data.md §2′.1. Each op's fixture is exactly one of:
 *
 *   - `{ result }`       the call resolves with a fresh deep copy of `result`;
 *   - `{ error }`        the call rejects with the real ApiError, so the app
 *                        shows its ordinary error handling;
 *   - `{ pending: true }` the call never settles, which holds a real loading state.
 *
 * A mutation answers the same way and changes no state: the fixture map is
 * never written, and every result is a copy. An Approve click in a preview runs
 * the real handler and the real toast, and changes nothing anywhere.
 *
 * A call with NO fixture is LOUD: it rejects with FixtureMissError (an ApiError
 * with status 500, never 404, because several hooks read a 404 as "nothing
 * here" and would hide the miss) and reports the op to `onMiss`.
 *
 * EARMARK (design §2′.1, P2): this runtime moves into framework-web, and its
 * fixture types into app-generator's webgen output.
 */
import { ApiError } from '../contracts/errors.ts';
import { OP_BINDINGS, type OpId, type OpParams, type OpsClient } from './ops.gen.ts';

/** The wire error a fixture injects. `status` decides, as it does on the wire. */
export interface FixtureError {
  readonly status: number;
  readonly code?: string;
  readonly message?: string;
}

/** One op's canned answer. */
export type FixtureOp =
  | { readonly result: unknown }
  | { readonly error: FixtureError }
  | { readonly pending: true };

/** Every op a fixture answers, keyed by OpId. */
export type FixtureOps = Readonly<Partial<Record<OpId, FixtureOp>>>;

/** One screen state's fixture file (uitests/preview-fixtures/<surface>/<screen>/<state>.json). */
export interface FixtureFile {
  /** The concrete route the state opens at, e.g. "/project/archistrator/construction". */
  readonly route: string;
  readonly title?: string;
  readonly note?: string;
  readonly ops: FixtureOps;
}

/** The code a miss carries; scripts/check-prod-bundle.mjs greps for the class name. */
export const FIXTURE_MISS_CODE = 'fixture_miss';

/** A call whose op has no fixture. Rejects loudly; never reads as "not found". */
export class FixtureMissError extends ApiError {
  readonly opId: OpId;

  constructor(opId: OpId) {
    const binding = OP_BINDINGS[opId];
    super(
      500,
      FIXTURE_MISS_CODE,
      `FixtureMissError: no fixture answers ${opId} (${binding.method} ${binding.path})`
    );
    this.name = 'FixtureMissError';
    this.opId = opId;
  }
}

export interface FixtureOpsOptions {
  /** Told about every miss, before the call rejects. The preview shell raises its alarm. */
  readonly onMiss?: (opId: OpId, params: OpParams) => void;
}

function fixtureFor(fixtures: FixtureOps, op: OpId): FixtureOp | undefined {
  return Object.prototype.hasOwnProperty.call(fixtures, op) ? fixtures[op] : undefined;
}

export function fixtureOpsClient(fixtures: FixtureOps, options: FixtureOpsOptions = {}): OpsClient {
  const call = <R = unknown>(op: OpId, params: OpParams = {}): Promise<R> => {
    const fixture = fixtureFor(fixtures, op);
    if (fixture === undefined) {
      options.onMiss?.(op, params);
      return Promise.reject(new FixtureMissError(op));
    }
    if ('pending' in fixture) {
      return new Promise<R>(() => {
        // Never settles: the state under preview IS the loading state.
      });
    }
    if ('error' in fixture) {
      const { status, code, message } = fixture.error;
      return Promise.reject(
        new ApiError(
          status,
          code ?? 'internal',
          message ?? `request failed with status ${String(status)}`
        )
      );
    }
    return Promise.resolve(structuredClone(fixture.result) as R);
  };
  // A fixture's `result` is JSON, so it always carries a body: `callForBody`
  // answers exactly as `call` does (the REST transport's empty-2xx refusal has
  // no fixture equivalent).
  return { call, callForBody: call };
}
