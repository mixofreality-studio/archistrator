/**
 * Side-effect module: installs the network guard on the page. It MUST be the
 * preview entry's first import (previewShell/main.tsx), so the guard is in place
 * before any other module evaluates.
 */
import { installNetworkGuard } from './networkGuard';
import { raisePreviewIncident } from './previewIncidents';

window.__ARCHISTRATOR_PREVIEW__ = { incidents: [] };

installNetworkGuard(window, (error) => {
  raisePreviewIncident({ kind: 'network-blocked', detail: `${error.channel} ${error.target}` });
});
