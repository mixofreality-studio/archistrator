/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { planRowFor, type PlanRowInput } from './planRowFor.ts';

void test('the three design activities are the front end, whatever layer the server reports', () => {
  for (const kind of ['requirements', 'architecture', 'projectDesign'] as const) {
    assert.equal(planRowFor({ kind }), 'frontEnd');
  }
});

void test('N-STP is the side lane and N-IT is system testing — the server calls both projectWide', () => {
  assert.equal(planRowFor({ kind: 'testing', variant: 'plan' }), 'sideLane');
  assert.equal(planRowFor({ kind: 'testing', variant: 'systemTest' }), 'systemTesting');
});

void test('a coding activity takes its component layer', () => {
  assert.equal(planRowFor({ kind: 'service', layer: 'resourceAccess' }), 'resourceAccess');
  assert.equal(planRowFor({ kind: 'frontend', layer: 'client' }), 'client');
  assert.equal(planRowFor({ kind: 'service', layer: 'resource' }), 'resource');
  assert.equal(planRowFor({ kind: 'service', layer: 'engine' }), 'engine');
  assert.equal(planRowFor({ kind: 'service', layer: 'manager' }), 'manager');
});

void test('an activity with no row is undefined, not guessed into one', () => {
  assert.equal(planRowFor({ kind: 'documentation' }), undefined, 'no component layer, no band');
  assert.equal(
    planRowFor({ kind: 'service', layer: 'utility' }),
    undefined,
    'utilities derive no activity'
  );
  assert.equal(planRowFor({ kind: 'testing', variant: 'harness' }), undefined);
  assert.equal(planRowFor({ kind: 'testing', variant: 'perf' }), undefined);
  assert.equal(planRowFor({ kind: 'testing', variant: 'qaProcess' }), undefined);
});

void test('an UNCLASSIFIED row is undefined — the server refused to guess and so does this', () => {
  assert.equal(planRowFor({}), undefined);
  assert.equal(planRowFor({ layer: 'engine' }), 'engine', 'a classified layer still places it');
});

void test('the live plan places 29 of its 32 activities and says which three it cannot', () => {
  // The repo's own committed list (slot 9): requirements/architecture/
  // projectDesign → frontEnd, 4 R-* → resource, 10 C-*-access → resourceAccess,
  // 7 C-*-engine → engine, 5 C-*-manager → manager, U-SPA-web-client → client,
  // N-STP → sideLane, N-IT → systemTesting. That is all 32, with nothing unplaced —
  // pinned here so a model change that orphans a tile fails a test, not a screen.
  const live: PlanRowInput[] = [
    { kind: 'requirements' },
    { kind: 'architecture' },
    { kind: 'projectDesign' },
    { kind: 'service', layer: 'resource' },
    { kind: 'service', layer: 'resourceAccess' },
    { kind: 'service', layer: 'engine' },
    { kind: 'service', layer: 'manager' },
    { kind: 'frontend', layer: 'client' },
    { kind: 'testing', variant: 'plan' },
    { kind: 'testing', variant: 'systemTest' },
  ];
  assert.ok(
    live.every((r) => planRowFor(r) !== undefined),
    'every live shape places'
  );
});
