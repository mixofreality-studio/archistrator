/// <reference types="node" />
/**
 * The fixture transport (src/api/fixtureOps.ts): result, error, pending-forever,
 * a mutation that changes nothing, and the LOUD miss.
 *
 * Plus, since stage 4a, the ONE selector-keyed op: `deliveryQueryProjectView`
 * answers a different body per `ProjectViewKind`, so its fixture is a map from
 * kind to answer and the transport reads the kind off the call's own body. The
 * cases below pin what a flat key could not express — a PENDING kind beside a
 * RESULT kind in one fixture — and that a miss on one kind names THAT kind.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { ApiError } from '../contracts/errors.ts';
import { FIXTURE_MISS_CODE, FixtureMissError, fixtureOpsClient } from './fixtureOps.ts';

const PENDING = Symbol('pending');

async function settledWithin<T>(p: Promise<T>, ms: number): Promise<T | typeof PENDING> {
  return Promise.race([
    p,
    new Promise<typeof PENDING>((resolve) => {
      setTimeout(() => {
        resolve(PENDING);
      }, ms);
    }),
  ]);
}

interface Demo {
  Name: string;
  Meta: { count: number };
}

/** A `deliveryQueryProjectView` call for one kind, shaped as the hooks send it. */
const view = (kind: string): { body: { query: { kind: string } } } => ({
  body: { query: { kind } },
});

void test('a result fixture resolves with a fresh copy every call', async () => {
  const fixtures = {
    deliveryQueryActivityView: { result: { Name: 'demo', Meta: { count: 1 } } },
  };
  const ops = fixtureOpsClient(fixtures);
  const first = await ops.call<Demo>('deliveryQueryActivityView');
  assert.deepEqual(first, { Name: 'demo', Meta: { count: 1 } });
  first.Name = 'mutated by a component';
  first.Meta.count = 99;
  const second = await ops.call<Demo>('deliveryQueryActivityView');
  assert.deepEqual(second, { Name: 'demo', Meta: { count: 1 } });
  assert.deepEqual(fixtures.deliveryQueryActivityView.result, { Name: 'demo', Meta: { count: 1 } });
});

void test('an error fixture rejects with the real ApiError', async () => {
  const ops = fixtureOpsClient({
    deliveryQueryActivityView: {
      error: { status: 409, code: 'failedPrecondition', message: 'x' },
    },
    deliveryQueryProjectView: { projects: { error: { status: 503 } } },
  });
  await assert.rejects(ops.call('deliveryQueryActivityView'), (err: unknown) => {
    assert.ok(err instanceof ApiError);
    assert.equal(err.status, 409);
    assert.equal(err.code, 'failedPrecondition');
    assert.equal(err.message, 'x');
    return true;
  });
  await assert.rejects(ops.call('deliveryQueryProjectView', view('projects')), (err: unknown) => {
    assert.ok(err instanceof ApiError);
    assert.equal(err.status, 503);
    assert.equal(err.code, 'internal');
    assert.equal(err.message, 'request failed with status 503');
    return true;
  });
});

void test('a pending fixture never settles', async () => {
  const ops = fixtureOpsClient({ deliveryQueryActivityView: { pending: true } });
  assert.equal(await settledWithin(ops.call('deliveryQueryActivityView'), 50), PENDING);
});

void test('a mutation answers from its fixture and changes no state', async () => {
  const fixtures = {
    deliveryQueryActivityView: { result: { Name: 'demo' } },
    deliverySubmitReviewDecision: { result: null },
  };
  const before = structuredClone(fixtures);
  const ops = fixtureOpsClient(fixtures);
  await ops.call('deliverySubmitReviewDecision', {
    path: { projectID: 'demo', activityID: 'C-x' },
    body: { decision: 1 },
  });
  await ops.call('deliverySubmitReviewDecision', { body: { decision: 2 } });
  assert.deepEqual(await ops.call('deliveryQueryActivityView'), { Name: 'demo' });
  assert.deepEqual(fixtures, before);
});

void test('a call with no fixture fails LOUDLY: FixtureMissError, never a 404', async () => {
  const missed: string[] = [];
  const ops = fixtureOpsClient(
    { deliveryQueryActivityView: { result: {} } },
    {
      onMiss: (miss) => {
        missed.push(miss.detail);
      },
    }
  );
  await assert.rejects(ops.call('deliveryExecuteNextActivity'), (err: unknown) => {
    assert.ok(err instanceof FixtureMissError);
    assert.ok(err instanceof ApiError, 'renders as the app ordinary error');
    assert.equal(err.name, 'FixtureMissError');
    assert.equal(err.status, 500, 'a 404 would read as "nothing here" and hide the miss');
    assert.equal(err.code, FIXTURE_MISS_CODE);
    assert.equal(err.opId, 'deliveryExecuteNextActivity');
    assert.equal(err.selector, undefined, 'an op with no selector reports none');
    assert.equal(err.detail, 'deliveryExecuteNextActivity');
    assert.match(err.message, /deliveryExecuteNextActivity/);
    assert.match(err.message, /POST \/api\/v1\/delivery\/execute-next-activity\//);
    return true;
  });
  assert.deepEqual(missed, ['deliveryExecuteNextActivity']);
});

void test('a mutation with no fixture misses too', async () => {
  const ops = fixtureOpsClient({});
  await assert.rejects(ops.call('deliveryExecuteNextActivity'), FixtureMissError);
});

void test('an inherited property is not a fixture', async () => {
  const fixtures = Object.create({
    deliveryQueryActivityView: { result: 'from the prototype' },
  }) as {
    deliveryQueryActivityView?: { result: unknown };
  };
  const ops = fixtureOpsClient(fixtures);
  await assert.rejects(ops.call('deliveryQueryActivityView'), FixtureMissError);
});

// -- the selector-keyed read ----------------------------------------------------

void test('the view read answers PER KIND, and one kind may pend while another resolves', async () => {
  // Exactly plan/loading.json's shape, and the reason the key nests at all: the
  // plan reads `summary` and `projects` through ONE op, and the state under
  // preview is "the summary never answers". A flat key would have to choose.
  const ops = fixtureOpsClient({
    deliveryQueryProjectView: {
      summary: { pending: true },
      projects: { result: { kind: 'projects', projects: [{ ProjectID: 'archistrator' }] } },
    },
  });
  assert.equal(
    await settledWithin(ops.call('deliveryQueryProjectView', view('summary')), 50),
    PENDING
  );
  assert.deepEqual(await ops.call('deliveryQueryProjectView', view('projects')), {
    kind: 'projects',
    projects: [{ ProjectID: 'archistrator' }],
  });
});

void test('a miss on ONE kind names that kind, not the op', async () => {
  const missed: string[] = [];
  const ops = fixtureOpsClient(
    { deliveryQueryProjectView: { summary: { result: { kind: 'summary' } } } },
    {
      onMiss: (miss) => {
        missed.push(miss.detail);
      },
    }
  );
  await assert.rejects(ops.call('deliveryQueryProjectView', view('timeline')), (err: unknown) => {
    assert.ok(err instanceof FixtureMissError);
    assert.equal(err.opId, 'deliveryQueryProjectView');
    assert.equal(err.selector, 'timeline');
    assert.equal(err.detail, 'deliveryQueryProjectView(timeline)');
    // Without the kind, an alarm over this fixture would read "no fixture answers
    // deliveryQueryProjectView" while one plainly does.
    assert.match(err.message, /deliveryQueryProjectView\(timeline\)/);
    return true;
  });
  assert.deepEqual(missed, ['deliveryQueryProjectView(timeline)']);
});

void test('a view call with no kind, or an unknown kind, is a miss and never another kind answer', async () => {
  const ops = fixtureOpsClient({
    deliveryQueryProjectView: { summary: { result: { kind: 'summary' } } },
  });
  // No body at all, a body with no query, and a kind the contract does not carry:
  // each is a miss. Falling back to the one fixtured kind would render a screen
  // built from the wrong view.
  for (const params of [
    {},
    { body: {} },
    { body: { query: {} } },
    { body: { query: { kind: 'sumary' } } },
  ]) {
    await assert.rejects(ops.call('deliveryQueryProjectView', params), FixtureMissError);
  }
  assert.deepEqual(await ops.call('deliveryQueryProjectView', view('summary')), {
    kind: 'summary',
  });
});

void test('a view kind inherited from a prototype is not a fixture either', async () => {
  const byKind = Object.create({ summary: { result: 'from the prototype' } }) as {
    summary?: { result: unknown };
  };
  const ops = fixtureOpsClient({ deliveryQueryProjectView: byKind });
  await assert.rejects(ops.call('deliveryQueryProjectView', view('summary')), FixtureMissError);
});
