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
  // The tool is the one the SERVER registers (camel(mgr) + operationId), not the
  // path-derived opId: until preview P1b this was bound to a tool that never
  // existed ('projectDesignSubmitSdpDecision'). scripts/mcp-tools.test.mjs pins
  // every binding against the server's tool tables.
  assert.equal(b.tool, 'projectDesignSubmitSDPDecision');
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
    // A real Response: openapi-fetch always hands one back, and its `ok` decides.
    response: new Response(null, { status: 200 }),
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
    // A real Response: openapi-fetch always hands one back, and its `ok` decides.
    response: new Response(null, { status: 404 }),
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

void test('mcp impl unwraps the generated single-result envelope to match REST (F-T11-4)', async () => {
  const calls: unknown[] = [];
  const app = {
    callServerTool: (req: unknown): Promise<unknown> => {
      calls.push(req);
      return Promise.resolve({ structuredContent: { result: { stage: 'drafting' } }, content: [] });
    },
  };
  const ops = mcpOpsClient(app as never);
  const out = await ops.call('systemDesignGetSessionState', {
    path: { projectID: 'p1' },
    query: { kind: 0 },
  });
  assert.deepEqual(out, { stage: 'drafting' });
});

void test('mcp impl passes a void-op empty structuredContent through as {}', async () => {
  const app = {
    callServerTool: (): Promise<unknown> =>
      Promise.resolve({ structuredContent: undefined, content: [] }),
  };
  const ops = mcpOpsClient(app as never);
  const out = await ops.call('systemDesignSetResearchInput', {
    path: { projectID: 'p1' },
    body: {},
  });
  assert.deepEqual(out, {});
});

// -- REST transport: the status decides (fix-E review) --------------------------

/** A fake openapi-fetch client whose every verb answers with `result`. */
function restAnswering(result: { data?: unknown; error?: unknown; response: Response }): {
  client: never;
  calls: string[];
} {
  const calls: string[] = [];
  const answer = (path: string): Promise<typeof result> => {
    calls.push(path);
    return Promise.resolve(result);
  };
  return { client: { GET: answer, POST: answer } as never, calls };
}

const emptyResponse = (status: number): Response =>
  new Response(null, { status, headers: { 'content-length': '0' } });

void test('rest impl: an EMPTY-body 500 on a mutation is an error, not a success', async () => {
  // openapi-fetch returns `error: undefined` for an empty body (a proxy's bare 5xx).
  const { client } = restAnswering({
    data: undefined,
    error: undefined,
    response: emptyResponse(500),
  });
  await assert.rejects(
    restOpsClient(client).call('systemDesignSubmitReviewDecision', {
      path: { projectID: 'p1' },
      body: { kind: 1, decision: 0 },
    }),
    (err: unknown) => {
      assert.ok(err instanceof ApiError);
      assert.equal(err.status, 500);
      assert.equal(err.message, 'request failed with status 500');
      return true;
    }
  );
});

void test('rest impl: an empty-body 404 GET is ApiError(404), which the session probe reads as "no session"', async () => {
  const { client } = restAnswering({
    data: undefined,
    error: undefined,
    response: emptyResponse(404),
  });
  await assert.rejects(
    restOpsClient(client).call('systemDesignGetSessionState', {
      path: { projectID: 'p1' },
      query: { kind: 1 },
    }),
    (err: unknown) => err instanceof ApiError && err.status === 404
  );
});

void test('rest impl: an empty-body 502 GET says "status 502", not a TypeError from a mapper', async () => {
  const { client } = restAnswering({
    data: undefined,
    error: undefined,
    response: emptyResponse(502),
  });
  await assert.rejects(
    restOpsClient(client).call('systemDesignGetProject', { path: { projectID: 'p1' } }),
    (err: unknown) => err instanceof ApiError && err.message === 'request failed with status 502'
  );
});

void test('rest impl: a 2xx hands its data back, and an error body keeps its code', async () => {
  const ok = restAnswering({ data: { stage: 1 }, response: new Response('{}', { status: 200 }) });
  assert.deepEqual(
    await restOpsClient(ok.client).call('systemDesignGetProject', { path: { projectID: 'p1' } }),
    { stage: 1 }
  );
  assert.deepEqual(ok.calls, ['/api/v1/system-design/get-project/{projectID}']);
  const refused = restAnswering({
    error: { code: 'failed_precondition', error: 'no research input' },
    response: new Response('{}', { status: 409 }),
  });
  await assert.rejects(
    restOpsClient(refused.client).call('systemDesignStartSystemDesign', {
      path: { projectID: 'p1' },
    }),
    (err: unknown) =>
      err instanceof ApiError && err.code === 'failed_precondition' && err.status === 409
  );
});

// -- callForBody: bodyUnlessError's semantics on the REST transport (preview P1b) --

void test('rest callForBody: a 2xx hands its data back; the request is exactly what call sends', async () => {
  const seen: RestCall[] = [];
  const answer = (path: string, options: unknown): Promise<unknown> => {
    seen.push({ path, options });
    return Promise.resolve({ data: 'p-1', response: new Response('"p-1"', { status: 200 }) });
  };
  const ops = restOpsClient({ GET: answer, POST: answer } as never);
  const params = { path: { projectID: 'p1' }, body: { acknowledgeStale: false } };
  assert.equal(await ops.callForBody('projectDesignAdvanceToConstruction', params), 'p-1');
  await ops.call('projectDesignAdvanceToConstruction', params);
  assert.equal(seen.length, 2);
  assert.deepEqual(seen[0], seen[1], 'the same request either way');
  assert.deepEqual(seen[0], {
    path: '/api/v1/project-design/advance-to-construction/{projectID}',
    options: { params: { path: { projectID: 'p1' }, query: undefined }, body: params.body },
  });
});

void test('rest callForBody: a 2xx with NO body is ApiError(<status>, empty_body), as bodyUnlessError says', async () => {
  for (const status of [200, 204]) {
    const { client } = restAnswering({
      data: undefined,
      response: status === 204 ? new Response(null, { status }) : emptyResponse(status),
    });
    await assert.rejects(
      restOpsClient(client).callForBody('systemDesignCreateProject', { body: {} }),
      (err: unknown) => {
        assert.ok(err instanceof ApiError);
        assert.equal(err.status, status);
        assert.equal(err.code, 'empty_body');
        assert.equal(err.message, `the response (status ${String(status)}) carried no body`);
        return true;
      }
    );
  }
  // `call` resolves the same answer as undefined: an op that returns nothing.
  const { client } = restAnswering({ data: undefined, response: emptyResponse(200) });
  assert.equal(await restOpsClient(client).call('systemDesignSetOperatingModel', {}), undefined);
});

void test('rest callForBody: the status decides first (4xx keeps its code, empty 5xx says its status)', async () => {
  const refused = restAnswering({
    error: { code: 'failed_precondition', error: 'no research input' },
    response: new Response('{}', { status: 409 }),
  });
  await assert.rejects(
    restOpsClient(refused.client).callForBody('systemDesignStartSystemDesign', {
      path: { projectID: 'p1' },
    }),
    (err: unknown) =>
      err instanceof ApiError &&
      err.status === 409 &&
      err.code === 'failed_precondition' &&
      err.message === 'no research input'
  );
  const bare = restAnswering({ data: undefined, error: undefined, response: emptyResponse(502) });
  await assert.rejects(
    restOpsClient(bare.client).callForBody('constructionGetSessionState', {
      path: { projectID: 'p1', activityID: 'a1' },
    }),
    (err: unknown) =>
      err instanceof ApiError &&
      err.status === 502 &&
      err.message === 'request failed with status 502'
  );
});

void test('rest: a network failure rejects with the fetch error itself, on call and callForBody', async () => {
  const fail = (): Promise<never> => Promise.reject(new TypeError('Failed to fetch'));
  const ops = restOpsClient({ GET: fail, POST: fail } as never);
  for (const run of [
    (): Promise<unknown> =>
      ops.call('constructionExecuteNextActivity', { path: { projectID: 'p1' }, body: {} }),
    (): Promise<unknown> =>
      ops.callForBody('constructionGetSessionState', { path: { projectID: 'p1' } }),
  ]) {
    await assert.rejects(run(), (err: unknown) => {
      assert.ok(err instanceof TypeError, 'not an ApiError: the outcome is unknown');
      assert.equal(err.message, 'Failed to fetch');
      return true;
    });
  }
});

void test('rest: only a composition route sends Accept: application/json', async () => {
  const seen: RestCall[] = [];
  const answer = (path: string, options: unknown): Promise<unknown> => {
    seen.push({ path, options });
    return Promise.resolve({ data: {}, response: new Response('{}', { status: 200 }) });
  };
  const ops = restOpsClient({ GET: answer, POST: answer } as never);
  await ops.call('compositionGetUserinfo');
  await ops.call('constructionGetSessionState', { path: { projectID: 'p1', activityID: 'a' } });
  assert.deepEqual((seen[0]?.options as { headers?: unknown }).headers, {
    Accept: 'application/json',
  });
  assert.equal('headers' in (seen[1]?.options as object), false);
});

// -- MCP: an op with no tool is refused LOUDLY, never sent -----------------------

void test('mcp refuses an op with no MCP tool with ApiError(501, no_mcp_tool), without calling a tool', async () => {
  const { app, calls } = spyApp(() => ({ structuredContent: {}, content: [] }));
  const ops = mcpOpsClient(app as never);
  for (const run of [
    (): Promise<unknown> => ops.call('compositionGetCapabilities'),
    (): Promise<unknown> => ops.callForBody('compositionGetUserinfo'),
  ]) {
    await assert.rejects(run(), (err: unknown) => {
      assert.ok(err instanceof ApiError);
      assert.equal(err.status, 501);
      assert.equal(err.code, 'no_mcp_tool');
      assert.match(err.message, /has no MCP tool: GET \/api\/.* is REST-only$/);
      return true;
    });
  }
  assert.deepEqual(calls, []);
});

void test('mcp: a migrated construction op calls its server tool, with flattened args', async () => {
  const { app, calls } = spyApp(() => ({
    structuredContent: { result: { dispatched: true } },
    content: [],
  }));
  const ops = mcpOpsClient(app as never);
  assert.deepEqual(
    await ops.callForBody('constructionExecuteNextActivity', {
      path: { projectID: 'p1' },
      body: { tickID: 't-1' },
    }),
    { dispatched: true }
  );
  await ops.call('projectDesignRequestSdpCommit', { path: { projectID: 'p1' } });
  assert.deepEqual(calls, [
    { name: 'constructionExecuteNextActivity', arguments: { projectID: 'p1', tickID: 't-1' } },
    { name: 'projectDesignRequestSDPCommit', arguments: { projectID: 'p1' } },
  ]);
});
