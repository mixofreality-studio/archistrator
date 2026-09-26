/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { openThreadCount, toReviewCommentView, toReviewThread } from './threadAdapter.ts';
import type { components } from '../../contracts/schema.ts';
import type { ReviewCommentView } from '../../contracts/types.ts';

type ThreadWire = components['schemas']['DeliveryReviewThreadComment'];

function wire(over: Partial<ThreadWire>): ThreadWire {
  return {
    id: 'c1',
    anchor: '$.mission.vision',
    authorRole: 'architect',
    text: 'tighten this',
    round: 1,
    reopened: false,
    replies: [],
    status: 'open',
    type: 'changeRequest',
    ...over,
  };
}

void test('a question keeps its addressee; a change request is given the empty one', () => {
  assert.equal(toReviewCommentView(wire({ type: 'question', addressee: 'pm' })).addressee, 'pm');
  assert.equal(
    toReviewCommentView(wire({ type: 'question', addressee: 'architect' })).addressee,
    'architect'
  );
  assert.equal(
    toReviewCommentView(wire({})).addressee,
    '',
    'a change request is addressed to no one'
  );
});

void test('an addressee the view has no member for falls back to the empty one, never crashes', () => {
  assert.equal(
    toReviewCommentView(wire({ type: 'question', addressee: 'someoneElse' })).addressee,
    ''
  );
});

void test('a staleAck entry survives as a change-request thread rather than being dropped', () => {
  const view = toReviewCommentView(
    wire({ id: 'ack1', type: 'staleAck', text: 'reviewed — unaffected' })
  );
  assert.equal(
    view.type,
    'changeRequest',
    'the view has no staleAck member; the wire default is changeRequest'
  );
  assert.equal(
    view.text,
    'reviewed — unaffected',
    'the audit entry is kept, not hidden from its own history'
  );
});

void test('the optional wire members become the view’s required ones', () => {
  const view = toReviewCommentView(wire({}));
  assert.equal(view.anchorText, '', 'absent anchorText is the empty snapshot, never undefined');
  assert.deepEqual(toReviewThread(undefined), [], 'an absent thread is an empty one');
});

void test('replies ride through in order, with every member carried', () => {
  const view = toReviewCommentView(
    wire({ replies: [{ id: 'r1', authorRole: 'pm', text: 'done', at: '2026-09-12T10:00:00Z' }] })
  );
  assert.deepEqual(view.replies, [
    { id: 'r1', authorRole: 'pm', text: 'done', at: '2026-09-12T10:00:00Z' },
  ]);
});

void test('openThreadCount counts unresolved change requests only — questions never block approve', () => {
  const thread = [
    { type: 'changeRequest', status: 'open' },
    { type: 'changeRequest', status: 'answered' },
    { type: 'changeRequest', status: 'resolved' },
    { type: 'question', status: 'open' },
  ] as unknown as ReviewCommentView[];
  assert.equal(openThreadCount(thread), 2);
});
