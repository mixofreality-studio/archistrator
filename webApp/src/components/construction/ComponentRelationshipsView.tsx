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
 *
 * UTILITIES ARE A LINE, NOT NODES (designer check on renderers S1, polish 2).
 * Every component may use Logging, Diagnostics and the MessageBus; drawn, they
 * were a side bar of nodes that said nothing about THIS component and led
 * nowhere on click (no activity builds a utility). They are named in one muted
 * line under the diagram instead, and not drawn.
 *
 * The diagram REMOUNTS per component (polish 9): a neighbour click moves the
 * selection, and a kept instance left the clicked node focused, so its Comment
 * toolbar stayed up — over an edge — on the new view.
 */
import { useMemo, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';

import type { ArtifactModelEnvelope } from '../../contracts/types';
import { toC4View, type C4View } from '../../contracts/adapters';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';
import { PerspectiveFlow } from '../flow/PerspectiveFlow';
import { utilityNeighbours, withoutUtilities } from './relationshipsView.ts';

export const RELATIONSHIPS_CAPTION =
  'From the committed architecture (system · relationships) — who calls this component and whom it calls.';

export function ComponentRelationshipsView({
  systemEnvelope,
  componentId,
  height = 420,
  onFocusComponent,
  isNavigable,
  caption = RELATIONSHIPS_CAPTION,
  testId = UI_IDENTIFIERS.ServiceContract.COMPONENT_FLOW,
}: {
  systemEnvelope: ArtifactModelEnvelope | undefined;
  /** The slot-5 component id (kebab) — the join's own, never re-derived from a name. */
  componentId: string;
  height?: number | undefined;
  /** Move the selection onto a neighbour; absent, a neighbour click does nothing. */
  onFocusComponent?: ((componentId: string) => void) | undefined;
  /**
   * Whether a click on this neighbour goes anywhere (an activity builds it). A
   * node that does not navigate shows no pointer: a pointer that leads nowhere
   * is a promise the diagram cannot keep.
   */
  isNavigable?: ((componentId: string) => boolean) | undefined;
  caption?: string;
  testId?: string;
}): ReactNode {
  const t = useTokens();
  const full = useMemo(() => toC4View(systemEnvelope), [systemEnvelope]);
  const view: C4View = useMemo(() => withoutUtilities(full, componentId), [full, componentId]);
  const utilities = useMemo(() => utilityNeighbours(full, componentId), [full, componentId]);
  const present = view.components.some((c) => c.id === componentId);
  const edges = view.relationships.filter((r) => r.from === componentId || r.to === componentId);
  const deadIds = useMemo(
    () =>
      new Set(
        view.components
          .filter(
            (c) =>
              c.id === componentId ||
              onFocusComponent === undefined ||
              (isNavigable !== undefined && !isNavigable(c.id))
          )
          .map((c) => c.id)
      ),
    [view.components, componentId, onFocusComponent, isNavigable]
  );

  return (
    <Box
      data-component-id={componentId}
      data-edge-count={String(edges.length)}
      data-testid={testId}
      sx={{
        display: 'flex',
        flexDirection: 'column',
        gap: 1,
        minWidth: 0,
        // A node whose click goes nowhere (the focal one, or a neighbour no
        // activity builds) shows the default cursor, not xyflow's pointer.
        ...Object.fromEntries(
          [...deadIds].map((id) => [
            `& .react-flow__node[data-id="${id}"]`,
            { cursor: 'default !important' },
          ])
        ),
      }}
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
          The committed architecture draws no relationship to or from {componentId}
          {utilities.length > 0 ? ' other than to the utilities below' : ''}.
        </Typography>
      ) : (
        <PerspectiveFlow
          componentId={componentId}
          height={height}
          key={componentId}
          view={view}
          {...(onFocusComponent !== undefined ? { onFocusComponent } : {})}
        />
      )}
      {utilities.length > 0 ? (
        <Typography
          data-testid={UI_IDENTIFIERS.ServiceContract.UTILITIES_LINE}
          sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted, lineHeight: 1.5 }}
        >
          {`Utilities any component may use: ${utilities.join(' · ')}`}
        </Typography>
      ) : null}
    </Box>
  );
}
