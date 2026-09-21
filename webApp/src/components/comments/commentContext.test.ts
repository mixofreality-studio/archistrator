/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { DISABLED_COMMENT_CTX } from './disabledCommentContext.ts';
import { loadPending, savePending, type PendingCommentStorage } from './pendingCommentsStore.ts';
import type { PostedComment } from './commentContextTypes.ts';

/** A `Map`-backed double matching the `PendingCommentStorage` shape `pendingCommentsStore`
 *  expects, so these tests can exercise the persistence round-trip under node:test without
 *  a real `localStorage`. */
function memoryStore(): PendingCommentStorage {
  const map = new Map<string, string>();
  return {
    getItem: (key: string): string | null => map.get(key) ?? null,
    setItem: (key: string, value: string): void => {
      map.set(key, value);
    },
    removeItem: (key: string): void => {
      map.delete(key);
    },
  };
}

void test('DISABLED_COMMENT_CTX — enabled === false', () => {
  assert.strictEqual(DISABLED_COMMENT_CTX.enabled, false);
});

void test('DISABLED_COMMENT_CTX — comments array is empty', () => {
  assert.strictEqual(Array.isArray(DISABLED_COMMENT_CTX.comments), true);
  assert.strictEqual(DISABLED_COMMENT_CTX.comments.length, 0);
});

void test('DISABLED_COMMENT_CTX — anchor is null', () => {
  assert.strictEqual(DISABLED_COMMENT_CTX.anchor, null);
});

void test('DISABLED_COMMENT_CTX — requestId is 0', () => {
  assert.strictEqual(DISABLED_COMMENT_CTX.requestId, 0);
});

void test('DISABLED_COMMENT_CTX — setAnchor is callable (no-op)', () => {
  // Should not throw.
  assert.doesNotThrow(() => {
    DISABLED_COMMENT_CTX.setAnchor(null);
  });
  assert.doesNotThrow(() => {
    DISABLED_COMMENT_CTX.setAnchor({
      kind: 'text',
      label: 'test',
      source: 'test',
      jsonPath: '$.test',
    });
  });
});

void test('DISABLED_COMMENT_CTX — post is callable (no-op)', () => {
  // Should not throw.
  assert.doesNotThrow(() => {
    DISABLED_COMMENT_CTX.post('test comment');
  });
});

void test('DISABLED_COMMENT_CTX — reset is callable (no-op)', () => {
  // Should not throw.
  assert.doesNotThrow(() => {
    DISABLED_COMMENT_CTX.reset();
  });
});

void test('DISABLED_COMMENT_CTX — toWire returns empty array', () => {
  const result = DISABLED_COMMENT_CTX.toWire();
  assert.strictEqual(Array.isArray(result), true);
  assert.strictEqual(result.length, 0);
});

void test('DISABLED_COMMENT_CTX — freeformNotes returns empty string', () => {
  const result = DISABLED_COMMENT_CTX.freeformNotes();
  assert.strictEqual(result, '');
});

void test('DISABLED_COMMENT_CTX — pendingQuestions returns empty array', () => {
  const result = DISABLED_COMMENT_CTX.pendingQuestions();
  assert.strictEqual(Array.isArray(result), true);
  assert.strictEqual(result.length, 0);
});

void test('DISABLED_COMMENT_CTX — required interface members exist', () => {
  // Spot-check key members of the CommentCtx interface.
  const requiredMembers = [
    'enabled',
    'comments',
    'anchor',
    'setAnchor',
    'setDraftPending',
    'post',
    'remove',
    'reset',
    'clearQuestions',
    'setActiveKey',
    'toWire',
    'freeformNotes',
    'pendingQuestions',
    'requestId',
  ];
  for (const member of requiredMembers) {
    assert.ok(
      member in DISABLED_COMMENT_CTX,
      `DISABLED_COMMENT_CTX missing required member: ${member}`
    );
  }
});

// NOTE: Full renderHook-based useComments missing-provider test is earmarked
// pending a component-test harness setup. This file tests only the exported
// disabled-context object's invariants, avoiding JSX/React rendering.

// NOTE: Full renderHook-based useComments missing-provider test is earmarked
// pending a component-test harness setup. This file tests only the exported
// disabled-context object's invariants, avoiding JSX/React rendering.

void test('a staged reply carries replyTo and survives a persist round-trip', () => {
  const store = memoryStore();
  const staged: PostedComment[] = [
    { text: 'Still too vague', anchor: null, commentType: 'changeRequest', replyTo: 'r1c1' },
  ];
  savePending(store, 'proj:mission', staged, 7);
  const loaded = loadPending(store, 'proj:mission', 7);
  assert.equal(loaded.length, 1);
  assert.equal(loaded[0]?.replyTo, 'r1c1');
});

void test('a new thread stages with no replyTo', () => {
  const store = memoryStore();
  savePending(
    store,
    'proj:mission',
    [{ text: 'Objective 3 is vague', anchor: null, commentType: 'changeRequest' }],
    7
  );
  assert.equal(loadPending(store, 'proj:mission', 7)[0]?.replyTo, undefined);
});
