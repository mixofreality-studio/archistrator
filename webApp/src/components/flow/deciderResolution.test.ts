/// <reference types="node" />
/**
 * Unit tests for resolveDecider: the decision/switch call-less-step decider
 * highlight (founder QA round 3, change 3) — actor-lane match, the
 * Machine-lane / no-match fallthrough to the entry Manager, and the
 * zero-step-view fallback to undefined (mute-all).
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

void test('an actor-lane node resolves to the actor whose role matches the lane', () => {
  const result = resolveDecider(
    'Reviewer',
    [actor('architect-user', 'Reviewer'), actor('pm-user', 'PM')],
    PARTICIPANTS,
    [call('web-client', 'system-design-manager')]
  );
  assert.deepEqual(result, { id: 'architect-user', label: 'Reviewer' });
});

void test('an actor-lane node with no matching actor role falls through to the entry Manager', () => {
  const result = resolveDecider('Nobody', [actor('architect-user', 'Reviewer')], PARTICIPANTS, [
    call('web-client', 'system-design-manager'),
  ]);
  assert.deepEqual(result, { id: 'system-design-manager', label: 'SystemDesignManager' });
});

void test('a Machine-lane node skips actor lookup entirely and resolves the entry Manager', () => {
  const result = resolveDecider(
    'Machine',
    [actor('architect-user', 'Machine')], // even a role coincidentally named 'Machine' is ignored
    PARTICIPANTS,
    [call('mcp-client', 'system-design-manager')]
  );
  assert.deepEqual(result, { id: 'system-design-manager', label: 'SystemDesignManager' });
});

void test('the FIRST client->manager call wins when more than one exists', () => {
  const result = resolveDecider('Machine', [], PARTICIPANTS, [
    call('project-state-access', 'system-design-manager'), // resourceAccess -> manager: not a match
    call('web-client', 'system-design-manager'),
    call('mcp-client', 'system-design-manager'),
  ]);
  assert.deepEqual(result, { id: 'system-design-manager', label: 'SystemDesignManager' });
});

void test('no client->manager call anywhere in the view resolves to undefined (fall back to mute-all)', () => {
  const result = resolveDecider('Machine', [], PARTICIPANTS, [
    call('system-design-manager', 'project-state-access'),
  ]);
  assert.equal(result, undefined);
});

void test('a zero-step view (no edges at all) resolves to undefined', () => {
  assert.equal(resolveDecider('Machine', [], PARTICIPANTS, []), undefined);
});
