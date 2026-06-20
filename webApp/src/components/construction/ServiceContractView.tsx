/**
 * ServiceContractView — the 4-tab C4 navigator for a SERVICE activity's contract.
 *
 * Props: { contract: ServiceContract } — fed by real data from
 * contractForActivity() / contractForComponent().
 *
 * Structure:
 *   1. VolatilityCard  — stereotype banner + component name + FROZEN/IN-DESIGN chip + volatility text
 *   2. 4-tab ToggleButtonGroup  — Code / interface | Component view | Dynamic | Contract facets
 *   3. Active tab pane
 *   4. ContractRevisionHistory timeline
 *
 * Tabs:
 *   Code / interface   — ContractCodeFlow: «interface» box listing ops + stereotypes + notes
 *   Component view     — ContractComponentFlow: inbound above, focal centered, outbound below
 *   Dynamic            — honest note (per-op call-sequence data not in the contract)
 *   Contract facets    — ops table + dataContracts + errorModel + idempotency prose
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
import CodeIcon from '@mui/icons-material/Code';
import AccountTreeIcon from '@mui/icons-material/AccountTree';
import TimelineIcon from '@mui/icons-material/Timeline';
import ArticleOutlinedIcon from '@mui/icons-material/ArticleOutlined';
import type { ArtifactModelEnvelope, ServiceContract } from '../../api/types';
import type { Tokens } from '../../theme/themes';
import { useTokens } from '../../theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';
import { listDynamicViewsForComponent, toC4View } from '../../api/adapters';
import { resolveContractComponentId } from '../../api/contractComponentId';
import { DynamicViewFlow } from '../flow/DynamicViewFlow';
import { ContractCodeFlow } from './ContractCodeFlow';
import { ContractComponentFlow } from './ContractComponentFlow';
import { ContractRevisionHistory } from './ContractRevisionHistory';

type DiagramView = 'code' | 'component' | 'dynamic' | 'facets';

// ---------------------------------------------------------------------------
// Layer accent helper
// ---------------------------------------------------------------------------

function layerColor(t: Tokens, layer: string): string {
  switch (layer) {
    case 'Client': return t.chatPmFg;
    case 'Manager': return t.accent;
    case 'Engine': return t.chatArchitectFg;
    case 'ResourceAccess': return t.accent2;
    case 'Utility': return t.muted;
    default: return t.muted;
  }
}

// ---------------------------------------------------------------------------
// VolatilityCard
// ---------------------------------------------------------------------------

function VolatilityCard({ c, t }: { c: ServiceContract; t: Tokens }): ReactNode {
  const lc = layerColor(t, c.layer);
  const status = c.status ?? 'IN-DESIGN';
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
          <Chip
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
        </Box>
        <Typography
          sx={{ fontFamily: t.display, fontWeight: 800, fontSize: 20, color: t.ink, lineHeight: 1.15, mt: 0.25 }}
        >
          {c.component}
        </Typography>
      </Box>
      {c.volatility !== undefined && c.volatility.length > 0 ? (
        <Box sx={{ px: 2, py: 1.25 }}>
          <Typography sx={{ fontFamily: t.mono, fontSize: 9, letterSpacing: '0.08em', color: t.muted }}>
            ENCAPSULATED VOLATILITY
          </Typography>
          <Typography sx={{ fontFamily: t.body, fontSize: 12.5, color: t.ink, lineHeight: 1.5, mt: 0.25 }}>
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
      <Typography sx={{ fontFamily: t.body, fontSize: 11.5, color: t.muted, mb: 1, lineHeight: 1.45 }}>
        The <b>«interface»</b> surface for <b>{c.component}</b> — {ops.length} op{ops.length !== 1 ? 's' : ''}.
        Click an op row to expand its request / response structs.
      </Typography>
      <ContractCodeFlow component={c.component} height={380 + ops.length * 40} ops={ops} t={t} />
    </Box>
  );
}

function ComponentPane({ c, t }: { c: ServiceContract; t: Tokens }): ReactNode {
  const inbound = c.inbound ?? [];
  const outbound = c.outbound ?? [];
  if (inbound.length === 0 && outbound.length === 0) {
    return (
      <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.muted }}>
        No inbound or outbound relationships defined in this contract.
      </Typography>
    );
  }
  return (
    <Box>
      <Typography sx={{ fontFamily: t.body, fontSize: 11.5, color: t.muted, mb: 1, lineHeight: 1.45 }}>
        Focal component centered; <b>inbound callers</b> connect from above, <b>outbound callees</b> connect below.
        Built from the contract&apos;s own inbound/outbound fields — does not cross-reference the system-design slot.
      </Typography>
      <ContractComponentFlow
        component={c.component}
        data-testid={UI_IDENTIFIERS.ServiceContract.COMPONENT_FLOW}
        height={Math.max(300, 120 * (Math.max(inbound.length, outbound.length) + 2))}
        inbound={inbound}
        layer={c.layer}
        outbound={outbound}
        t={t}
      />
    </Box>
  );
}

function DynamicPane({
  contract,
  systemEnvelope,
  t,
}: {
  contract: ServiceContract;
  systemEnvelope: ArtifactModelEnvelope | undefined;
  t: Tokens;
}): ReactNode {
  // Resolve the component id (kebab-case) from the contract's camelCase component name.
  const c4 = useMemo(() => toC4View(systemEnvelope), [systemEnvelope]);
  const focalId = useMemo(
    () => resolveContractComponentId(contract.component, c4.components),
    [contract.component, c4.components]
  );

  // Find all dynamic views where this component participates.
  const matchingViews = useMemo(
    () => (focalId !== undefined ? listDynamicViewsForComponent(systemEnvelope, focalId) : []),
    [systemEnvelope, focalId]
  );

  const [selectedKey, setSelectedKey] = useState<string>('');
  const activeKey = matchingViews.some((v) => v.key === selectedKey)
    ? selectedKey
    : (matchingViews[0]?.key ?? '');

  // Honest-empty path: no system envelope or component does not participate anywhere.
  if (systemEnvelope === undefined || focalId === undefined || matchingViews.length === 0) {
    return (
      <Paper sx={{ p: 2 }}>
        <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11, letterSpacing: '0.06em', color: t.ink, mb: 1 }}>
          DYNAMIC SEQUENCE VIEW
        </Typography>
        <Typography sx={{ fontFamily: t.body, fontSize: 12.5, color: t.muted, lineHeight: 1.55 }}>
          This contract&apos;s component does not participate in any of the committed use-case dynamic views.
        </Typography>
      </Paper>
    );
  }

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5 }}>
      {/* Use-case selector chips */}
      <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.75, alignItems: 'center' }}>
        <Typography sx={{ fontFamily: t.mono, fontSize: 10, letterSpacing: '0.08em', color: t.muted, mr: 0.5 }}>
          USE CASES · {matchingViews.length.toString()}
        </Typography>
        {matchingViews.map((v) => (
          <Chip
            key={v.key}
            label={v.title}
            size="small"
            sx={{
              fontFamily: t.mono,
              fontSize: 10,
              fontWeight: 700,
              cursor: 'pointer',
              bgcolor: v.key === activeKey ? t.accent : t.paperAlt,
              color: v.key === activeKey ? t.accentText : t.ink,
              border: `1.5px solid ${v.key === activeKey ? t.accent : t.line}`,
            }}
            onClick={() => { setSelectedKey(v.key); }}
          />
        ))}
      </Box>
      {/* The selected dynamic-view diagram with focal highlight */}
      <DynamicViewFlow
        envelope={systemEnvelope}
        focalComponentId={focalId}
        height={500}
        viewKey={activeKey}
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
              <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11, letterSpacing: '0.06em', color: t.ink, mb: 1 }}>
                DATA CONTRACTS
              </Typography>
              <Box component="pre" sx={{ m: 0, fontFamily: t.mono, fontSize: 11, color: t.ink, whiteSpace: 'pre-wrap', lineHeight: 1.7 }}>
                {dataContracts.join('\n')}
              </Box>
            </Paper>
          ) : null}
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
            {hasErrorModel ? (
              <Paper sx={{ p: 2 }}>
                <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11, letterSpacing: '0.06em', color: t.ink, mb: 0.5 }}>
                  ERROR MODEL
                </Typography>
                <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.ink, lineHeight: 1.5 }}>
                  {c.errorModel}
                </Typography>
              </Paper>
            ) : null}
            {hasIdempotency ? (
              <Paper sx={{ p: 2 }}>
                <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11, letterSpacing: '0.06em', color: t.ink, mb: 0.5 }}>
                  IDEMPOTENCY
                </Typography>
                <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.ink, lineHeight: 1.5 }}>
                  {c.idempotency}
                </Typography>
              </Paper>
            ) : null}
          </Box>
        </Box>
      ) : (
        <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.muted, fontStyle: 'italic' }}>
          This contract declares no data/error/idempotency facets (Client layer).
        </Typography>
      )}

      {/* ops table BELOW the facets cards */}
      {ops.length > 0 ? (
        <Paper sx={{ p: 0, overflow: 'hidden' }}>
          <Box sx={{ px: 2, py: 1.1, bgcolor: t.paperAlt, borderBottom: `1.5px solid ${t.line}` }}>
            <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11, letterSpacing: '0.06em', color: t.ink }}>
              OPERATIONS · {ops.length} · App-B §5.2 sweet spot 3–5
            </Typography>
          </Box>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell sx={{ fontFamily: t.mono, fontSize: 10, fontWeight: 700, color: t.muted }}>SIGNATURE</TableCell>
                <TableCell sx={{ fontFamily: t.mono, fontSize: 10, fontWeight: 700, color: t.muted }}>STEREOTYPE</TableCell>
                <TableCell sx={{ fontFamily: t.mono, fontSize: 10, fontWeight: 700, color: t.muted }}>NOTE</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {ops.map((op, i) => (
                <TableRow key={`${op.signature}-${String(i)}`}>
                  <TableCell sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.ink, verticalAlign: 'top' }}>
                    {op.signature}
                  </TableCell>
                  <TableCell sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted, verticalAlign: 'top', whiteSpace: 'nowrap' }}>
                    {op.stereotype}
                  </TableCell>
                  <TableCell sx={{ fontFamily: t.body, fontSize: 11, color: t.ink, verticalAlign: 'top' }}>
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
}: {
  contract: ServiceContract;
  systemEnvelope?: ArtifactModelEnvelope | undefined;
}): ReactNode {
  const t = useTokens();
  const c = contract;
  const ops = c.ops ?? [];
  const revisions = c.revisions ?? [];

  const [view, setView] = useState<DiagramView>('code');

  return (
    <Box
      data-testid={UI_IDENTIFIERS.ServiceContract.ROOT}
      sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}
    >
      <VolatilityCard c={c} t={t} />

      {/* 4-tab selector */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
        <ToggleButtonGroup
          exclusive
          size="small"
          sx={{
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
            '& .Mui-selected': { bgcolor: `${t.accent} !important`, color: `${t.accentText} !important` },
          }}
          value={view}
          onChange={(_e, v: DiagramView | null) => { if (v !== null) setView(v); }}
        >
          <ToggleButton data-testid={UI_IDENTIFIERS.ServiceContract.TAB_CODE} value="code">
            <CodeIcon sx={{ fontSize: 15, mr: 0.6 }} /> Code / interface
          </ToggleButton>
          <ToggleButton data-testid={UI_IDENTIFIERS.ServiceContract.TAB_COMPONENT} value="component">
            <AccountTreeIcon sx={{ fontSize: 15, mr: 0.6 }} /> Component view
          </ToggleButton>
          <ToggleButton data-testid={UI_IDENTIFIERS.ServiceContract.TAB_DYNAMIC} value="dynamic">
            <TimelineIcon sx={{ fontSize: 15, mr: 0.6 }} /> Dynamic
          </ToggleButton>
          <ToggleButton data-testid={UI_IDENTIFIERS.ServiceContract.TAB_FACETS} value="facets">
            <ArticleOutlinedIcon sx={{ fontSize: 15, mr: 0.6 }} /> Contract facets
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
      {view === 'component' && <ComponentPane c={c} t={t} />}
      {view === 'dynamic' && <DynamicPane contract={c} systemEnvelope={systemEnvelope} t={t} />}
      {view === 'facets' && <FacetsPane c={c} t={t} />}

      {/* revision history */}
      <ContractRevisionHistory revisions={revisions} t={t} />
    </Box>
  );
}
