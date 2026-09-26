/// <reference types="node" />
/**
 * The preview's network guard (src/previewShell/networkGuard.ts): every channel
 * throws, names its target, reports it, and the original is never reached.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  NETWORK_GUARD_MARKER,
  PreviewNetworkBlockedError,
  installNetworkGuard,
} from './networkGuard.ts';

interface FakeWindow {
  fetch: (input: unknown) => Promise<unknown>;
  XMLHttpRequest: new () => unknown;
  WebSocket: new (url: string) => unknown;
  EventSource: new (url: string) => unknown;
  navigator: { sendBeacon: (url: string) => boolean };
  open: (url?: string) => unknown;
}

function guarded(): { win: FakeWindow; blocked: PreviewNetworkBlockedError[]; reached: string[] } {
  const reached: string[] = [];
  const win: FakeWindow = {
    fetch: (input) => {
      reached.push(`fetch ${String(input)}`);
      return Promise.resolve('sent');
    },
    XMLHttpRequest: class {
      readonly channel = 'xhr';
      constructor() {
        reached.push(this.channel);
      }
    },
    WebSocket: class {
      readonly channel = 'ws';
      constructor(url: string) {
        reached.push(`${this.channel} ${url}`);
      }
    },
    EventSource: class {
      readonly channel = 'sse';
      constructor(url: string) {
        reached.push(`${this.channel} ${url}`);
      }
    },
    navigator: {
      sendBeacon: (url) => {
        reached.push(`beacon ${url}`);
        return true;
      },
    },
    open: (url) => {
      reached.push(`open ${String(url)}`);
      return {};
    },
  };
  const blocked: PreviewNetworkBlockedError[] = [];
  installNetworkGuard(win as unknown as Window, (e) => {
    blocked.push(e);
  });
  return { win, blocked, reached };
}

function isBlocked(channel: string, target: string): (err: unknown) => boolean {
  return (err: unknown) => {
    assert.ok(err instanceof PreviewNetworkBlockedError);
    assert.equal(err.channel, channel);
    assert.equal(err.target, target);
    assert.ok(err.message.includes(NETWORK_GUARD_MARKER));
    return true;
  };
}

void test('fetch rejects and never reaches the network', async () => {
  const { win, blocked, reached } = guarded();
  await assert.rejects(
    win.fetch('/api/v1/delivery/query-project-view'),
    isBlocked('fetch', '/api/v1/delivery/query-project-view')
  );
  await assert.rejects(
    win.fetch(new URL('https://example.test/a')),
    isBlocked('fetch', 'https://example.test/a')
  );
  await assert.rejects(
    win.fetch(new Request('https://example.test/b')),
    isBlocked('fetch', 'https://example.test/b')
  );
  assert.equal(blocked.length, 3);
  assert.deepEqual(reached, []);
});

void test('XMLHttpRequest, WebSocket and EventSource cannot even be constructed', () => {
  const { win, blocked, reached } = guarded();
  assert.throws(() => new win.XMLHttpRequest(), isBlocked('XMLHttpRequest', '(constructed)'));
  assert.throws(
    () => new win.WebSocket('ws://localhost:1/x'),
    isBlocked('WebSocket', 'ws://localhost:1/x')
  );
  assert.throws(() => new win.EventSource('/events'), isBlocked('EventSource', '/events'));
  assert.equal(blocked.length, 3);
  assert.deepEqual(reached, []);
});

void test('sendBeacon throws and never sends', () => {
  const { win, blocked, reached } = guarded();
  assert.throws(() => win.navigator.sendBeacon('/beacon'), isBlocked('sendBeacon', '/beacon'));
  assert.equal(blocked.length, 1);
  assert.deepEqual(reached, []);
});

void test('window.open throws: a preview never opens a second browsing context', () => {
  const { win, blocked, reached } = guarded();
  assert.throws(
    () => win.open('/index.html?screen=a&state=b'),
    isBlocked('window.open', '/index.html?screen=a&state=b')
  );
  assert.throws(() => win.open(), isBlocked('window.open', '(blank)'));
  assert.equal(blocked.length, 2);
  assert.deepEqual(reached, []);
});
