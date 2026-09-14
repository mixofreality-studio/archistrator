/**
 * ServiceContractView — the 4-tab C4 navigator for a SERVICE activity's contract.
 *
 * Props: the contract the activity → contract JOIN resolved (contracts/
 * serviceContracts.ts: componentId → the component's contractKey), and that
 * component's id.
 *
 * Structure:
 *   1. VolatilityCard  — stereotype banner + component name + a status chip ONLY
 *                        when the contract records one + volatility text
 *   2. 4-tab ToggleButtonGroup  — Code | Component | Dynamic | Facets
 *   3. Active tab pane
 *   4. ContractRevisionHistory timeline, or one muted line when none is recorded
 *
 * Tabs:
 *   Code       — ContractCodeFlow: «interface» box listing ops + stereotypes + notes
 *   Component  — the committed architecture's relationships (PerspectiveFlow)
 *   Dynamic    — the use-case dynamic views this component takes part in
 *   Facets     — dataContracts + errorModel + idempotency prose + the ops table
 *
 * Honest-empty: omits tab content sections when their data is absent.
 */
import { useState, useMemo, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Chip from '@mui/material/Chip';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Table from '@mui/material/Table';
import TableBody from '@mui/material/TableBody';
import TableCell from '@mui/material/TableCell';
import TableHead from '@mui/material/TableHead';
import TableRow from '@mui/material/TableRow';
import ToggleButton from '@mui/material/ToggleButton';
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup';
import FormControl from '@mui/material/FormControl';
import Select from '@mui/material/Select';
import MenuItem from '@mui/material/MenuItem';
import CodeIcon from '@mui/icons-material/Code';
import AccountTreeIcon from '@mui/icons-material/AccountTree';
import TimelineIcon from '@mui/icons-material/Timeline';
import ArticleOutlinedIcon from '@mui/icons-material/ArticleOutlined';
import type { ArtifactModelEnvelope, ServiceContract } from '../../contracts/types';
import type { Tokens } from '../../utilities/theme/themes';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';
import { listDynamicViewsForComponent, toC4View, toDynamicView } from '../../contracts/adapters';
import { resolveContractComponentId } from '../../contracts/contractComponentId';
import { DynamicViewFlow } from '../flow/DynamicViewFlow';
import { ContractCodeFlow } from './ContractCodeFlow';
import { ComponentRelationshipsView } from './ComponentRelationshipsView';
import { ContractRevisionHistory } from './ContractRevisionHistory';
import { facetsEmptyCopy } from './serviceContractCopy.ts';

export type DiagramView = 'code' | 'component' | 'dynamic' | 'facets';

// ---------------------------------------------------------------------------
// Layer accent helper
// ---------------------------------------------------------------------------

function layerColor(t: Tokens, layer: string): string {
  switch (layer) {
    case 'Client':
      return t.chatPmFg;
    case 'Manager':
      return t.accent;
    case 'Engine':
      return t.chatArchitectFg;
    case 'ResourceAccess':
      return t.accent2;
    case 'Utility':
      return t.muted;
    default:
      return t.muted;
  }
}

// ---------------------------------------------------------------------------
// VolatilityCard
// ---------------------------------------------------------------------------

function VolatilityCard({ c, t }: { c: ServiceContract; t: Tokens }): ReactNode {
  const lc = layerColor(t, c.layer);
  // No contract on the wire records a status. A default here used to paint an
  // awaiting-coloured IN-DESIGN chip on all 29 — a claim nothing made. An absent
  // or empty status renders no chip at all (designer §2.2, §5.8).
  const status = c.status !== undefined && c.status.length > 0 ? c.status : undefined;
  return (
    <Paper sx={{ p: 0, overflow: 'hidden', borderTop: `4px solid ${lc}` }}>
      <Box sx={{ px: 2, py: 1.25, bgcolor: t.paperAlt, borderBottom: `1.5px solid ${lc}` }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
          {c.stereotype !== undefined && c.stereotype.length > 0 ? (
            <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: lc, fontWeight: 700 }}>
              {c.stereotype}
            </Typography>
          ) : null}
          <Box sx={{ flexGrow: 1 }} />
          {status !== undefined ? (
            <Chip
              data-testid={UI_IDENTIFIERS.ServiceContract.STATUS_CHIP}
              label={status}
              size="small"
              sx={{
                height: 20,
                fontSize: 9,
                fontWeight: 700,
                color: status === 'FROZEN' ? t.committedFg : t.awaitingFg,
                bgcolor: status === 'FROZEN' ? t.committedBg : t.awaitingBg,
              }}
            />
          ) : null}
        </Box>
        <Typography
          sx={{
            fontFamily: t.display,
            fontWeight: 800,
            fontSize: 20,
            color: t.ink,
            lineHeight: 1.15,
            mt: 0.25,
          }}
        >
          {c.component}
        </Typography>
      </Box>
      {c.volatility !== undefined && c.volatility.length > 0 ? (
        <Box sx={{ px: 2, py: 1.25 }}>
          <Typography
            sx={{ fontFamily: t.mono, fontSize: 9, letterSpacing: '0.08em', color: t.muted }}
          >
            ENCAPSULATED VOLATILITY
          </Typography>
          <Typography
            sx={{ fontFamily: t.body, fontSize: 12.5, color: t.ink, lineHeight: 1.5, mt: 0.25 }}
          >
            {c.volatility}
          </Typography>
        </Box>
      ) : null}
    </Paper>
  );
}

// ---------------------------------------------------------------------------
// Tab panes
// ---------------------------------------------------------------------------

function CodePane({ c, t }: { c: ServiceContract; t: Tokens }): ReactNode {
  const ops = c.ops ?? [];
  if (ops.length === 0) {
    return (
      <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.muted }}>
        No operations defined in this contract.
      </Typography>
    );
  }
  return (
    <Box>
      <Typography
        sx={{ fontFamily: t.body, fontSize: 11.5, color: t.muted, mb: 1, lineHeight: 1.45 }}
      >
        The <b>«interface»</b> surface for <b>{c.component}</b> — {ops.length} op
        {ops.length !== 1 ? 's' : ''}. Click an op row to expand its request / response structs.
      </Typography>
      <ContractCodeFlow component={c.component} height={380 + ops.length * 40} ops={ops} t={t} />
    </Box>
  );
}

/**
 * The Component tab: the architecture's own relationships (designer Q7), through
 * the design page's PerspectiveFlow. The contract's `inbound`/`outbound` fields
 * are empty for every committed contract, so they are no longer read (earmark E6).
 */
function ComponentPane({
  componentId,
  systemEnvelope,
  onFocusComponent,
  t,
}: {
  componentId: string | undefined;
  systemEnvelope: ArtifactModelEnvelope | undefined;
  onFocusComponent: ((componentId: string) => void) | undefined;
  t: Tokens;
}): ReactNode {
  if (componentId === undefined) {
    return (
      <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.muted }}>
        This contract is not placed on a component of the committed architecture, so its
        relationships cannot be drawn.
      </Typography>
    );
  }
  return (
    <ComponentRelationshipsView
      componentId={componentId}
      systemEnvelope={systemEnvelope}
      onFocusComponent={onFocusComponent}
    />
  );
}

function DynamicPane({
  focalId,
  systemEnvelope,
  t,
}: {
  focalId: string | undefined;
  systemEnvelope: ArtifactModelEnvelope | undefined;
  t: Tokens;
}): ReactNode {
  // Find all dynamic views where this component participates.
  const matchingViews = useMemo(
    () => (focalId !== undefined ? listDynamicViewsForComponent(systemEnvelope, focalId) : []),
    [systemEnvelope, focalId]
  );

  const [selectedKey, setSelectedKey] = useState<string>('');
  const activeKey = matchingViews.some((v) => v.key === selectedKey)
    ? selectedKey
    : (matchingViews[0]?.key ?? '');
  const dynamicModel = useMemo(
    () => toDynamicView(systemEnvelope, activeKey),
    [systemEnvelope, activeKey]
  );

  // Honest-empty path: no system envelope or component does not participate anywhere.
  if (systemEnvelope === undefined || focalId === undefined || matchingViews.length === 0) {
    return (
      <Paper sx={{ p: 2 }}>
        <Typography
          sx={{
            fontFamily: t.mono,
            fontWeight: 700,
            fontSize: 11,
            letterSpacing: '0.06em',
            color: t.ink,
            mb: 1,
          }}
        >
          DYNAMIC SEQUENCE VIEW
        </Typography>
        <Typography sx={{ fontFamily: t.body, fontSize: 12.5, color: t.muted, lineHeight: 1.55 }}>
          This contract&apos;s component does not participate in any of the committed use-case
          dynamic views.
        </Typography>
      </Paper>
    );
  }

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5 }}>
      {/* Use-case selector — dynamic, one entry per use case this component
          participates in, so a dropdown replaces a chip strip here too. */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        <Typography
          sx={{ fontFamily: t.mono, fontSize: 10, letterSpacing: '0.08em', color: t.muted }}
        >
          USE CASES · {matchingViews.length.toString()}
        </Typography>
        <FormControl size="small" sx={{ minWidth: 240 }}>
          <Select
            aria-label="Use case"
            sx={{ fontFamily: t.mono, fontSize: 13 }}
            value={activeKey}
            onChange={(e) => {
              setSelectedKey(e.target.value);
            }}
          >
            {matchingViews.map((v) => (
              <MenuItem key={v.key} sx={{ fontFamily: t.mono, fontSize: 13 }} value={v.key}>
                {v.title}
              </MenuItem>
            ))}
          </Select>
        </FormControl>
      </Box>
      {/* The selected dynamic-view diagram with focal highlight */}
      <DynamicViewFlow
        dv={dynamicModel}
        focalComponentId={focalId}
        height={500}
        resetKey={activeKey}
      />
    </Box>
  );
}

function FacetsPane({ c, t }: { c: ServiceContract; t: Tokens }): ReactNode {
  const ops = c.ops ?? [];
  const dataContracts = c.dataContracts ?? [];
  const hasErrorModel = c.errorModel !== undefined && c.errorModel.length > 0;
  const hasIdempotency = c.idempotency !== undefined && c.idempotency.length > 0;
  const hasDataContracts = dataContracts.length > 0;
  const hasAnyFacet = hasDataContracts || hasErrorModel || hasIdempotency;

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
      {/* Facet cards FIRST — DATA CONTRACTS, ERROR MODEL, IDEMPOTENCY */}
      {hasAnyFacet ? (
        <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', md: '1fr 1fr' }, gap: 2 }}>
          {hasDataContracts ? (
            <Paper sx={{ p: 2 }}>
              <Typography
                sx={{
                  fontFamily: t.mono,
                  fontWeight: 700,
                  fontSize: 11,
                  letterSpacing: '0.06em',
                  color: t.ink,
                  mb: 1,
                }}
              >
                DATA CONTRACTS
              </Typography>
              <Box
                component="pre"
                sx={{
                  m: 0,
                  fontFamily: t.mono,
                  fontSize: 11,
                  color: t.ink,
                  whiteSpace: 'pre-wrap',
                  lineHeight: 1.7,
                }}
              >
                {dataContracts.join('\n')}
              </Box>
            </Paper>
          ) : null}
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
            {hasErrorModel ? (
              <Paper sx={{ p: 2 }}>
                <Typography
                  sx={{
                    fontFamily: t.mono,
                    fontWeight: 700,
                    fontSize: 11,
                    letterSpacing: '0.06em',
                    color: t.ink,
                    mb: 0.5,
                  }}
                >
                  ERROR MODEL
                </Typography>
                <Typography
                  sx={{ fontFamily: t.body, fontSize: 12, color: t.ink, lineHeight: 1.5 }}
                >
                  {c.errorModel}
                </Typography>
              </Paper>
            ) : null}
            {hasIdempotency ? (
              <Paper sx={{ p: 2 }}>
                <Typography
                  sx={{
                    fontFamily: t.mono,
                    fontWeight: 700,
                    fontSize: 11,
                    letterSpacing: '0.06em',
                    color: t.ink,
                    mb: 0.5,
                  }}
                >
                  IDEMPOTENCY
                </Typography>
                <Typography
                  sx={{ fontFamily: t.body, fontSize: 12, color: t.ink, lineHeight: 1.5 }}
                >
                  {c.idempotency}
                </Typography>
              </Paper>
            ) : null}
          </Box>
        </Box>
      ) : (
        <Typography
          data-testid={UI_IDENTIFIERS.ServiceContract.FACETS_EMPTY}
          sx={{ fontFamily: t.body, fontSize: 12, color: t.muted, fontStyle: 'italic' }}
        >
          {facetsEmptyCopy(c.layer)}
        </Typography>
      )}

      {/* ops table BELOW the facets cards */}
      {ops.length > 0 ? (
        <Paper sx={{ p: 0, overflow: 'hidden' }}>
          <Box sx={{ px: 2, py: 1.1, bgcolor: t.paperAlt, borderBottom: `1.5px solid ${t.line}` }}>
            <Typography
              sx={{
                fontFamily: t.mono,
                fontWeight: 700,
                fontSize: 11,
                letterSpacing: '0.06em',
                color: t.ink,
              }}
            >
              OPERATIONS · {ops.length} · App-B §5.2 sweet spot 3–5
            </Typography>
          </Box>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell
                  sx={{ fontFamily: t.mono, fontSize: 10, fontWeight: 700, color: t.muted }}
                >
                  SIGNATURE
                </TableCell>
                <TableCell
                  sx={{ fontFamily: t.mono, fontSize: 10, fontWeight: 700, color: t.muted }}
                >
                  STEREOTYPE
                </TableCell>
                <TableCell
                  sx={{ fontFamily: t.mono, fontSize: 10, fontWeight: 700, color: t.muted }}
                >
                  NOTE
                </TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {ops.map((op, i) => (
                <TableRow key={`${op.signature}-${String(i)}`}>
                  <TableCell
                    sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.ink, verticalAlign: 'top' }}
                  >
                    {op.signature}
                  </TableCell>
                  <TableCell
                    sx={{
                      fontFamily: t.mono,
                      fontSize: 10,
                      color: t.muted,
                      verticalAlign: 'top',
                      whiteSpace: 'nowrap',
                    }}
                  >
                    {op.stereotype}
                  </TableCell>
                  <TableCell
                    sx={{ fontFamily: t.body, fontSize: 11, color: t.ink, verticalAlign: 'top' }}
                  >
                    {op.note ?? '—'}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Paper>
      ) : null}
    </Box>
  );
}

// ---------------------------------------------------------------------------
// ServiceContractView (public export)
// ---------------------------------------------------------------------------

export function ServiceContractView({
  contract,
  systemEnvelope,
  componentId,
  view: controlledView,
  onViewChange,
  onFocusComponent,
}: {
  contract: ServiceContract;
  systemEnvelope?: ArtifactModelEnvelope | undefined;
  /**
   * The slot-5 component id the contract JOIN placed it on (contracts/
   * serviceContracts.ts). Preferred over a name-based resolution; absent, the
   * Dynamic tab falls back to resolveContractComponentId.
   */
  componentId?: string | undefined;
  /** Controlled tab (the pane holds it in the URL, `av`); uncontrolled when absent. */
  view?: DiagramView | undefined;
  onViewChange?: ((view: DiagramView) => void) | undefined;
  /** The Component tab's neighbour click — moves the selection onto its activity. */
  onFocusComponent?: ((componentId: string) => void) | undefined;
}): ReactNode {
  const t = useTokens();
  const c = contract;
  const ops = c.ops ?? [];
  const revisions = c.revisions ?? [];

  const [localView, setLocalView] = useState<DiagramView>('code');
  const view = controlledView ?? localView;
  const setView = (v: DiagramView): void => {
    setLocalView(v);
    onViewChange?.(v);
  };

  const c4 = useMemo(() => toC4View(systemEnvelope), [systemEnvelope]);
  const focalId = useMemo(
    () => componentId ?? resolveContractComponentId(c.component, c4.components),
    [componentId, c.component, c4.components]
  );

  return (
    <Box
      data-testid={UI_IDENTIFIERS.ServiceContract.ROOT}
      data-view={view}
      sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}
    >
      <VolatilityCard c={c} t={t} />

      {/* 4-tab selector */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
        <ToggleButtonGroup
          exclusive
          size="small"
          sx={{
            flexWrap: 'wrap',
            '& .MuiToggleButton-root': {
              fontFamily: t.mono,
              fontSize: 11,
              fontWeight: 700,
              letterSpacing: '0.02em',
              textTransform: 'none',
              color: t.ink,
              borderColor: t.line,
              px: 1.25,
              py: 0.4,
            },
            '& .Mui-selected': {
              bgcolor: `${t.accent} !important`,
              color: `${t.accentText} !important`,
            },
          }}
          value={view}
          onChange={(_e, v: DiagramView | null) => {
            if (v !== null) setView(v);
          }}
        >
          <ToggleButton data-testid={UI_IDENTIFIERS.ServiceContract.TAB_CODE} value="code">
            <CodeIcon sx={{ fontSize: 15, mr: 0.6 }} /> Code
          </ToggleButton>
          <ToggleButton
            data-testid={UI_IDENTIFIERS.ServiceContract.TAB_COMPONENT}
            value="component"
          >
            <AccountTreeIcon sx={{ fontSize: 15, mr: 0.6 }} /> Component
          </ToggleButton>
          <ToggleButton data-testid={UI_IDENTIFIERS.ServiceContract.TAB_DYNAMIC} value="dynamic">
            <TimelineIcon sx={{ fontSize: 15, mr: 0.6 }} /> Dynamic
          </ToggleButton>
          <ToggleButton data-testid={UI_IDENTIFIERS.ServiceContract.TAB_FACETS} value="facets">
            <ArticleOutlinedIcon sx={{ fontSize: 15, mr: 0.6 }} /> Facets
          </ToggleButton>
        </ToggleButtonGroup>
        <Box sx={{ flexGrow: 1 }} />
        <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.muted }}>
          {view === 'component'
            ? 'C4 · focused component view'
            : view === 'code'
              ? 'C4 · code level (interface)'
              : view === 'dynamic'
                ? 'C4 · dynamic sequence'
                : `${ops.length.toString()} ops · App-B §5.2 sweet spot 3–5`}
        </Typography>
      </Box>

      {/* active pane */}
      {view === 'code' && <CodePane c={c} t={t} />}
      {view === 'component' && (
        <ComponentPane
          componentId={focalId}
          systemEnvelope={systemEnvelope}
          t={t}
          onFocusComponent={onFocusComponent}
        />
      )}
      {view === 'dynamic' && (
        <DynamicPane focalId={focalId} systemEnvelope={systemEnvelope} t={t} />
      )}
      {view === 'facets' && <FacetsPane c={c} t={t} />}

      {/* revision history — with none recorded, one muted line, never an empty timeline */}
      {revisions.length > 0 ? (
        <ContractRevisionHistory revisions={revisions} t={t} />
      ) : (
        <Typography
          data-testid={UI_IDENTIFIERS.ServiceContract.REVISION_HISTORY}
          sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}
        >
          No revision history recorded.
        </Typography>
      )}
    </Box>
  );
}
