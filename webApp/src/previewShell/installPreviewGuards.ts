/**
 * Side-effect module: installs the preview's guards on the page. It MUST be the
 * preview entry's first import (previewShell/main.tsx), so both are in place
 * before any other module evaluates.
 *
 *   - the network guard (networkGuard.ts): fetch, XHR, WebSocket, EventSource,
 *     sendBeacon and window.open throw instead of reaching anything;
 *   - the navigation guard (navigationGuard.ts): a link that would open another
 *     tab or window, a nested preview among them, is refused.
 *
 * Every refusal is raised as a preview incident, so it shows on the alarm.
 */
import { installNavigationGuard } from './navigationGuard';
import { installNetworkGuard } from './networkGuard';
import { raisePreviewIncident } from './previewIncidents';

window.__ARCHISTRATOR_PREVIEW__ = { incidents: [] };

installNetworkGuard(window, (error) => {
  raisePreviewIncident({ kind: 'network-blocked', detail: `${error.channel} ${error.target}` });
});

installNavigationGuard(document, (href) => {
  raisePreviewIncident({ kind: 'navigation-blocked', detail: href });
});
