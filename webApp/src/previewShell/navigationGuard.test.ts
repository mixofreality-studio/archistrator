/// <reference types="node" />
/**
 * The preview's navigation guard decision (src/previewShell/navigationGuard.ts):
 * only an activation that would open another browsing context is refused.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { opensNewContext, type LinkActivation } from './navigationGuard.ts';

const plain: LinkActivation = {
  target: '',
  button: 0,
  ctrlKey: false,
  metaKey: false,
  shiftKey: false,
};

void test('an in-app link, clicked plainly, stays in the preview', () => {
  assert.equal(opensNewContext(plain), false);
  assert.equal(opensNewContext({ ...plain, target: '_self' }), false);
  assert.equal(opensNewContext({ ...plain, target: ' _SELF ' }), false);
});

void test('a link that targets another context is refused', () => {
  assert.equal(opensNewContext({ ...plain, target: '_blank' }), true);
  assert.equal(opensNewContext({ ...plain, target: '_top' }), true);
  assert.equal(opensNewContext({ ...plain, target: '_parent' }), true);
  assert.equal(opensNewContext({ ...plain, target: 'some-window' }), true);
});

void test('a click the browser would turn into a new tab is refused', () => {
  assert.equal(opensNewContext({ ...plain, button: 1 }), true, 'middle click');
  assert.equal(opensNewContext({ ...plain, ctrlKey: true }), true);
  assert.equal(opensNewContext({ ...plain, metaKey: true }), true);
  assert.equal(opensNewContext({ ...plain, shiftKey: true }), true, 'shift opens a window');
});
