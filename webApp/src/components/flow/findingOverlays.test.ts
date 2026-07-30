/// <reference types="node" />
/**
 * Unit tests for the pure Design-Health → diagram join
 * (src/components/flow/findingOverlays.ts). Fixture messages/locations mirror the
 * server's designhealth rule layer (server/internal/utility/designhealth):
 *
 *   DH-GRAPH edge rules      → Location.Section = "From→To"  (U+2192, component ids)
 *   DH-GRAPH-UTIL-REACHABLE  → "utility <id>"
 *   DH-GRAPH-MANAGER-EMPTY   → "manager <id>"
 *   DH-COMP-*                → "component <id>"
 *   DH-CARD-* / DH-CHAIN-* / DH-VOL-* / DH-OBJ-* / DH-CONTRACT-* → non-diagram
 *     sections ("components", "dynamicView …", "volatility …", "contract …") that
 *     must NOT attach to the diagram (they stay in the Design Health list).
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  computeStructureOverlays,
  edgeOverlayKey,
  findingLines,
  maxSeverity,
  structureFindingsChipLabel,
} from './findingOverlays.ts';
import type { Finding, Severity } from '../../contracts/types';

/** Shorthand Finding fixture; omit `section` for a location-less finding. */
function fnd(ruleId: string, severity: Severity, message: string, section?: string): Finding {
  return {
    ruleId,
    severity,
    message,
    ...(section !== undefined ? { location: { ordinal: 0, section } } : {}),
  };
}

// A small Method system: one client, two managers, an engine, a RA, a resource,
// and an (unreached) utility — enough to host every DH-GRAPH violation shape.
const components = [
  { id: 'webClient' },
  { id: 'billingManager' },
  { id: 'constructionManager' },
  { id: 'billingEngine' },
  { id: 'projectStateAccess' },
  { id: 'projectStore' },
  { id: 'securityUtility' },
];

const relationships = [
  { from: 'webClient', to: 'billingManager' },
  { from: 'webClient', to: 'billingEngine' },
  { from: 'billingManager', to: 'billingEngine' },
  { from: 'billingManager', to: 'constructionManager' },
  { from: 'billingEngine', to: 'billingManager' },
  { from: 'billingEngine', to: 'projectStateAccess' },
  { from: 'constructionManager', to: 'projectStateAccess' },
  { from: 'projectStateAccess', to: 'projectStore' },
];

const engineIoFinding = fnd(
  'DH-GRAPH-ENGINE-IO',
  'warning',
  'engine billingEngine calls projectStateAccess (resourceAccess): Engines are pure computation in this architecture — route IO through a Manager',
  'billingEngine→projectStateAccess'
);

// The five edge-anchored DH-GRAPH findings, worded as the Go rules mint them.
const edgeFindings: Finding[] = [
  fnd(
    'DH-GRAPH-UPCALL',
    'error',
    'up-call: billingEngine (engine) calls billingManager (manager) — a higher layer; the closed architecture only calls DOWN the layers',
    'billingEngine→billingManager'
  ),
  fnd(
    'DH-GRAPH-SIDEWAYS-SYNC',
    'error',
    'sideways sync call: billingManager→constructionManager are the same layer (manager) but the call is "sync" — same-layer calls (e.g. Manager→Manager) are permitted only when queued',
    'billingManager→constructionManager'
  ),
  fnd(
    'DH-GRAPH-CLIENT-ENTRY',
    'error',
    'client webClient calls billingEngine (engine) — a Client may enter the system only at a Manager, never at engine directly',
    'webClient→billingEngine'
  ),
  fnd(
    'DH-GRAPH-QUEUED-TARGET',
    'error',
    'queued call into a resourceAccess (projectStateAccess): Engines and ResourceAccess are synchronous — only Managers receive queued calls',
    'constructionManager→projectStateAccess'
  ),
  engineIoFinding,
];

// Node-anchored findings: the DH-GRAPH reachability/orchestration warnings plus
// the DH-COMP component-level rules.
const nodeFindings: Finding[] = [
  fnd(
    'DH-GRAPH-UTIL-REACHABLE',
    'warning',
    'utility "securityUtility" has no inbound edge — it is unreachable/dead in the architecture, or the caller relationships are missing',
    'utility securityUtility'
  ),
  fnd(
    'DH-GRAPH-MANAGER-EMPTY',
    'warning',
    'Manager "constructionManager" has no outbound edge to an Engine or ResourceAccess — a Manager must orchestrate something; one with no downstream edges is empty (missing its edges, or a mislabeled component)',
    'manager constructionManager'
  ),
  fnd(
    'DH-COMP-NO-VOLATILITY',
    'warning',
    'engine component "billingEngine" encapsulates no volatility — every Manager/Engine/ResourceAccess must encapsulate at least one area of volatility (Righting Software ch. 2: a component owning none is functional decomposition, the siren song)',
    'component billingEngine'
  ),
  fnd(
    'DH-COMP-VOL-DANGLING',
    'error',
    'component "billingManager" lists "Ghost Volatility" in encapsulatesVolatilities, which is not a committed volatility name — a dangling reference (stale after a volatility rename/removal)',
    'component billingManager'
  ),
];

// Findings the diagram must IGNORE (they still render in the Design Health list).
const ignoredFindings: Finding[] = [
  fnd('DH-CARD-MANAGERS', 'warning', '6 Manager components — …', 'components'),
  fnd('DH-CHAIN-ENTRY-MANAGER', 'warning', 'chain "uc-install" has 2 …', 'dynamicView uc-install'),
  fnd(
    'DH-VOL-ENCAP-MISSING',
    'error',
    'volatility "Payment Methods" is encapsulated by no component — …',
    'volatility Payment Methods'
  ),
  fnd('DH-CONTRACT-OPCOUNT-MAX', 'warning', '13 operations — …', 'contract billingManager'),
  fnd(
    'DH-OBJ-COVERAGE',
    'info',
    'objective(s) [3] are referenced by no decision — …',
    'objectives'
  ),
  // No location at all → nothing to join on.
  fnd('DH-GRAPH-UPCALL', 'error', 'up-call with no location'),
  // Both endpoints unknown → unresolvable, ignored.
  fnd('DH-GRAPH-UPCALL', 'error', 'up-call: ghostA calls ghostB — …', 'ghostA→ghostB'),
  // Unknown component id on a node-shaped section → ignored.
  fnd(
    'DH-GRAPH-MANAGER-EMPTY',
    'warning',
    'Manager "ghostManager" has no …',
    'manager ghostManager'
  ),
  // NON-DH (framework methodcheck) finding whose section happens to look like an
  // edge pair — the diagram only trusts the DH location grammar.
  fnd('ARCH-EDGE-RULE', 'error', 'some framework finding', 'webClient→billingManager'),
];

void test('DH-GRAPH edge findings attach to their matching relationship (from→to pair)', () => {
  const o = computeStructureOverlays(edgeFindings, components, relationships);
  assert.equal(o.edges.size, 5);
  for (const f of edgeFindings) {
    const key = f.location?.section ?? '';
    const list = o.edges.get(key);
    assert.ok(list, `expected an edge overlay at ${key}`);
    assert.deepEqual(list, [f]);
  }
  assert.equal(o.nodes.size, 0);
  assert.equal(o.attachedCount, 5);
});

void test('node-shaped sections (utility/manager/component <id>) attach to the component', () => {
  const o = computeStructureOverlays(nodeFindings, components, relationships);
  assert.equal(o.edges.size, 0);
  assert.deepEqual([...o.nodes.keys()].sort(), [
    'billingEngine',
    'billingManager',
    'constructionManager',
    'securityUtility',
  ]);
  assert.equal(o.nodes.get('billingEngine')?.[0]?.ruleId, 'DH-COMP-NO-VOLATILITY');
  assert.equal(o.attachedCount, 4);
});

void test('non-diagram families, location-less and unresolvable findings are ignored', () => {
  const o = computeStructureOverlays(ignoredFindings, components, relationships);
  assert.equal(o.edges.size, 0);
  assert.equal(o.nodes.size, 0);
  assert.equal(o.attachedCount, 0);
});

void test('an edge finding whose relationship is gone degrades to its surviving endpoint node', () => {
  // Drift posture: health evaluated against an older head-state can reference an
  // edge the current diagram no longer draws — anchor the finding on the caller.
  const drifted = fnd(
    'DH-GRAPH-UPCALL',
    'error',
    'up-call: billingEngine (engine) calls ghostManager (manager) — …',
    'billingEngine→ghostManager'
  );
  const o = computeStructureOverlays([drifted], components, relationships);
  assert.equal(o.edges.size, 0);
  assert.deepEqual(o.nodes.get('billingEngine'), [drifted]);
  assert.equal(o.attachedCount, 1);

  // Caller gone too → the callee side keeps it; both gone → ignored (covered above).
  const calleeOnly = fnd('DH-GRAPH-UPCALL', 'error', '…', 'ghostEngine→billingManager');
  const o2 = computeStructureOverlays([calleeOnly], components, relationships);
  assert.deepEqual(o2.nodes.get('billingManager'), [calleeOnly]);
});

void test('multiple findings on the same edge / node aggregate under one key', () => {
  const twice = [
    engineIoFinding, // ENGINE-IO on billingEngine→projectStateAccess
    fnd(
      'DH-GRAPH-QUEUED-TARGET',
      'error',
      'queued call into a resourceAccess …',
      'billingEngine→projectStateAccess'
    ),
  ];
  const o = computeStructureOverlays(twice, components, relationships);
  assert.equal(o.edges.get(edgeOverlayKey('billingEngine', 'projectStateAccess'))?.length, 2);
  assert.equal(o.attachedCount, 2);
});

void test('edgeOverlayKey matches the Go section rendering (U+2192, no spaces)', () => {
  assert.equal(edgeOverlayKey('a', 'b'), 'a→b');
});

void test('maxSeverity ranks error > warning > info (info for an empty list)', () => {
  assert.equal(
    maxSeverity([fnd('X-Y', 'info', 'm'), fnd('X-Y', 'error', 'm'), fnd('X-Y', 'warning', 'm')]),
    'error'
  );
  assert.equal(maxSeverity([fnd('X-Y', 'info', 'm'), fnd('X-Y', 'warning', 'm')]), 'warning');
  assert.equal(maxSeverity([fnd('X-Y', 'info', 'm')]), 'info');
  assert.equal(maxSeverity([]), 'info');
});

void test('findingLines renders "ruleId — message" for tooltips/aria', () => {
  const lines = findingLines([fnd('DH-GRAPH-UPCALL', 'error', 'up-call: a calls b')]);
  assert.deepEqual(lines, ['DH-GRAPH-UPCALL — up-call: a calls b']);
});

void test('the count chip label pluralises', () => {
  assert.equal(structureFindingsChipLabel(1), '1 structure finding');
  assert.equal(structureFindingsChipLabel(2), '2 structure findings');
});
