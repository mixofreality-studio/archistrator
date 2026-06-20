/**
 * InterventionDrawer — the right-anchored per-activity intervention experience.
 *
 * Opened when the operator clicks "Review contract / change" (or equivalent) on a
 * QueueCard in InterventionQueue. Renders:
 *
 *   1. Header: "INTERVENTION · {activityId}" + KindBadge + activity name + close button.
 *   2. "⚑ WHAT YOU ARE BEING ASKED TO APPROVE" banner — derives the ask from the
 *      activity's id, kind, and phase.
 *   3. Contract body: ServiceContractView (read-only import) when a contract is
 *      resolvable via contractForActivity; an honest-empty note otherwise.
 *   4. OperatorBar (footer) — the steer buttons (Approve +1 / Send back / Take over /
 *      Reassign / Skip / Pause / Replay). All INERT: disabled with a Tooltip noting
 *      "Operator steer binds once the live construction pump (R-CPR) is provisioned
 *      — currently read-only." No mutation is wired.
 *
 * Why inert: the live construction pump (R-CPR) is not provisioned. Steer commands
 * (constructionManager.overrideActivity / pauseProject) have no active session to
 * signal. The buttons are present per the design spec but disabled so the operator
 * understands the full interaction model before the pump ships.
 *
 * The existing project-level Pause control (InterventionsTab → POST /construction/pause)
 * is SEPARATE and remains wired — only the per-activity steer overrides here are inert.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Drawer from '@mui/material/Drawer';
import IconButton from '@mui/material/IconButton';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import Tooltip from '@mui/material/Tooltip';
import CloseIcon from '@mui/icons-material/Close';
import CheckIcon from '@mui/icons-material/Check';
import ReplayIcon from '@mui/icons-material/Replay';
import OpenWithIcon from '@mui/icons-material/OpenWith';
import SwapHorizIcon from '@mui/icons-material/SwapHoriz';
import SkipNextIcon from '@mui/icons-material/SkipNext';
import PauseCircleOutlineIcon from '@mui/icons-material/PauseCircleOutline';
import HistoryIcon from '@mui/icons-material/History';
import type { ConstructionRow, GitRow, ProjectStateWithGit } from '../../api/types';
import { contractForActivity } from '../../api/serviceContracts';
import { useTokens } from '../../theme/ThemeContext';
import type { Tokens } from '../../theme/themes';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';
import { KindBadge, KIND_META } from './KindBadge';
import { ServiceContractView } from './ServiceContractView';
import { GitRowMeta } from '../GitStatus';

// ---------------------------------------------------------------------------
// Steer affordances — the OperatorBar buttons (INERT until R-CPR lands).
// ---------------------------------------------------------------------------

const INERT_TOOLTIP =
  'Operator steer binds once the live construction pump (R-CPR) is provisioned — currently read-only.';

interface Affordance {
  kind: string;
  label: string;
  detail: string;
  icon: ReactNode;
}

const AFFORDANCES: Affordance[] = [
  {
    kind: 'Approve',
    label: 'Approve · +1',
    detail: 'Approve this gate — the life cycle proceeds.',
    icon: <CheckIcon sx={{ fontSize: 15 }} />,
  },
  {
    kind: 'SendBack',
    label: 'Send back',
    detail: 'Return with feedback — the worker redrafts.',
    icon: <ReplayIcon sx={{ fontSize: 15 }} />,
  },
  {
    kind: 'Takeover',
    label: 'Take over',
    detail: 'Re-dispatch under a changed arrangement / reset the durable execution.',
    icon: <OpenWithIcon sx={{ fontSize: 15 }} />,
  },
  {
    kind: 'Reassign',
    label: 'Reassign',
    detail: 'Re-cast the worker class (e.g. AI worker → senior-developer).',
    icon: <SwapHorizIcon sx={{ fontSize: 15 }} />,
  },
  {
    kind: 'Skip',
    label: 'Skip',
    detail: 'Record the activity exited with an operator-skip outcome.',
    icon: <SkipNextIcon sx={{ fontSize: 15 }} />,
  },
  {
    kind: 'Pause',
    label: 'Pause',
    detail: 'Signal operatorPauseRequested — cancel in-flight pipelines.',
    icon: <PauseCircleOutlineIcon sx={{ fontSize: 15 }} />,
  },
  {
    kind: 'Replay',
    label: 'Replay',
    detail: 'Replay the durable Temporal history to inspect how it reached this state (UC6).',
    icon: <HistoryIcon sx={{ fontSize: 15 }} />,
  },
];

// ---------------------------------------------------------------------------
// OperatorBar — the steer control strip (all INERT; see module-level doc).
// ---------------------------------------------------------------------------

function OperatorBar({ t }: { t: Tokens }): ReactNode {
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.INTERVENTION_OPERATOR_BAR}
      sx={{
        flexShrink: 0,
        px: 2.5,
        py: 1.5,
        borderTop: `1.5px solid ${t.line}`,
        bgcolor: t.paper,
      }}
    >
      <Typography
        sx={{
          fontFamily: t.mono,
          fontSize: 9.5,
          letterSpacing: '0.06em',
          color: t.muted,
          mb: 0.75,
        }}
      >
        OPERATOR STEER · constructionManager.overrideActivity · reviewEngine gate
      </Typography>

      {/* inert note */}
      <Box
        sx={{
          mb: 1,
          px: 1.25,
          py: 0.6,
          bgcolor: t.paperAlt,
          border: `1px solid ${t.line}`,
          borderRadius: 1,
        }}
      >
        <Typography
          sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.muted, lineHeight: 1.4 }}
        >
          Acts once the live construction pump (R-CPR) is provisioned — currently read-only.
        </Typography>
      </Box>

      <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.75, alignItems: 'center' }}>
        {AFFORDANCES.map((af) => (
          <Tooltip key={af.kind} title={`${af.detail} ${INERT_TOOLTIP}`}>
            {/* span needed for Tooltip on disabled button */}
            <span>
              <Button
                disabled
                data-testid={UI_IDENTIFIERS.Construction.interventionSteerButton(af.kind)}
                size="small"
                startIcon={af.icon}
                sx={{ py: 0.25, fontSize: 11, borderColor: t.line }}
                variant="outlined"
              >
                {af.label}
              </Button>
            </span>
          </Tooltip>
        ))}
      </Box>
    </Box>
  );
}

// ---------------------------------------------------------------------------
// DrawerBody — the scrollable middle content area.
// ---------------------------------------------------------------------------

function askString(activityId: string, row: ConstructionRow): string {
  const kindLabel = KIND_META[row.kind].label;
  return `${activityId} reached CODE REVIEW — the computed reviewer set needs your gate. Review the ${kindLabel.toLowerCase()} contract / produced change and approve or send back.`;
}

function DrawerBody({
  activityId,
  name,
  row,
  git,
  project,
  t,
}: {
  activityId: string;
  name: string;
  row: ConstructionRow;
  git: GitRow | undefined;
  project: ProjectStateWithGit | undefined;
  t: Tokens;
}): ReactNode {
  const contract = contractForActivity(project, activityId);

  return (
    <Box sx={{ flexGrow: 1, overflowY: 'auto', px: 2.5, py: 2 }}>
      {/* git row (if available) */}
      {git !== undefined && (
        <Box sx={{ mb: 1.5 }}>
          <GitRowMeta git={git} />
        </Box>
      )}

      {/* ask banner */}
      <Box
        sx={{
          mb: 2,
          p: 1.5,
          bgcolor: t.awaitingBg,
          border: `1.5px solid ${t.line}`,
          borderRadius: 1,
        }}
      >
        <Typography
          sx={{
            fontFamily: t.mono,
            fontSize: 10,
            letterSpacing: '0.06em',
            color: t.awaitingFg,
            mb: 0.3,
          }}
        >
          ⚑ WHAT YOU ARE BEING ASKED TO APPROVE
        </Typography>
        <Typography
          sx={{ fontFamily: t.body, fontSize: 13, color: t.awaitingFg, lineHeight: 1.5 }}
        >
          {askString(activityId, row)}
        </Typography>
      </Box>

      {/* contract body */}
      {row.kind === 'service' ? (
        contract !== undefined ? (
          <Box>
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
              {row.phase === 'svc-contract'
                ? 'REVIEW THE SERVICE CONTRACT BEFORE FREEZE'
                : 'REVIEW THE PRODUCED CHANGE AGAINST THE FROZEN CONTRACT'}
            </Typography>
            <ServiceContractView contract={contract} />
          </Box>
        ) : (
          <Box
            sx={{
              p: 2,
              bgcolor: t.paperAlt,
              border: `1px solid ${t.line}`,
              borderRadius: 1,
            }}
          >
            <Typography sx={{ fontFamily: t.mono, fontSize: 11.5, color: t.muted }}>
              No service contract resolved for {name} ({activityId}). The contract will appear
              here once the activity produces a service-contract artifact.
            </Typography>
          </Box>
        )
      ) : (
        <Box
          sx={{
            p: 2,
            bgcolor: t.paperAlt,
            border: `1px solid ${t.line}`,
            borderRadius: 1,
          }}
        >
          <Typography sx={{ fontFamily: t.mono, fontSize: 11.5, color: t.muted }}>
            {row.kind === 'frontend'
              ? 'Frontend design-loop review — the design loop experience renders here once the live pump is provisioned (R-CPR).'
              : 'Testing artifact review — the test-plan view renders here once the live pump is provisioned (R-CPR).'}
          </Typography>
        </Box>
      )}
    </Box>
  );
}

// ---------------------------------------------------------------------------
// InterventionDrawer (public export)
// ---------------------------------------------------------------------------

export function InterventionDrawer({
  activityId,
  row,
  name,
  git,
  project,
  onClose,
}: {
  activityId: string | null;
  row: ConstructionRow | undefined;
  name: string;
  git: GitRow | undefined;
  project: ProjectStateWithGit | undefined;
  onClose: () => void;
}): ReactNode {
  const t = useTokens();
  const open = activityId !== null && row !== undefined;

  return (
    <Drawer
      anchor="right"
      data-testid={UI_IDENTIFIERS.Construction.INTERVENTION_DRAWER}
      open={open}
      slotProps={{
        paper: {
          sx: { width: { xs: '100%', md: 720 }, bgcolor: t.bg, backgroundImage: 'none' },
        },
      }}
      onClose={onClose}
    >
      {activityId !== null && row !== undefined ? (
        <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
          {/* drawer header */}
          <Box
            sx={{
              flexShrink: 0,
              px: 2.5,
              py: 1.75,
              borderBottom: `1.5px solid ${t.line}`,
              borderTop: `4px solid ${t.accent}`,
              bgcolor: t.paper,
              display: 'flex',
              alignItems: 'center',
              gap: 1.5,
            }}
          >
            <Box sx={{ minWidth: 0, flexGrow: 1 }}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, flexWrap: 'wrap' }}>
                <Typography
                  sx={{
                    fontFamily: t.mono,
                    fontSize: 10,
                    letterSpacing: '0.16em',
                    color: t.accent,
                  }}
                >
                  INTERVENTION · {activityId}
                </Typography>
                <KindBadge kind={row.kind} size="xs" t={t} />
              </Box>
              <Typography
                sx={{
                  fontFamily: t.display,
                  fontWeight: 800,
                  fontSize: 20,
                  color: t.ink,
                  lineHeight: 1.15,
                }}
              >
                {name}
              </Typography>
            </Box>
            <IconButton
              aria-label="close intervention drawer"
              data-testid={UI_IDENTIFIERS.Construction.INTERVENTION_DRAWER_CLOSE}
              size="small"
              sx={{ color: t.ink, flexShrink: 0 }}
              onClick={onClose}
            >
              <CloseIcon fontSize="small" />
            </IconButton>
          </Box>

          {/* scrollable body */}
          <DrawerBody
            activityId={activityId}
            git={git}
            name={name}
            project={project}
            row={row}
            t={t}
          />

          {/* operator steer bar — inert until pump */}
          <OperatorBar t={t} />
        </Box>
      ) : null}
    </Drawer>
  );
}
