/**
 * The MCP half of the op table names tools the server REALLY registers (preview P1b).
 *
 * Before P1b the tool was derived from the path, and two ops were bound to tools
 * that never existed: /project-design/request-sdp-commit and
 * /submit-sdp-decision are `projectDesignRequestSDPCommit` and
 * `projectDesignSubmitSDPDecision` on the server (mcpemit names a tool
 * camel(mgr) + operationId). An MCP-hosted app calling either got a tool error,
 * not the op.
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
  assert.ok(tools.size >= 40, `read ${String(tools.size)} tools`);
  for (const [opId, binding] of Object.entries(opBindings(doc))) {
    if (binding.tool !== null) assert.ok(tools.has(binding.tool), `${opId} → ${binding.tool}`);
  }
});

test('every server tool is bound to some op (no tool is unreachable)', () => {
  const bound = new Set(Object.values(opBindings(doc)).map((b) => b.tool));
  assert.deepEqual(
    [...tools].filter((t) => !bound.has(t)),
    []
  );
});

test('the SDP ops bind the tools the server names', () => {
  const b = opBindings(doc);
  assert.equal(b.projectDesignRequestSdpCommit.tool, 'projectDesignRequestSDPCommit');
  assert.equal(b.projectDesignSubmitSdpDecision.tool, 'projectDesignSubmitSDPDecision');
});

test('an OAS op the server registers no tool for is bound tool: null (refused loudly over MCP)', () => {
  const without = new Set(tools);
  without.delete('constructionExecuteNextActivity');
  const b = deriveBindings(doc, without);
  assert.equal(b.constructionExecuteNextActivity.tool, null);
  assert.equal('composition' in b.constructionExecuteNextActivity, false, 'still an OAS op');
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
