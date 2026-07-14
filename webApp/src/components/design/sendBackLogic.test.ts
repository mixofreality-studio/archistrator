/// <reference types="node" />
/**
 * Unit tests for GatePanel's "Send back" disabled logic (src/components/design/
 * sendBackLogic.ts). Covers the final-review CRITICAL fix: MCP's send-back was
 * permanently unreachable because McpSystemDesignContainer hardcodes
 * commentSurface.commentCount to 0 (it collects reject feedback AFTER the click,
 * via its own composer) while GatePanel unconditionally disabled the button at
 * commentCount === 0. `allowEmptySendBack` (MCP: true) unblocks the click while
 * leaving the SPA's byte-identical default (false) behavior untouched.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { sendBackDisabled } from './sendBackLogic.ts';

void test('SPA default (allowEmptySendBack=false): disabled with zero accumulated comments', () => {
  assert.equal(sendBackDisabled(false, 0, false), true);
});

void test('SPA default (allowEmptySendBack=false): enabled once a comment is accumulated', () => {
  assert.equal(sendBackDisabled(false, 1, false), false);
});

void test('MCP (allowEmptySendBack=true): enabled even at zero accumulated comments', () => {
  assert.equal(sendBackDisabled(false, 0, true), false);
});

void test('a pending decision mutation disables the button regardless of allowEmptySendBack', () => {
  assert.equal(sendBackDisabled(true, 5, true), true);
  assert.equal(sendBackDisabled(true, 5, false), true);
});
