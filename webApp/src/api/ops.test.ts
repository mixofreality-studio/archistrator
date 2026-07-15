/// <reference types="node" />
/**
 * Unit tests for the generated OpsClient (src/api/ops.gen.ts). Run with
 * `npm run test` (Node's built-in test runner over TypeScript via native type
 * stripping; there is no other test framework in the webApp toolchain — see
 * webapp-checks.yml / src/components/design/roleLine.test.ts for the idiom).
 *
 * The app build pins `types: ["vite/client"]`, so this file pulls in the Node
 * runtime type declarations (node:test / node:assert) via the reference above
 * rather than widening the whole project's global types.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { ApiError } from '../contracts/errors.ts';
import { OP_BINDINGS, restOpsClient, mcpOpsClient, type OpsClient } from './ops.gen.ts';

// -- OP_BINDINGS: the mechanically-derived REST-path <-> tool-name table -----

void test('binds systemDesignGetSessionState to its REST path and tool name', () => {
  const b = OP_BINDINGS.systemDesignGetSessionState;
  assert.equal(b.method, 'GET');
  assert.equal(b.path, '/api/v1/system-design/get-session-state/{projectID}');
  assert.equal(b.tool, 'systemDesignGetSessionState');
});

void test('binds a POST op with a compound manager/op name (project-design)', () => {
  const b = OP_BINDINGS.projectDesignSubmitSdpDecision;
  assert.equal(b.method, 'POST');
  assert.equal(b.path, '/api/v1/project-design/submit-sdp-decision/{projectID}/{optionID}');
  assert.equal(b.tool, 'projectDesignSubmitSdpDecision');
});

// -- MCP transport ------------------------------------------------------------

interface Call {
  name: string;
  arguments: Record<string, unknown>;
}

/** Hand-rolled spy standing in for `App.callServerTool` — this repo's test
 * toolchain (node:test) has no vi.fn()/jest.fn() equivalent. */
function spyApp(resolve: (call: Call) => unknown): {
  app: { callServerTool: (params: Call) => Promise<unknown> };
  calls: Call[];
} {
  const calls: Call[] = [];
  return {
    calls,
    app: {
      callServerTool: (params: Call): Promise<unknown> => {
        calls.push(params);
        return Promise.resolve(resolve(params));
      },
    },
  };
}

void test('mcp impl routes through callServerTool and unwraps structuredContent', async () => {
  const { app, calls } = spyApp(() => ({
    structuredContent: { stage: 'drafting' },
    content: [],
  }));
  const ops: OpsClient = mcpOpsClient(app as never);
  const out = await ops.call('systemDesignGetSessionState', {
    path: { projectID: 'p1' },
    query: { kind: 1 },
  });
  assert.deepEqual(calls, [
    { name: 'systemDesignGetSessionState', arguments: { projectID: 'p1', kind: 1 } },
  ]);
  assert.deepEqual(out, { stage: 'drafting' });
});

void test('mcp impl maps the real NotFound manager-error grammar to ApiError(404)', async () => {
  // Byte-exact server grammar: `${Kind.String()}: ${message}` — no space
  // before the colon (see server/internal/manager errors, and gen-ops.mjs's
  // isNotFoundToolError doc comment).
  const { app } = spyApp(() => ({
    isError: true,
    content: [{ type: 'text', text: 'NotFound: no active design session for project "p1"' }],
  }));
  const ops: OpsClient = mcpOpsClient(app as never);
  await assert.rejects(
    ops.call('systemDesignGetSessionState', { path: { projectID: 'p1' }, query: { kind: 1 } }),
    (err: unknown) => {
      assert.ok(err instanceof ApiError);
      assert.equal(err.status, 404);
      return true;
    }
  );
});

void test('mcp impl maps the tolerant "not found" fallback wording to ApiError(404)', async () => {
  const { app } = spyApp(() => ({
    isError: true,
    content: [{ type: 'text', text: 'session not found for project "p1"' }],
  }));
  const ops: OpsClient = mcpOpsClient(app as never);
  await assert.rejects(
    ops.call('systemDesignGetSessionState', { path: { projectID: 'p1' }, query: { kind: 1 } }),
    (err: unknown) => {
      assert.ok(err instanceof ApiError);
      assert.equal(err.status, 404);
      return true;
    }
  );
});

void test('mcp impl maps any other tool error to ApiError(500)', async () => {
  const { app } = spyApp(() => ({
    isError: true,
    content: [{ type: 'text', text: 'boom: database unreachable' }],
  }));
  const ops: OpsClient = mcpOpsClient(app as never);
  await assert.rejects(
    ops.call('systemDesignGetSessionState', { path: { projectID: 'p1' }, query: { kind: 1 } }),
    (err: unknown) => {
      assert.ok(err instanceof ApiError);
      assert.equal(err.status, 500);
      return true;
    }
  );
});

void test('mcp impl rejects with ApiError(500) on a path/body arg-key collision, without calling the tool', async () => {
  const { app, calls } = spyApp(() => ({
    structuredContent: {},
    content: [],
  }));
  const ops: OpsClient = mcpOpsClient(app as never);
  await assert.rejects(
    ops.call('systemDesignGetSessionState', {
      path: { projectID: 'p1' },
      body: { projectID: 'p2' },
    }),
    (err: unknown) => {
      assert.ok(err instanceof ApiError);
      assert.equal(err.status, 500);
      assert.match(err.message, /collision/);
      return true;
    }
  );
  assert.deepEqual(calls, []);
});

// -- REST transport ------------------------------------------------------------

interface RestCall {
  path: string;
  options: unknown;
}

function spyRestClient(resolve: () => unknown): {
  client: Record<string, (path: string, options: unknown) => Promise<unknown>>;
  calls: RestCall[];
} {
  const calls: RestCall[] = [];
  const handler = (path: string, options: unknown): Promise<unknown> => {
    calls.push({ path, options });
    return Promise.resolve(resolve());
  };
  return { calls, client: { GET: handler, POST: handler } };
}

void test('rest impl dispatches to the bound method/path and returns data', async () => {
  const { client, calls } = spyRestClient(() => ({
    data: { stage: 'drafting' },
    error: undefined,
    response: { status: 200 },
  }));
  const ops: OpsClient = restOpsClient(client as never);
  const out = await ops.call('systemDesignGetSessionState', {
    path: { projectID: 'p1' },
    query: { kind: 1 },
  });
  assert.equal(calls.length, 1);
  assert.equal(calls[0]?.path, '/api/v1/system-design/get-session-state/{projectID}');
  assert.deepEqual(out, { stage: 'drafting' });
});

void test('rest impl maps a non-2xx response to ApiError via toApiError', async () => {
  const { client } = spyRestClient(() => ({
    data: undefined,
    error: { code: 'not_found', error: 'no session' },
    response: { status: 404 },
  }));
  const ops: OpsClient = restOpsClient(client as never);
  await assert.rejects(
    ops.call('systemDesignGetSessionState', { path: { projectID: 'p1' }, query: { kind: 1 } }),
    (err: unknown) => {
      assert.ok(err instanceof ApiError);
      assert.equal(err.status, 404);
      assert.equal(err.code, 'not_found');
      return true;
    }
  );
});

test('mcp impl unwraps the generated single-result envelope to match REST (F-T11-4)', async () => {
  const calls: unknown[] = [];
  const app = {
    callServerTool: (req: unknown) => {
      calls.push(req);
      return Promise.resolve({ structuredContent: { result: { stage: 'drafting' } }, content: [] });
    },
  };
  const ops = mcpOpsClient(app as never);
  const out = await ops.call('systemDesignGetSessionState', { path: { projectID: 'p1' }, query: { kind: 0 } });
  assert.deepEqual(out, { stage: 'drafting' });
});

test('mcp impl passes a void-op empty structuredContent through as {}', async () => {
  const app = { callServerTool: () => Promise.resolve({ structuredContent: undefined, content: [] }) };
  const ops = mcpOpsClient(app as never);
  const out = await ops.call('systemDesignSetResearchInput', { path: { projectID: 'p1' }, body: {} });
  assert.deepEqual(out, {});
});
