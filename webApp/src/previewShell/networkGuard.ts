/**
 * The preview's network guard (design-renderer-data.md §2′.1): in a preview, the
 * fixture transport is the ONLY I/O. Every other way a page can reach the network
 * throws PreviewNetworkBlockedError instead of sending anything, and reports the
 * attempt so the preview's alarm can show it. It catches any future bypass of the
 * OpsClient seam, the way the three raw fetches this change closed would have been
 * caught.
 *
 * It is defence in depth. The preview document's own CSP (preview.html) also sets
 * `connect-src 'none'`, and the future Go preview listener's response CSP will too
 * (§2′.3). The guard is the layer that fails LOUDLY and names the caller's URL.
 *
 * installNetworkGuard runs from installNetworkGuard.ts, the preview entry's FIRST
 * import, so it is in place before any other module evaluates (openapi-fetch, for
 * one, captures `globalThis.fetch` when a client is created).
 */

/** Present in every blocked-request message; scripts/check-prod-bundle.mjs greps for it. */
export const NETWORK_GUARD_MARKER = 'archistrator-preview-network-guard';

/** A request the preview refused to send. */
export class PreviewNetworkBlockedError extends Error {
  readonly channel: string;
  readonly target: string;

  constructor(channel: string, target: string) {
    super(
      `${NETWORK_GUARD_MARKER}: ${channel} ${target} was blocked. A preview answers every ` +
        'read from its fixtures through the OpsClient; nothing reaches the network.'
    );
    this.name = 'PreviewNetworkBlockedError';
    this.channel = channel;
    this.target = target;
  }
}

function describe(input: unknown): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.href;
  if (typeof input === 'object' && input !== null && 'url' in input) {
    if (typeof input.url === 'string') return input.url;
  }
  return String(input);
}

/**
 * Replaces fetch, XMLHttpRequest, WebSocket, EventSource and navigator.sendBeacon
 * on `target` (the window) with versions that throw. `onBlocked` hears about every
 * attempt before it throws.
 */
export function installNetworkGuard(
  target: Window,
  onBlocked: (error: PreviewNetworkBlockedError) => void
): void {
  const block = (channel: string, url: string): PreviewNetworkBlockedError => {
    const error = new PreviewNetworkBlockedError(channel, url);
    onBlocked(error);
    return error;
  };

  // fetch rejects, as a refused real fetch does, so callers' error paths run.
  target.fetch = (input: RequestInfo | URL): Promise<Response> =>
    Promise.reject(block('fetch', describe(input)));

  // The constructors themselves throw: nothing can even open a connection.
  const refuseConstructor = (channel: string): new (url?: unknown) => never =>
    function Blocked(url?: unknown): never {
      throw block(channel, url === undefined ? '(constructed)' : describe(url));
    } as unknown as new (url?: unknown) => never;

  Object.defineProperty(target, 'XMLHttpRequest', {
    configurable: true,
    writable: true,
    value: refuseConstructor('XMLHttpRequest'),
  });
  Object.defineProperty(target, 'WebSocket', {
    configurable: true,
    writable: true,
    value: refuseConstructor('WebSocket'),
  });
  Object.defineProperty(target, 'EventSource', {
    configurable: true,
    writable: true,
    value: refuseConstructor('EventSource'),
  });
  Object.defineProperty(target.navigator, 'sendBeacon', {
    configurable: true,
    writable: true,
    value: (url: string | URL): never => {
      throw block('sendBeacon', describe(url));
    },
  });
  // A second browsing context is a nested preview (or an exit from the fixture
  // world); navigationGuard.ts refuses the link form, this the scripted form.
  Object.defineProperty(target, 'open', {
    configurable: true,
    writable: true,
    value: (url?: string | URL): never => {
      throw block('window.open', url === undefined ? '(blank)' : describe(url));
    },
  });
}
