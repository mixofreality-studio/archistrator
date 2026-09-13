/**
 * ONE component and its direct callers and callees, drawn from the COMMITTED
 * ARCHITECTURE (`system.relationships`) — the contract's Component tab and the
 * Resource rows' WHO REACHES IT card (designer Q7, §2.4).
 *
 * It reuses the design page's own PerspectiveFlow (the Component focus), so the
 * visual language and the op-name edge labels are the architecture's. It used to
 * be ContractComponentFlow over the contract's own `inbound`/`outbound` fields,
 * which are empty for all 29 contracts: every Component tab read "no
 * relationships" while the architecture holds 74 of them. One source now.
 */
import { useMemo, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';

import type { ArtifactModelEnvelope } from '../../contracts/types';
import { toC4View } from '../../contracts/adapters';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';
import { PerspectiveFlow } from '../flow/PerspectiveFlow';

export const RELATIONSHIPS_CAPTION =
  'From the committed architecture (system · relationships) — who calls this component and whom it calls.';

export function ComponentRelationshipsView({
  systemEnvelope,
  componentId,
  height = 420,
  onFocusComponent,
  caption = RELATIONSHIPS_CAPTION,
  testId = UI_IDENTIFIERS.ServiceContract.COMPONENT_FLOW,
}: {
  systemEnvelope: ArtifactModelEnvelope | undefined;
  /** The slot-5 component id (kebab) — the join's own, never re-derived from a name. */
  componentId: string;
  height?: number | undefined;
  /** Move the selection onto a neighbour; absent, a neighbour click does nothing. */
  onFocusComponent?: ((componentId: string) => void) | undefined;
  caption?: string;
  testId?: string;
}): ReactNode {
  const t = useTokens();
  const view = useMemo(() => toC4View(systemEnvelope), [systemEnvelope]);
  const present = view.components.some((c) => c.id === componentId);
  const edges = view.relationships.filter((r) => r.from === componentId || r.to === componentId);

  return (
    <Box
      data-component-id={componentId}
      data-edge-count={String(edges.length)}
      data-testid={testId}
      sx={{ display: 'flex', flexDirection: 'column', gap: 1, minWidth: 0 }}
    >
      <Typography sx={{ fontFamily: t.body, fontSize: 11.5, color: t.muted, lineHeight: 1.45 }}>
        {caption}
      </Typography>
      {!present ? (
        <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.muted }}>
          The committed architecture does not hold {componentId}, so there is nothing to draw.
        </Typography>
      ) : edges.length === 0 ? (
        <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.muted }}>
          The committed architecture draws no relationship to or from {componentId}.
        </Typography>
      ) : (
        <PerspectiveFlow
          componentId={componentId}
          height={height}
          view={view}
          {...(onFocusComponent !== undefined ? { onFocusComponent } : {})}
        />
      )}
    </Box>
  );
}
