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
 * TWO FORMS (designer recheck on renderers S2). In the pane the diagram fit to
 * 0.31 scale — unreadable. So the pane's `list` form names the callers and the
 * callees as TEXT rows, each one a navigation to the neighbour's activity, with
 * "Open diagram in focus view" on top; the `canvas` form draws in the focus view.
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
import Button from '@mui/material/Button';
import Link from '@mui/material/Link';
import Typography from '@mui/material/Typography';
import OpenInFullRoundedIcon from '@mui/icons-material/OpenInFullRounded';

import type { ArtifactModelEnvelope } from '../../contracts/types';
import { toC4View, type C4View } from '../../contracts/adapters';
import { useTokens } from '../../utilities/theme/ThemeContext';
import type { Tokens } from '../../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';
import { PerspectiveFlow } from '../flow/PerspectiveFlow';
import {
  neighbourRowsFor,
  utilityNeighbours,
  withoutUtilities,
  type NeighbourRow,
} from './relationshipsView.ts';

export const RELATIONSHIPS_CAPTION =
  'From the committed architecture (system · relationships) — who calls this component and whom it calls.';

export function ComponentRelationshipsView({
  systemEnvelope,
  componentId,
  height = 420,
  onFocusComponent,
  isNavigable,
  destinationOf,
  caption = RELATIONSHIPS_CAPTION,
  testId = UI_IDENTIFIERS.ServiceContract.COMPONENT_FLOW,
  mode = 'canvas',
  onOpenFocus,
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
  /** The activity a neighbour's row opens, said at the row's end (`→ C-review-engine`). */
  destinationOf?: ((componentId: string) => string | undefined) | undefined;
  caption?: string;
  testId?: string;
  /** `list` in the pane (text rows), `canvas` in the focus view. */
  mode?: 'canvas' | 'list';
  /** The list's "Open diagram in focus view"; absent inside the focus view. */
  onOpenFocus?: (() => void) | undefined;
}): ReactNode {
  const t = useTokens();
  const full = useMemo(() => toC4View(systemEnvelope), [systemEnvelope]);
  const view: C4View = useMemo(() => withoutUtilities(full, componentId), [full, componentId]);
  const utilities = useMemo(() => utilityNeighbours(full, componentId), [full, componentId]);
  const rows = useMemo(() => neighbourRowsFor(full, componentId), [full, componentId]);
  const present = view.components.some((c) => c.id === componentId);
  const edges = view.relationships.filter((r) => r.from === componentId || r.to === componentId);
  const navigable = (id: string): boolean =>
    onFocusComponent !== undefined && (isNavigable === undefined || isNavigable(id));
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
      data-mode={mode}
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
      {mode === 'list' && onOpenFocus !== undefined ? (
        <Box>
          <Button
            data-testid={UI_IDENTIFIERS.ServiceContract.OPEN_FOCUS}
            size="small"
            startIcon={<OpenInFullRoundedIcon sx={{ fontSize: 14 }} />}
            sx={{ fontFamily: t.mono, fontSize: 11, fontWeight: 700, textTransform: 'none' }}
            variant="outlined"
            onClick={onOpenFocus}
          >
            Open diagram in focus view
          </Button>
        </Box>
      ) : null}
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
      ) : mode === 'list' ? (
        <>
          <NeighbourList
            destinationOf={destinationOf}
            label="CALLED BY"
            navigable={navigable}
            rows={rows.callers}
            t={t}
            onFocusComponent={onFocusComponent}
          />
          <NeighbourList
            destinationOf={destinationOf}
            label="CALLS"
            navigable={navigable}
            rows={rows.callees}
            t={t}
            onFocusComponent={onFocusComponent}
          />
        </>
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
          {`Utilities it uses: ${utilities.join(' · ')}`}
        </Typography>
      ) : null}
    </Box>
  );
}

/** One side (callers or callees) as text rows; a row that navigates is a button. */
function NeighbourList({
  label,
  rows,
  navigable,
  destinationOf,
  onFocusComponent,
  t,
}: {
  label: string;
  rows: NeighbourRow[];
  navigable: (id: string) => boolean;
  destinationOf: ((componentId: string) => string | undefined) | undefined;
  onFocusComponent: ((componentId: string) => void) | undefined;
  t: Tokens;
}): ReactNode {
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.25, minWidth: 0 }}>
      <Typography
        sx={{
          fontFamily: t.mono,
          fontSize: 10,
          fontWeight: 700,
          letterSpacing: '0.08em',
          color: t.muted,
        }}
      >
        {`${label} · ${String(rows.length)}`}
      </Typography>
      {rows.length === 0 ? (
        <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.muted }}>None.</Typography>
      ) : (
        <Box
          component="ul"
          sx={{
            m: 0,
            p: 0,
            listStyle: 'none',
            border: `1px solid ${t.line}`,
            borderRadius: '8px',
            bgcolor: t.paperAlt,
            overflow: 'hidden',
          }}
        >
          {rows.map((row, i) => {
            const content = (
              <>
                <Box component="span" sx={{ fontWeight: 700, color: t.ink }}>
                  {row.name}
                </Box>
                <Box component="span" sx={{ color: t.muted }}>{` · ${row.layer}`}</Box>
                {row.operations.length > 0 ? (
                  <Box
                    component="span"
                    sx={{ display: 'block', color: t.muted, fontSize: 11, wordBreak: 'break-word' }}
                  >
                    {row.operations.join(' · ')}
                  </Box>
                ) : null}
              </>
            );
            const goes = navigable(row.id);
            const destination = goes ? destinationOf?.(row.id) : undefined;
            return (
              <Box
                component="li"
                key={row.id}
                sx={{ borderTop: i === 0 ? 'none' : `1px solid ${t.line}` }}
              >
                {goes && onFocusComponent !== undefined ? (
                  <Link
                    component="button"
                    data-testid={UI_IDENTIFIERS.ServiceContract.neighbourRow(row.id)}
                    sx={{
                      display: 'block',
                      width: '100%',
                      px: 1,
                      py: 0.6,
                      fontFamily: t.mono,
                      fontSize: 12,
                      textAlign: 'left',
                      color: t.ink,
                      '&:hover': { bgcolor: t.paper },
                      '&:focus-visible': { outline: `2px solid ${t.accent}`, outlineOffset: -2 },
                    }}
                    underline="hover"
                    onClick={() => {
                      onFocusComponent(row.id);
                    }}
                  >
                    {content}
                    {/* Where the row goes, said at its end (as the reached-through rows). */}
                    <Box
                      component="span"
                      sx={{ ml: 0.75, color: t.accent2, fontWeight: 700, whiteSpace: 'nowrap' }}
                    >
                      {`→ ${destination ?? 'open'}`}
                    </Box>
                  </Link>
                ) : (
                  <Typography
                    data-testid={UI_IDENTIFIERS.ServiceContract.neighbourRow(row.id)}
                    sx={{ px: 1, py: 0.6, fontFamily: t.mono, fontSize: 12 }}
                  >
                    {content}
                  </Typography>
                )}
              </Box>
            );
          })}
        </Box>
      )}
    </Box>
  );
}
