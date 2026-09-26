/**
 * The MCP half of the op table names tools the server REALLY registers (preview P1b).
 *
 * Before P1b the tool was derived from the path, and two ops were bound to tools
 * that never existed: /project-design/request-sdp-commit and /submit-sdp-decision
 * were `projectDesignRequestSDPCommit` and `projectDesignSubmitSDPDecision` on the
 * server (mcpemit names a tool camel(mgr) + operationId), while the path-derived
 * ids spelled the acronym `Sdp`. An MCP-hosted app calling either got a tool
 * error, not the op.
 *
 * STAGE 4a RETIRED THAT CASE'S SUBJECT: both ops folded into `deliveryDispatchActivityTask`
 * and `deliverySubmitReviewDecision`, and NO surviving op id contains an acronym at
 * all (measured against the emitted table), so the `SDP`-vs-`Sdp` assertions were
 * dropped rather than pointed at an op that cannot exercise them. The GENERAL rule
 * they were an instance of — every binding names a tool the server registers, and
 * every registered tool is reachable — is still pinned by the first two tests, which
 * walk the whole table and are what would catch the next acronym.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdirSync, mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { loadMcpTools } from './mcp-tools.mjs';
import { deriveBindings, opBindings } from './op-bindings.mjs';
import { loadOas } from './fixture-schema.mjs';
import { COMPOSITION_ROUTES } from './composition-routes.mjs';

const doc = loadOas();
const tools = loadMcpTools();

test('the server registers tools, and every bound tool is one of them', () => {
  // Stage 4a: three design/construction managers became one delivery manager, so
  // the floor moved from 40 to 20 — 12 delivery tools + 8 operations tools, the
  // EXACT count, because a blind read that returns nothing must fail here (the
  // last test in this file pins that loadMcpTools throws rather than answering an
  // empty table).
  assert.equal(tools.size, 20, `read ${String(tools.size)} tools`);
  for (const [opId, binding] of Object.entries(opBindings(doc))) {
    if (binding.tool !== null) assert.ok(tools.has(binding.tool), `${opId} → ${binding.tool}`);
  }
});

test('no op id spells an acronym differently from its server tool', () => {
  // What the dropped SDP pair guarded, as a rule instead of an instance: the
  // path-derived opId and the server tool agree wherever the op has one. A future
  // /request-sdp-commit would fail here, on the whole table, rather than needing a
  // hand-written pair.
  for (const [opId, binding] of Object.entries(opBindings(doc))) {
    if (binding.tool === null) continue;
    assert.equal(binding.tool.toLowerCase(), opId.toLowerCase(), opId);
  }
});

test('every server tool is bound to some op (no tool is unreachable)', () => {
  const bound = new Set(Object.values(opBindings(doc)).map((b) => b.tool));
  assert.deepEqual(
    [...tools].filter((t) => !bound.has(t)),
    []
  );
});

test('an OAS op the server registers no tool for is bound tool: null (refused loudly over MCP)', () => {
  const without = new Set(tools);
  without.delete('deliveryExecuteNextActivity');
  const b = deriveBindings(doc, without);
  assert.equal(b.deliveryExecuteNextActivity.tool, null);
  assert.equal('composition' in b.deliveryExecuteNextActivity, false, 'still an OAS op');
});

test('composition routes are tool: null and marked composition', () => {
  const b = opBindings(doc);
  for (const opId of Object.keys(COMPOSITION_ROUTES)) {
    assert.equal(b[opId].tool, null, opId);
    assert.equal(b[opId].composition, true, opId);
  }
});

test('a blind read of the tool tables is an error, never an all-null table', () => {
  assert.throws(() => loadMcpTools(join(tmpdir(), 'no-such-mcp-dir-p1b')), /no generated MCP tool/);
  const empty = mkdtempSync(join(tmpdir(), 'mcp-tools-'));
  mkdirSync(join(empty, 'mgr'));
  assert.throws(() => loadMcpTools(empty), /read no mcp\.AddTool/);
});

test('the committed ops.gen.ts carries exactly this derivation', () => {
  const text = readFileSync(new URL('../src/api/ops.gen.ts', import.meta.url), 'utf8');
  const match = /export const OP_BINDINGS = (\{[\s\S]*?\n\}) as const;/.exec(text);
  assert.ok(match, 'OP_BINDINGS in ops.gen.ts');
  // Prettier writes the object as TS (unquoted keys, single quotes); read it as JS.
  const committed = new Function(`return (${match[1]});`)();
  assert.deepEqual(committed, opBindings(doc), 'run npm run gen:ops');
});
