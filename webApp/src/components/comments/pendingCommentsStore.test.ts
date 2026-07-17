/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  loadPending,
  savePending,
  storageKeyFor,
  type PendingCommentStorage,
} from './pendingCommentsStore.ts';
import type { PostedComment } from './commentContextTypes.ts';

/** In-memory Web-Storage stand-in for node:test. */
function memoryStorage(): PendingCommentStorage & { map: Map<string, string> } {
  const map = new Map<string, string>();
  return {
    map,
    getItem: (k): string | null => map.get(k) ?? null,
    setItem: (k, v): void => {
      map.set(k, v);
    },
    removeItem: (k): void => {
      map.delete(k);
    },
  };
}

const KEY = 'proj-1:mission';
const FREEFORM: PostedComment = {
  text: 'Should the weekly review cadence be configurable per user?',
  anchor: null,
};
const ANCHORED: PostedComment = {
  text: 'rename this',
  anchor: {
    kind: 'node',
    label: 'GtdManager',
    source: 'Architecture · C4',
    jsonPath: '$.components[id=GtdManager]',
  },
};
const COMMENTS: PostedComment[] = [FREEFORM, ANCHORED];

void test('save/load round-trip within the same incarnation (same Version)', () => {
  const storage = memoryStorage();
  savePending(storage, KEY, COMMENTS, 7);
  assert.deepStrictEqual(loadPending(storage, KEY, 7), COMMENTS);
});

void test('load keeps entries when the project has advanced (current Version > stamp)', () => {
  const storage = memoryStorage();
  savePending(storage, KEY, COMMENTS, 7);
  assert.deepStrictEqual(loadPending(storage, KEY, 12), COMMENTS);
});

void test('F-QA2-5: entries stamped by a previous incarnation (stamp > current Version) are dropped and the slot removed', () => {
  const storage = memoryStorage();
  // Old incarnation persisted at Version 42; the recreated project reads at Version 2.
  savePending(storage, KEY, COMMENTS, 42);
  assert.deepStrictEqual(loadPending(storage, KEY, 2), []);
  assert.strictEqual(storage.map.has(storageKeyFor(KEY)), false, 'stale slot must be removed');
});

void test('legacy pre-envelope format (bare array, no incarnation stamp) is dropped and removed', () => {
  const storage = memoryStorage();
  storage.setItem(storageKeyFor(KEY), JSON.stringify(COMMENTS));
  assert.deepStrictEqual(loadPending(storage, KEY, 5), []);
  assert.strictEqual(storage.map.has(storageKeyFor(KEY)), false);
});

void test('malformed envelope (missing projectVersion) is dropped and removed', () => {
  const storage = memoryStorage();
  storage.setItem(storageKeyFor(KEY), JSON.stringify({ comments: COMMENTS }));
  assert.deepStrictEqual(loadPending(storage, KEY, 5), []);
  assert.strictEqual(storage.map.has(storageKeyFor(KEY)), false);
});

void test('corrupt JSON degrades to empty (no throw)', () => {
  const storage = memoryStorage();
  storage.setItem(storageKeyFor(KEY), '{not json');
  assert.deepStrictEqual(loadPending(storage, KEY, 5), []);
});

void test('missing slot loads empty', () => {
  const storage = memoryStorage();
  assert.deepStrictEqual(loadPending(storage, KEY, 5), []);
});

void test('saving an empty list removes the slot (no orphan)', () => {
  const storage = memoryStorage();
  savePending(storage, KEY, COMMENTS, 3);
  savePending(storage, KEY, [], 3);
  assert.strictEqual(storage.map.has(storageKeyFor(KEY)), false);
});

void test('null storage (sandboxed iframe / storage disabled) is a no-op', () => {
  assert.deepStrictEqual(loadPending(null, KEY, 5), []);
  assert.doesNotThrow(() => {
    savePending(null, KEY, COMMENTS, 5);
  });
});

void test('slots are isolated per (project, kind) key', () => {
  const storage = memoryStorage();
  savePending(storage, 'proj-1:mission', COMMENTS, 3);
  savePending(storage, 'proj-1:glossary', [ANCHORED], 3);
  assert.deepStrictEqual(loadPending(storage, 'proj-1:mission', 3), COMMENTS);
  assert.deepStrictEqual(loadPending(storage, 'proj-1:glossary', 3), [ANCHORED]);
});
