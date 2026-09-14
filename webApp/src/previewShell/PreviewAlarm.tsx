/**
 * The preview's LOUD failure banner: pinned over the app, listing every fixture
 * miss and blocked request so far (previewIncidents.ts). It renders nothing while
 * the preview is clean, so a clean state's screenshot is the app alone.
 */
import { useSyncExternalStore, type ReactNode } from 'react';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';
import { previewIncidents, subscribePreviewIncidents } from './previewIncidents';

const SHOWN = 6;

export function PreviewAlarm(): ReactNode {
  const incidents = useSyncExternalStore(subscribePreviewIncidents, previewIncidents);
  if (incidents.length === 0) return null;
  return (
    <div
      data-testid={UI_IDENTIFIERS.Preview.ALARM}
      role="alert"
      style={{
        position: 'fixed',
        left: 12,
        right: 12,
        bottom: 12,
        zIndex: 2147483647,
        padding: '10px 14px',
        background: '#7f1d1d',
        color: '#fff',
        font: '12px/1.5 ui-monospace, SFMono-Regular, Menlo, monospace',
        borderRadius: 6,
        boxShadow: '0 4px 16px rgba(0,0,0,0.35)',
      }}
    >
      <strong>
        Preview incident{incidents.length === 1 ? '' : 's'} ({incidents.length}): this state is not
        fully answered by its fixtures.
      </strong>
      <ul style={{ margin: '4px 0 0', paddingLeft: 18 }}>
        {incidents.slice(0, SHOWN).map((incident, i) => (
          // Incidents are append-only, so the index is a stable key.
          <li key={i}>
            {incident.kind}: {incident.detail}
          </li>
        ))}
        {incidents.length > SHOWN ? <li>…and {incidents.length - SHOWN} more</li> : null}
      </ul>
    </div>
  );
}
