/**
 * The honest in-frame error page for a preview URL that names no recorded state
 * (design-renderer-data.md §2′.1): it says what was asked for, why it cannot be
 * shown, and which screen × state pairs this build does carry.
 */
import type { ReactNode } from 'react';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';
import type { PreviewResolution } from './fixtureRegistry';

type Unresolved = Exclude<PreviewResolution, { kind: 'ok' }>;

function reason(r: Unresolved): string {
  switch (r.kind) {
    case 'no-fixtures':
      return 'This preview build carries no fixture states.';
    case 'missing-params':
      return 'Name a screen and a state: ?screen=<id>&state=<id>.';
    case 'unknown-screen':
      return `This build has no screen "${r.screen ?? ''}".`;
    case 'unknown-state':
      return `Screen "${r.screen ?? ''}" has no state "${r.state ?? ''}".`;
  }
}

export function PreviewErrorPage({ resolution }: { resolution: Unresolved }): ReactNode {
  return (
    <main
      data-testid={UI_IDENTIFIERS.Preview.ERROR_PAGE}
      style={{
        maxWidth: 720,
        margin: '48px auto',
        padding: '0 24px',
        font: '14px/1.6 Inter, system-ui, sans-serif',
        color: '#1f2328',
      }}
    >
      <h1 style={{ fontSize: 20 }}>No preview for this address</h1>
      <p>{reason(resolution)}</p>
      {resolution.available.length > 0 ? (
        <>
          <p>States in this build:</p>
          <ul>
            {resolution.available.flatMap(({ screen, states }) =>
              states.map((state) => (
                <li key={`${screen}/${state}`}>
                  <a
                    href={`?screen=${encodeURIComponent(screen)}&state=${encodeURIComponent(state)}`}
                  >
                    {screen} · {state}
                  </a>
                </li>
              ))
            )}
          </ul>
        </>
      ) : null}
    </main>
  );
}
