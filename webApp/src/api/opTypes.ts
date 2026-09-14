/**
 * Per-op wire types over the OpsClient, derived from the OAS types the REST client
 * compiles against (contracts/schema.ts) through OP_BINDINGS. Type-only.
 *
 * `ops.call` takes an untyped body and returns what the caller names. A hook that
 * moved off apiClient (preview P1b) keeps the compile-time checks apiClient gave
 * it by naming its op here:
 *
 *   ops.callForBody<OpResult<'constructionExecuteNextActivity'>>(
 *     'constructionExecuteNextActivity',
 *     { path: { projectID }, body: { tickID } satisfies OpBody<'constructionExecuteNextActivity'> }
 *   );
 *
 * A composition route (no OAS path) has neither: both resolve to `never`.
 */
import type { paths } from '../contracts/schema';
import type { OP_BINDINGS, OpId } from './ops.gen';

type Binding<K extends OpId> = (typeof OP_BINDINGS)[K];

type Operation<K extends OpId> = Binding<K>['path'] extends keyof paths
  ? paths[Binding<K>['path']][Lowercase<Binding<K>['method']> & keyof paths[Binding<K>['path']]]
  : never;

/** The op's JSON request body. */
export type OpBody<K extends OpId> =
  Operation<K> extends { requestBody?: { content: { 'application/json': infer B } } } ? B : never;

/** The op's 200 JSON body. */
export type OpResult<K extends OpId> =
  Operation<K> extends { responses: { 200: { content: { 'application/json': infer R } } } }
    ? R
    : never;
