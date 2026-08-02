/// <reference types="node" />
/**
 * Unit tests for resolveDecider: the decision/switch call-less-step decider
 * highlight (founder QA round 3, change 3) — an explicit authored `decidedBy`
 * first (call-chain rollout Task 5, guarded on placement the same as the
 * actor-lane rule), actor-lane match (guarded on the actor being PLACED in the
 * view — review fix round 1), the Machine-lane / no-match / not-placed
 * fallthrough to the entry Manager, and the zero-step-view fallback to
 * undefined (mute-all).
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  resolveDecider,
  type DeciderActor,
  type DeciderCall,
  type DeciderParticipant,
} from './deciderResolution.ts';

function actor(id: string, role: string): DeciderActor {
  return { id, role };
}

function comp(id: string, layer: string, name = id): DeciderParticipant {
  return { id, name, layer };
}

function call(from: string, to: string): DeciderCall {
  return { from, to };
}

const PARTICIPANTS = [
  comp('web-client', 'client', 'WebClient'),
  comp('mcp-client', 'client', 'MCPClient'),
  comp('system-design-manager', 'manager', 'SystemDesignManager'),
  comp('project-state-access', 'resourceAccess', 'ProjectStateAccess'),
];

// ── decidedBy — explicit authored preference (call-chain rollout Task 5) ────

void test('an explicit decidedBy resolving to a PLACED actor wins outright, ignoring the lane entirely', () => {
  const result = resolveDecider(
    'architect-user',
    'Nobody', // the lane would fall through to the entry Manager on its own
    [actor('architect-user', 'Reviewer'), actor('pm-user', 'PM')],
    PARTICIPANTS,
    [call('architect-user', 'system-design-manager')]
  );
  assert.deepEqual(result, { id: 'architect-user', label: 'Reviewer' });
});

void test('an explicit decidedBy resolving to a PLACED component wins outright', () => {
  const result = resolveDecider('system-design-manager', 'Machine', [], PARTICIPANTS, [
    call('web-client', 'system-design-manager'),
  ]);
  assert.deepEqual(result, { id: 'system-design-manager', label: 'SystemDesignManager' });
});

void test('an explicit decidedBy naming an actor that is NOT placed anywhere in the view falls through to the lane/entry-Manager chain', () => {
  const result = resolveDecider(
    'architect-user',
    'Machine',
    [actor('architect-user', 'Reviewer')], // known, but never a call endpoint below
    PARTICIPANTS,
    [call('web-client', 'system-design-manager')]
  );
  assert.deepEqual(result, { id: 'system-design-manager', label: 'SystemDesignManager' });
});

void test('an explicit decidedBy that resolves to neither an actor nor a component (unknown id) falls through', () => {
  const result = resolveDecider(
    'nobody-authored-this-id',
    'Reviewer',
    [actor('architect-user', 'Reviewer')],
    PARTICIPANTS,
    [call('architect-user', 'system-design-manager')]
  );
  assert.deepEqual(result, { id: 'architect-user', label: 'Reviewer' });
});

void test('an undefined or blank decidedBy skips the branch entirely and proceeds to the lane/entry-Manager chain', () => {
  const undef = resolveDecider(undefined, 'Machine', [], PARTICIPANTS, [
    call('web-client', 'system-design-manager'),
  ]);
  assert.deepEqual(undef, { id: 'system-design-manager', label: 'SystemDesignManager' });

  const blank = resolveDecider('', 'Machine', [], PARTICIPANTS, [
    call('web-client', 'system-design-manager'),
  ]);
  assert.deepEqual(blank, { id: 'system-design-manager', label: 'SystemDesignManager' });
});

// ── actor-lane / entry-Manager inference (unchanged since founder QA round 3/4) ──

void test('an actor-lane node resolves to the actor whose role matches the lane, when that actor is placed (a call endpoint) in the view', () => {
  const result = resolveDecider(
    undefined,
    'Reviewer',
    [actor('architect-user', 'Reviewer'), actor('pm-user', 'PM')],
    PARTICIPANTS,
    [call('architect-user', 'system-design-manager')]
  );
  assert.deepEqual(result, { id: 'architect-user', label: 'Reviewer' });
});

void test('an actor-lane node with no matching actor role falls through to the entry Manager', () => {
  const result = resolveDecider(
    undefined,
    'Nobody',
    [actor('architect-user', 'Reviewer')],
    PARTICIPANTS,
    [call('web-client', 'system-design-manager')]
  );
  assert.deepEqual(result, { id: 'system-design-manager', label: 'SystemDesignManager' });
});

void test('an actor-lane node whose role matches but the actor is NOT placed anywhere in the view (no call endpoint) falls through to the entry Manager', () => {
  // review fix round 1: a lane-matching actor absent from every call in the view
  // is also absent from dv.persons (personParticipants filters on the same call
  // endpoints) — resolving to it would name a node the diagram never renders.
  const result = resolveDecider(
    undefined,
    'Reviewer',
    [actor('architect-user', 'Reviewer')],
    PARTICIPANTS,
    [call('web-client', 'system-design-manager')] // architect-user is not an endpoint here
  );
  assert.deepEqual(result, { id: 'system-design-manager', label: 'SystemDesignManager' });
});

void test('a Machine-lane node skips actor lookup entirely and resolves the entry Manager', () => {
  const result = resolveDecider(
    undefined,
    'Machine',
    [actor('architect-user', 'Machine')], // even a role coincidentally named 'Machine' is ignored
    PARTICIPANTS,
    [call('mcp-client', 'system-design-manager')]
  );
  assert.deepEqual(result, { id: 'system-design-manager', label: 'SystemDesignManager' });
});

void test('the FIRST client->manager call wins when more than one exists', () => {
  const result = resolveDecider(undefined, 'Machine', [], PARTICIPANTS, [
    call('project-state-access', 'system-design-manager'), // resourceAccess -> manager: not a match
    call('web-client', 'system-design-manager'),
    call('mcp-client', 'system-design-manager'),
  ]);
  assert.deepEqual(result, { id: 'system-design-manager', label: 'SystemDesignManager' });
});

void test('no client->manager call anywhere in the view resolves to undefined (fall back to mute-all)', () => {
  const result = resolveDecider(undefined, 'Machine', [], PARTICIPANTS, [
    call('system-design-manager', 'project-state-access'),
  ]);
  assert.equal(result, undefined);
});

void test('a zero-step view (no edges at all) resolves to undefined', () => {
  assert.equal(resolveDecider(undefined, 'Machine', [], PARTICIPANTS, []), undefined);
});
