import { test } from 'node:test';
import assert from 'node:assert/strict';
import { toDeploymentEdges, connectedElementKeys } from './deploymentEdges.ts';
import type { DeploymentRelationship } from './types.ts';

function rel(from: string, to: string, label: string, technology = ''): DeploymentRelationship {
  return { from, to, label, technology, mode: 'sync' };
}

void test('joins the authored and derived sets', () => {
  const edges = toDeploymentEdges(
    [rel('gateway', 'server', 'Forwards the authenticated request to', 'HTTP')],
    [rel('server', 'db', 'Reads from and writes to', 'SQL/TCP')]
  );
  assert.equal(edges.length, 2);
  assert.deepEqual(
    edges.map((e) => e.id),
    ['gateway->server', 'server->db']
  );
});

void test('marks derived-only edges as derived and authored ones as not', () => {
  const [authored, derived] = toDeploymentEdges([rel('a', 'b', 'Uses')], [rel('c', 'd', 'Calls')]);
  assert.ok(authored);
  assert.ok(derived);
  assert.equal(authored.derived, false);
  assert.equal(derived.derived, true);
});

// The collapse is the difference between a readable diagram and five identical
// lines: derivation is per-relationship, and a client calling five Managers that
// all ship in one container lands five edges on the same pair of boxes.
void test('collapses parallel strands between one pair into a single "N calls" edge', () => {
  const edges = toDeploymentEdges(
    [],
    [
      rel('spa', 'server', 'requestArtifactDraft'),
      rel('spa', 'server', 'submitReviewDecision'),
      rel('spa', 'server', 'advancePhase'),
    ]
  );
  assert.equal(edges.length, 1);
  const [collapsed] = edges;
  assert.ok(collapsed);
  assert.equal(collapsed.label, '3 calls');
  assert.deepEqual(collapsed.details, [
    'requestArtifactDraft',
    'submitReviewDecision',
    'advancePhase',
  ]);
});

// An authored label names the DEPLOYMENT-level interaction ("Makes API calls
// to"); a derived one is the component-level operation list, which is the wrong
// altitude for this view. So the authored label must win the pair.
void test('an authored label survives a derived strand on the same pair', () => {
  const edges = toDeploymentEdges(
    [rel('spa', 'server', 'Makes API calls to', 'JSON/HTTPS')],
    [rel('spa', 'server', 'requestArtifactDraft')]
  );
  assert.equal(edges.length, 1);
  const [merged] = edges;
  assert.ok(merged);
  assert.equal(merged.label, '2 calls');
  assert.equal(merged.technology, 'JSON/HTTPS');
  assert.equal(merged.derived, false);
  assert.deepEqual(merged.details, ['Makes API calls to', 'requestArtifactDraft']);
});

void test('direction matters — a→b and b→a are separate edges', () => {
  const edges = toDeploymentEdges([rel('a', 'b', 'Calls'), rel('b', 'a', 'Notifies')], []);
  assert.equal(edges.length, 2);
});

void test('a technology from a later strand fills an empty one', () => {
  const [edge] = toDeploymentEdges([rel('a', 'b', 'Calls')], [rel('a', 'b', 'Also calls', 'gRPC')]);
  assert.ok(edge);
  assert.equal(edge.technology, 'gRPC');
});

void test('drops relationships with an empty endpoint', () => {
  assert.deepEqual(toDeploymentEdges([rel('', 'b', 'Calls'), rel('a', '', 'Calls')], []), []);
});

void test('empty input yields no edges', () => {
  assert.deepEqual(toDeploymentEdges([], []), []);
});

void test('connectedElementKeys collects both endpoints of every edge', () => {
  const edges = toDeploymentEdges([rel('a', 'b', 'Calls'), rel('b', 'c', 'Calls')], []);
  assert.deepEqual([...connectedElementKeys(edges)].sort(), ['a', 'b', 'c']);
});
