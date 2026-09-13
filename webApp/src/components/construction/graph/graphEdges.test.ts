/**
 * The GRAPH lens's edge rules (graphEdges.ts). The one with teeth is R5's: an
 * ALARM edge is never hidden — not by hover-focus, not by a hovered milestone.
 * The live architecture has no alarm edge today, so only a fixture can prove it.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  buildActivityGraphModel,
  type GraphComponentLike,
  type GraphEdge,
  type GraphRelationshipLike,
} from './activityGraphModel.ts';
import {
  cardSetFocusFor,
  edgePresentationFor,
  hoverFocusFor,
  type EdgePresentation,
} from './graphEdges.ts';

function comp(id: string, layer: GraphComponentLike['layer']): GraphComponentLike {
  return { id, name: id, layer };
}

function rel(
  from: string,
  to: string,
  mode: GraphRelationshipLike['mode'] = 'sync'
): GraphRelationshipLike {
  return { from, to, mode };
}

// web-client → m1 → e1 (down), m1 ⇢ m2 (sanctioned queued Manager→Manager),
// e2 → m2 (UPWARD: the alarm edge), m2 → e2 (down).
const MODEL = buildActivityGraphModel({
  components: [
    comp('web-client', 'client'),
    comp('m1', 'manager'),
    comp('m2', 'manager'),
    comp('e1', 'engine'),
    comp('e2', 'engine'),
  ],
  relationships: [
    rel('web-client', 'm1'),
    rel('m1', 'e1'),
    rel('m1', 'm2', 'queued'),
    rel('e2', 'm2'),
    rel('m2', 'e2'),
  ],
  activities: [],
});

function edge(id: string): GraphEdge {
  const e = MODEL.edges.find((x) => x.id === id);
  assert.ok(e !== undefined, `no edge ${id}`);
  return e;
}

function presented(
  focus: Parameters<typeof edgePresentationFor>[1]
): Map<string, EdgePresentation> {
  return new Map(MODEL.edges.map((e) => [e.id, edgePresentationFor(e, focus)]));
}

void test('fixture: exactly one alarm edge, and it is upward', () => {
  assert.deepEqual(
    MODEL.edges.filter((e) => e.alarm).map((e) => [e.id, e.direction]),
    [['e2->m2', 'up']]
  );
});

void test('at rest nothing is hidden and nothing is focused', () => {
  for (const p of presented(null).values()) {
    assert.equal(p.hidden, false, p.id);
    assert.equal(p.variant, 'normal', p.id);
  }
});

void test('R5: a hover that EXCLUDES the alarm edge hides every other stranger, never the alarm', () => {
  // web-client touches only web-client->m1; the alarm edge e2->m2 is not incident.
  const focus = hoverFocusFor('web-client', MODEL.edges);
  assert.equal(focus.incident(edge('e2->m2')), false, 'fixture: the alarm edge is excluded');
  const p = presented(focus);
  assert.equal(p.get('e2->m2')?.hidden, false, 'the alarm edge stays drawn');
  assert.equal(p.get('m1->e1')?.hidden, true, 'a non-incident normal edge hides');
  assert.equal(p.get('web-client->m1')?.hidden, false);
  assert.equal(p.get('web-client->m1')?.variant, 'focus');
});

void test('R5: a hovered milestone that excludes the alarm edge never hides it either', () => {
  const p = presented(cardSetFocusFor(new Set(['m1', 'e1'])));
  assert.equal(p.get('e2->m2')?.hidden, false);
  assert.equal(p.get('m1->e1')?.variant, 'focus');
  assert.equal(p.get('web-client->m1')?.hidden, true);
});

void test('hover lights the hovered card and every neighbour, in both directions', () => {
  assert.deepEqual([...hoverFocusFor('m2', MODEL.edges).cards].sort(), ['e2', 'm1', 'm2']);
});

void test('queued calls are dashed; synchronous ones are not', () => {
  const p = presented(null);
  assert.equal(p.get('m1->m2')?.dashed, true);
  assert.equal(p.get('m1->e1')?.dashed, false);
});

void test('the class names the direction, so the alarm channel is findable', () => {
  const p = presented(null);
  assert.equal(p.get('e2->m2')?.className, 'graph-edge graph-edge-up');
  assert.equal(p.get('m1->m2')?.className, 'graph-edge graph-edge-sanctionedSideways');
  assert.equal(p.get('m1->e1')?.className, 'graph-edge graph-edge-down');
});
