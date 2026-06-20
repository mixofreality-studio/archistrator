/**
 * The shared GIT-FORWARD row chrome (U-SPA-GIT) — one place so it recolors across
 * all themes and reads identically on every surface that carries git-backed
 * activity rows (today: the construction tracker's active-activity detail).
 *
 * Ported faithfully from the ux-mock `components/GitStatus.tsx`. Three composable
 * pieces:
 *   <CiStatusIcon>  — a DUMB reflection of the GitHub-Actions run (3 states).
 *   <PrLink>        — the clickable PR link, opens GitHub in a new tab.
 *   <GitRowMeta>    — the compact inline cluster (PR · branch · CI [· +1]) for a row.
 *   <ArchApprovedTag> — the "+1 posted" tag, shown when the human relays the +1.
 *
 * Deliberately DISTINCT from construction/status.tsx (that palette is the Method
 * LIFECYCLE state). CI/PR status is its own vocabulary; a row can show both. CI
 * status NEVER gates any Approve control — it only displays.
 *
 * Binds to the REAL server `gitRow` head-state (C-CW-GIT): `prNumber`/`prUrl` are
 * SERVER-side read-time projections — read directly, NEVER constructed here.
 * Honest-empty is the CALLER's job: render nothing when `gitFor(...)` is
 * undefined; this component assumes a present row.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Link from '@mui/material/Link';
import Tooltip from '@mui/material/Tooltip';
import HourglassTopIcon from '@mui/icons-material/HourglassTop';
import ErrorIcon from '@mui/icons-material/Error';
import CheckCircleIcon from '@mui/icons-material/CheckCircle';
import CallMergeIcon from '@mui/icons-material/CallMerge';
import UndoIcon from '@mui/icons-material/Undo';
import VerifiedIcon from '@mui/icons-material/Verified';
import { useTokens } from '../theme/ThemeContext';
import type { Tokens } from '../theme/themes';
import type { CiStatus, GitRow } from '../api/types';
import { UI_IDENTIFIERS } from '../constants/UIIdentifiers';

const CI_STATUS_VERB: Record<CiStatus, string> = {
  in_progress: 'GitHub Actions in progress',
  failed: 'GitHub Actions run failed',
  success: 'GitHub Actions run succeeded',
};

function ciColor(t: Tokens, s: CiStatus): string {
  switch (s) {
    case 'success':
      return t.committedDot;
    case 'failed':
      return t.dangerFg;
    case 'in_progress':
      return t.accent2;
  }
}

/** The build-status icon — in-progress (hourglass, pulsing) / failed / success. */
export function CiStatusIcon({ status, size = 17 }: { status: CiStatus; size?: number }): ReactNode {
  const t = useTokens();
  const color = ciColor(t, status);
  const verb = CI_STATUS_VERB[status];
  const Icon = status === 'success' ? CheckCircleIcon : status === 'failed' ? ErrorIcon : HourglassTopIcon;
  return (
    <Tooltip title={`PR build · ${verb}`}>
      <Box
        aria-label={verb}
        component="span"
        data-testid={UI_IDENTIFIERS.Git.ciStatus(status)}
        role="img"
        sx={{
          display: 'inline-flex',
          alignItems: 'center',
          color,
          '& svg': {
            fontSize: size,
            ...(status === 'in_progress' && {
              animation: 'ciPulse 1.4s ease-in-out infinite',
              '@keyframes ciPulse': { '0%,100%': { opacity: 0.45 }, '50%': { opacity: 1 } },
            }),
          },
        }}
      >
        <Icon />
      </Box>
    </Tooltip>
  );
}

/**
 * The clickable PR link — opens the GitHub PR in a new tab. `prUrl`/`prNumber`
 * are server-projected; rendered only when both are present (honest — never a
 * broken href). Returns null when the PR has not opened yet (branch-only).
 */
export function PrLink({ git, dense = false }: { git: GitRow; dense?: boolean }): ReactNode {
  const t = useTokens();
  if (git.prUrl === undefined || git.prNumber === undefined) return null;
  return (
    <Tooltip title={`Open PR #${String(git.prNumber)} · ${git.branchName}${git.isRevert === true ? ' · revert' : ''}`}>
      <Link
        data-testid={UI_IDENTIFIERS.Git.PR_LINK}
        href={git.prUrl}
        rel="noopener noreferrer"
        sx={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: 0.3,
          fontFamily: t.mono,
          fontSize: dense ? 9.5 : 11,
          fontWeight: 700,
          color: t.accent2,
          whiteSpace: 'nowrap',
        }}
        target="_blank"
        underline="hover"
        onClick={(e) => { e.stopPropagation(); }}
      >
        {git.isRevert === true ? <UndoIcon sx={{ fontSize: dense ? 11 : 13 }} /> : <CallMergeIcon sx={{ fontSize: dense ? 11 : 13 }} />}
        PR #{String(git.prNumber)}
      </Link>
    </Tooltip>
  );
}

/** The "architecture +1 posted" tag — shown once the human relays the +1 to the PR. */
export function ArchApprovedTag({ dense = false }: { dense?: boolean }): ReactNode {
  const t = useTokens();
  return (
    <Tooltip title="Architecture approved — a +1 review was posted to the PR">
      <Box
        aria-label="architecture approved, +1 posted to PR"
        component="span"
        data-testid={UI_IDENTIFIERS.Git.ARCH_APPROVED}
        sx={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: 0.3,
          px: 0.6,
          py: 0.1,
          borderRadius: 99,
          bgcolor: t.committedBg,
          color: t.committedFg,
          border: `1px solid ${t.committedDot}`,
          fontFamily: t.mono,
          fontSize: dense ? 8.5 : 9.5,
          fontWeight: 700,
          whiteSpace: 'nowrap',
        }}
      >
        <VerifiedIcon sx={{ fontSize: dense ? 11 : 12 }} />
        +1 posted
      </Box>
    </Tooltip>
  );
}

/**
 * The compact inline git cluster for a single git-backed row:
 *   PR #42 · [merged] · branch · [cr label] · [CI icon] · [+1 posted]
 * Drop it into any git-backed row's metadata line. When `merged` is true a
 * "merged" affordance is shown alongside the PR link (the terminal git fact —
 * additive to the ux-mock, which carries no merged state).
 */
export function GitRowMeta({
  git,
  dense = false,
  showBranch = true,
}: {
  git: GitRow;
  dense?: boolean;
  showBranch?: boolean;
}): ReactNode {
  const t = useTokens();
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Git.ROW_META}
      sx={{ display: 'inline-flex', alignItems: 'center', gap: dense ? 0.6 : 0.9, flexWrap: 'wrap' }}
    >
      <PrLink dense={dense} git={git} />
      {git.merged ? (
        <Tooltip title="Merged to main">
          <Box
            aria-label="merged to main"
            component="span"
            data-testid={UI_IDENTIFIERS.Git.MERGED}
            sx={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: 0.2,
              fontFamily: t.mono,
              fontSize: dense ? 8.5 : 9.5,
              fontWeight: 700,
              color: t.committedFg,
            }}
          >
            <CallMergeIcon sx={{ fontSize: dense ? 11 : 12 }} />
            merged
          </Box>
        </Tooltip>
      ) : null}
      {showBranch ? (
        <Tooltip title={`branch · ${git.branchName}`}>
          <Box
            component="span"
            data-testid={UI_IDENTIFIERS.Git.BRANCH}
            sx={{
              fontFamily: t.mono,
              fontSize: dense ? 8.5 : 10,
              color: t.muted,
              maxWidth: dense ? 120 : 200,
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            }}
          >
            {git.branchName}
          </Box>
        </Tooltip>
      ) : null}
      {git.crLabel !== undefined && git.crLabel.length > 0 && (
        <Box
          component="span"
          data-testid={UI_IDENTIFIERS.Git.CR_LABEL}
          sx={{
            fontFamily: t.mono,
            fontSize: dense ? 8 : 9,
            fontWeight: 700,
            color: t.chatPmFg,
            bgcolor: t.chatPmBg,
            border: `1px solid ${t.chatPmFg}`,
            borderRadius: 99,
            px: 0.5,
            py: 0.05,
          }}
        >
          {git.crLabel}
        </Box>
      )}
      <CiStatusIcon size={dense ? 14 : 16} status={git.ciStatus} />
      {git.architectureApproved ? <ArchApprovedTag dense={dense} /> : null}
    </Box>
  );
}
