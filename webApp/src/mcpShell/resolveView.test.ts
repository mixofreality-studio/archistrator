/// <reference types="node" />
/**
 * Unit tests for the MCP shell's view-registry resolution (src/mcpShell/
 * resolveView.ts). Covers the final-review IMPORTANT fix: the view id
 * project.json declares (ui.view) was dead — mcpemit only checked nil-ness and
 * the shell keyed its registry by tool name alone, two independent sources of
 * truth for the same routing decision. resolveViewKey now resolves PRIMARILY
 * from the tool's `_meta.ui.view` (which mcpemit now stamps), falling back to
 * tool name for hosts that don't forward `_meta`.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { resolveViewKey } from './resolveView.ts';

const REGISTRY_KEYS = new Set(['system-design-session', 'systemDesignGetSessionState']);
const hasKey = (key: string): boolean => REGISTRY_KEYS.has(key);

void test('resolves via the tool _meta view id when present and registered', () => {
  const res = resolveViewKey(
    { ui: { resourceUri: 'ui://archistrator/shell.html', view: 'system-design-session' } },
    'systemDesignGetSessionState',
    hasKey
  );
  assert.deepEqual(res, { key: 'system-design-session', resolvedBy: 'view' });
});

void test('falls back to tool name when _meta carries no ui.view', () => {
  const res = resolveViewKey(undefined, 'systemDesignGetSessionState', hasKey);
  assert.deepEqual(res, { key: 'systemDesignGetSessionState', resolvedBy: 'toolName' });
});

void test('falls back to tool name when the _meta view id is not itself registered', () => {
  const res = resolveViewKey(
    { ui: { view: 'some-unregistered-view' } },
    'systemDesignGetSessionState',
    hasKey
  );
  assert.deepEqual(res, { key: 'systemDesignGetSessionState', resolvedBy: 'toolName' });
});

void test('resolves to none when neither the view id nor the tool name is registered', () => {
  const res = resolveViewKey({ ui: { view: 'nope' } }, 'someOtherTool', hasKey);
  assert.deepEqual(res, { key: undefined, resolvedBy: 'none' });
});

void test('tolerates a malformed _meta.ui shape (non-object, missing view, non-string view)', () => {
  assert.deepEqual(resolveViewKey({ ui: 'not-an-object' }, 'systemDesignGetSessionState', hasKey), {
    key: 'systemDesignGetSessionState',
    resolvedBy: 'toolName',
  });
  assert.deepEqual(resolveViewKey({ ui: {} }, 'systemDesignGetSessionState', hasKey), {
    key: 'systemDesignGetSessionState',
    resolvedBy: 'toolName',
  });
  assert.deepEqual(resolveViewKey({ ui: { view: 42 } }, 'systemDesignGetSessionState', hasKey), {
    key: 'systemDesignGetSessionState',
    resolvedBy: 'toolName',
  });
});

void test('falls back to the single distinct view when the host omits toolInfo entirely (F-T11-3)', () => {
  const hasKey = (k: string): boolean =>
    k === 'system-design-session' || k === 'systemDesignGetSessionState';
  const res = resolveViewKey(undefined, undefined, hasKey, ['system-design-session']);
  assert.equal(res.resolvedBy, 'singleViewDefault');
  assert.equal(res.key, 'system-design-session');
});

void test('does NOT guess among multiple distinct views when toolInfo is absent', () => {
  const hasKey = (): boolean => true;
  const res = resolveViewKey(undefined, '', hasKey, ['view-a', 'view-b']);
  assert.equal(res.resolvedBy, 'none');
  assert.equal(res.key, undefined);
});
