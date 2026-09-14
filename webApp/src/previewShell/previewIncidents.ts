/**
 * The preview's incident log: every fixture miss and every blocked request. It
 * is how a preview fails LOUDLY (design-renderer-data.md §2′.1):
 *
 *   - each incident is logged with console.error;
 *   - the PreviewAlarm banner renders the log over the app;
 *   - `window.__ARCHISTRATOR_PREVIEW__.incidents` exposes it to a headless
 *     reader: the uitests today, and the rig's smoke gate later (§2′.2 step 3),
 *     which fails a state on any GET miss.
 *
 * A plain module store (subscribe + snapshot) so React can read it through
 * useSyncExternalStore and non-React code (the guard, the transport's onMiss)
 * can write it.
 */

export type PreviewIncidentKind = 'fixture-miss' | 'network-blocked';

export interface PreviewIncident {
  readonly kind: PreviewIncidentKind;
  readonly detail: string;
}

declare global {
  interface Window {
    __ARCHISTRATOR_PREVIEW__?: { readonly incidents: readonly PreviewIncident[] };
  }
}

let incidents: readonly PreviewIncident[] = [];
const listeners = new Set<() => void>();

export function raisePreviewIncident(incident: PreviewIncident): void {
  incidents = [...incidents, incident];
  console.error(`[archistrator preview] ${incident.kind}: ${incident.detail}`);
  if (typeof window !== 'undefined') {
    window.__ARCHISTRATOR_PREVIEW__ = { incidents };
  }
  for (const listener of listeners) listener();
}

export function subscribePreviewIncidents(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function previewIncidents(): readonly PreviewIncident[] {
  return incidents;
}
