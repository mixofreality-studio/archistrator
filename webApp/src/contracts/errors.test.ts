import { test } from 'node:test';
import assert from 'node:assert/strict';
import { ApiError, bodyUnlessError, throwUnlessOk } from './errors.ts';

function empty(status: number): Response {
  return new Response(null, { status, headers: { 'content-length': '0' } });
}

void test('an EMPTY-body error is still an error: the status decides, not the parsed body (fix-D review I2)', () => {
  // openapi-fetch hands back `error: undefined` for these, which a proxy's 502/503/504 look like.
  for (const status of [400, 404, 500, 502, 503, 504]) {
    assert.throws(
      () => {
        throwUnlessOk(empty(status), undefined);
      },
      (e: unknown) =>
        e instanceof ApiError &&
        e.status === status &&
        e.message === `request failed with status ${String(status)}`,
      String(status)
    );
  }
});

void test('an error body carries its code and message through', () => {
  assert.throws(
    () => {
      throwUnlessOk(new Response('{}', { status: 400 }), {
        code: 'contract_misuse',
        error: 'empty tickId',
      });
    },
    (e: unknown) =>
      e instanceof ApiError &&
      e.status === 400 &&
      e.code === 'contract_misuse' &&
      e.message === 'empty tickId'
  );
});

void test('a 2xx is success, with or without a body', () => {
  for (const status of [200, 202, 204]) {
    assert.doesNotThrow(() => {
      throwUnlessOk(empty(status), undefined);
    }, String(status));
  }
});

void test('bodyUnlessError: the status decides, and a 2xx with no body where one is owed is an error too', () => {
  // An empty-body 5xx: the status, never "success with undefined".
  assert.throws(
    () => {
      bodyUnlessError({ data: undefined, error: undefined, response: empty(500) });
    },
    (e: unknown) => e instanceof ApiError && e.status === 500 && e.code === 'internal'
  );
  // A 2xx with no body: create-project answered that way used to hand `undefined`
  // on as the new project's id.
  assert.throws(
    () => {
      bodyUnlessError({ data: undefined, error: undefined, response: empty(200) });
    },
    (e: unknown) => e instanceof ApiError && e.status === 200 && e.code === 'empty_body'
  );
  assert.equal(
    bodyUnlessError({ data: 'p-1', response: new Response('"p-1"', { status: 200 }) }),
    'p-1'
  );
  assert.deepEqual(
    bodyUnlessError({ data: { a: 1 }, response: new Response('{}', { status: 201 }) }),
    { a: 1 }
  );
});
