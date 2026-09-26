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
 * ONE op is keyed by op id PLUS a selector: `deliveryQueryProjectView` is the
 * single read that absorbed thirteen per-rail readers (stage 4a), and it answers
 * a different body per `ProjectViewKind`. A fixture keying it by op id alone
 * could hold only ONE of the kinds a screen reads — the plan reads `summary` and
 * `projects`, the landing reads `projects`, an activity reads `summary` and
 * `timeline` — and the second would silently overwrite the first, rendering a
 * plausible and wrong screen. So its entry is a map from kind to answer, and the
 * transport resolves the kind from the call's own body, exactly as the server
 * does. Each kind may answer differently: plan/loading.json holds `summary`
 * PENDING beside `projects` RESULT, which one shared answer cannot express.
 *
 * EARMARK (design §2′.1, P2): this runtime moves into framework-web, and its
 * fixture types into app-generator's webgen output.
 */
import { ApiError } from '../contracts/errors.ts';
import { PROJECT_VIEW_KIND_VALUES, type ProjectViewKind } from '../contracts/enums.gen.ts';
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

/**
 * The one op whose fixture is keyed by op id AND selector (see the module note).
 * A literal, not a derived type: the nesting mirrors a decision about THIS op's
 * contract, and a second such op would need its own selector reader below.
 */
export const VIEW_OP = 'deliveryQueryProjectView';

/** `deliveryQueryProjectView`'s answers, one per `ProjectViewKind` a screen reads. */
export type FixtureViewOps = Readonly<Partial<Record<ProjectViewKind, FixtureOp>>>;

/**
 * Every op a fixture answers, keyed by OpId — and `deliveryQueryProjectView` by
 * its seven view kinds under that key.
 */
export type FixtureOps = Readonly<{
  [K in OpId]?: K extends typeof VIEW_OP ? FixtureViewOps : FixtureOp;
}>;

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
  /**
   * The selector the call carried, where the op has one — the view kind for
   * `deliveryQueryProjectView`, `undefined` for every other op.
   */
  readonly selector: string | undefined;
  /**
   * What is missing, as a reader needs it named: the op id, or `op(selector)`.
   * A selector-keyed op whose OTHER kinds are fixtured would otherwise report
   * "no fixture answers deliveryQueryProjectView" while one plainly exists.
   */
  readonly detail: string;

  constructor(opId: OpId, selector?: string) {
    const binding = OP_BINDINGS[opId];
    const detail = selector === undefined ? opId : `${opId}(${selector})`;
    super(
      500,
      FIXTURE_MISS_CODE,
      `FixtureMissError: no fixture answers ${detail} (${binding.method} ${binding.path})`
    );
    this.name = 'FixtureMissError';
    this.opId = opId;
    this.selector = selector;
    this.detail = detail;
  }
}

export interface FixtureOpsOptions {
  /**
   * Told about every miss, before the call rejects, with the very error the call
   * will reject with. The preview shell raises its alarm on `miss.detail`.
   */
  readonly onMiss?: (miss: FixtureMissError, params: OpParams) => void;
}

/** Own property only: a fixture inherited from a prototype is not a fixture. */
function hasOwn(source: object, key: string): boolean {
  return Object.prototype.hasOwnProperty.call(source, key);
}

/** Whether a selector off the wire is one of the seven kinds. An unknown one is a miss. */
function isViewKind(kind: string): kind is ProjectViewKind {
  return (PROJECT_VIEW_KIND_VALUES as readonly string[]).includes(kind);
}

/**
 * The `ProjectViewKind` a `deliveryQueryProjectView` call selects, read from the
 * call's own body (`{ body: { query: { kind } } }` — the op is a POST and the
 * selector is never a URL query). `undefined` when the caller sent no kind, which
 * the server refuses too; the miss then names the op with no selector.
 */
function viewKindOf(params: OpParams): string | undefined {
  const body: unknown = params.body;
  if (typeof body !== 'object' || body === null || !('query' in body)) return undefined;
  const query: unknown = body.query;
  if (typeof query !== 'object' || query === null || !('kind' in query)) return undefined;
  const kind: unknown = query.kind;
  return typeof kind === 'string' ? kind : undefined;
}

export function fixtureOpsClient(fixtures: FixtureOps, options: FixtureOpsOptions = {}): OpsClient {
  const call = <R = unknown>(op: OpId, params: OpParams = {}): Promise<R> => {
    let selector: string | undefined;
    let fixture: FixtureOp | undefined;
    if (op === VIEW_OP) {
      selector = viewKindOf(params);
      const byKind = hasOwn(fixtures, op) ? fixtures[VIEW_OP] : undefined;
      fixture =
        byKind !== undefined &&
        selector !== undefined &&
        isViewKind(selector) &&
        hasOwn(byKind, selector)
          ? byKind[selector]
          : undefined;
    } else {
      fixture = hasOwn(fixtures, op) ? fixtures[op] : undefined;
    }
    if (fixture === undefined) {
      const miss = new FixtureMissError(op, selector);
      options.onMiss?.(miss, params);
      return Promise.reject(miss);
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
