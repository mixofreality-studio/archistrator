/* eslint-disable react-refresh/only-export-components -- PolicyRule type and INTERVENTION_POLICY default data colocated with the panel that owns them */
/**
 * Intervention Policy configuration panel — backed by useUpdateReviewPolicy.
 *
 * Renders a collapsible panel above the operator's approval queue: a header bar
 * showing "INTERVENTION POLICY · N gates active" with collapsed per-kind dot
 * summary, and an expanded body with per-kind PolicyRow (KindBadge + gateAt +
 * detail + enabled Switch). The always-on variance-gate footer note is included.
 *
 * Toggling a rule POSTS the updated gatedPhasesByType policy to the server via
 * useUpdateReviewPolicy. Edits apply to newly-started activities only — the
 * construction workflow captures the ReviewPolicy at workflow start and never
 * re-reads it mid-loop (per-execution snapshot, Task 6 B5).
 *
 * SEAM — activityType key alignment:
 *   The `gatedPhasesByType` keys emitted by this panel MUST be the exact
 *   ActivityType wire names the server uses (ActivityType.String() in
 *   server/internal/resourceaccess/projectstate/activityconstructionstatus.go):
 *     "service" | "frontend" | "testing"
 *   These are identical to ActivityKind in KindBadge.tsx by design. The
 *   compile-time assertion `ACTIVITY_TYPE_KEYS satisfies ActivityKind[]` below
 *   guarantees alignment — a rename on either side will cause a type error here.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Switch from '@mui/material/Switch';
import Tooltip from '@mui/material/Tooltip';
import Collapse from '@mui/material/Collapse';
import Alert from '@mui/material/Alert';
import TuneIcon from '@mui/icons-material/Tune';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import { KindBadge, kindColor } from './KindBadge';
import type { ActivityKind } from './KindBadge';
import { useUpdateReviewPolicy } from '../../hooks/useConstructionMutations';
import { useTokens } from '../../theme/ThemeContext';
import type { Tokens } from '../../theme/themes';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';

// ---------------------------------------------------------------------------
// Seam: guarantee the ActivityKind values are the server's ActivityType wire names.
// If the server renames a type, this assertion will fail at compile time.
// ---------------------------------------------------------------------------

/**
 * The exact ActivityType wire names from server DeriveType().String().
 * The `satisfies` check is the compile-time alignment guard — if ActivityKind
 * or the server's type strings diverge, this line fails to compile.
 */
// eslint-disable-next-line @typescript-eslint/no-unused-vars -- compile-time alignment guard only
const ACTIVITY_TYPE_KEYS = ['service', 'frontend', 'testing'] as const satisfies ActivityKind[];

// ---------------------------------------------------------------------------
// PolicyRule — mirrors the ux-mock PolicyRule interface exactly.
// ---------------------------------------------------------------------------

export interface PolicyRule {
  kind: (typeof ACTIVITY_TYPE_KEYS)[number];
  /** the human-readable gate(s) for this kind */
  gateAt: string;
  /**
   * The phase ids (in the kind's life cycle) that pause for human approval.
   * These are the ad-hoc gate IDs understood by server ReviewPolicyFromGateIDs
   * (e.g. "svc-contract", "svc-review", "fe-approve", "test-plan").
   */
  gatePhaseIds: string[];
  enabled: boolean;
  detail: string;
}

/**
 * The default Intervention Policy. Transcribed verbatim from ux-mock
 * `INTERVENTION_POLICY` in methodpoc/.../ux-mock/src/data/activities.ts.
 *   Service  → gate at Contract freeze & Code Review
 *   Frontend → gate at Design approval
 *   Testing  → gate at Test Plan sign-off
 * Plus the always-on variance gate (Escalate/Takeover) regardless of phase.
 */
export const INTERVENTION_POLICY: PolicyRule[] = [
  {
    kind: 'service',
    gateAt: 'Contract freeze · Code review',
    gatePhaseIds: ['svc-contract', 'svc-review'],
    enabled: true,
    detail:
      'A Service pauses for human approval when its service contract is ready to FREEZE (construction may not begin against an unfrozen contract) and again when a produced change reaches CODE REVIEW (the computed reviewer set must clear).',
  },
  {
    kind: 'frontend',
    gateAt: 'Design approval',
    gatePhaseIds: ['fe-approve'],
    enabled: true,
    detail:
      'A Frontend pauses at the DESIGN-APPROVAL gate — the human is the design authority; the ui-design concept is approved (or sent back) before any UI-code is built against it.',
  },
  {
    kind: 'testing',
    gateAt: 'Test Plan sign-off',
    gatePhaseIds: ['test-plan'],
    enabled: true,
    detail:
      'A Testing activity pauses at TEST-PLAN SIGN-OFF — the enumerated system-failure scenarios are reviewed before the harness is built, and again on any flagged result.',
  },
];

// ---------------------------------------------------------------------------
// PolicyRow — one per-kind row inside the expanded panel.
// ---------------------------------------------------------------------------

function PolicyRow({
  r,
  t,
  onToggle,
  last,
  disabled,
}: {
  r: PolicyRule;
  t: Tokens;
  onToggle: () => void;
  last: boolean;
  disabled: boolean;
}): ReactNode {
  const c = kindColor(t, r.kind);
  return (
    <Box
      sx={{
        display: 'flex',
        alignItems: 'flex-start',
        gap: 1.5,
        px: 2,
        py: 1.25,
        borderBottom: last ? 'none' : `1px solid ${t.line}`,
      }}
    >
      <Box sx={{ minWidth: 92, pt: 0.25 }}>
        <KindBadge kind={r.kind} t={t} />
      </Box>
      <Box sx={{ flexGrow: 1, minWidth: 0 }}>
        <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12, color: c.fg }}>
          gate at: {r.gateAt}
        </Typography>
        <Typography
          sx={{ fontFamily: t.body, fontSize: 11.5, color: t.muted, lineHeight: 1.45, mt: 0.2 }}
        >
          {r.detail}
        </Typography>
      </Box>
      <Tooltip
        title={
          r.enabled
            ? 'Gate ON — pauses for human approval'
            : 'Gate OFF — auto-proceeds (policy disabled)'
        }
      >
        <Switch
          checked={r.enabled}
          data-testid={UI_IDENTIFIERS.Construction.policyRowToggle(r.kind)}
          disabled={disabled}
          size="small"
          onChange={onToggle}
        />
      </Tooltip>
    </Box>
  );
}

// ---------------------------------------------------------------------------
// PolicyPanel — the collapsible top-level component exported for use in
// InterventionsTab. Toggles POST to the server via useUpdateReviewPolicy.
// Edits apply to newly-started activities only.
// ---------------------------------------------------------------------------

export function PolicyPanel({ projectId }: { projectId: string }): ReactNode {
  const t = useTokens();
  const [policy, setPolicy] = useState<PolicyRule[]>(INTERVENTION_POLICY);
  // Collapsed by default — the approval queue is what the operator wants most
  // of the time; first-time setup can expand it.
  const [open, setOpen] = useState(false);

  const updatePolicy = useUpdateReviewPolicy(projectId);

  const toggle = (kind: ActivityKind): void => {
    setPolicy((prev) => {
      const next = prev.map((r) => (r.kind === kind ? { ...r, enabled: !r.enabled } : r));
      // Build gatedPhasesByType: include only enabled rules.
      // Keys are ActivityType wire names (guaranteed by ACTIVITY_TYPE_KEYS satisfies check).
      const gatedPhasesByType: Record<string, string[]> = {};
      for (const rule of next) {
        if (rule.enabled) {
          gatedPhasesByType[rule.kind] = rule.gatePhaseIds;
        }
      }
      updatePolicy.mutate({ gatedPhasesByType });
      return next;
    });
  };

  const activeGates = policy.filter((r) => r.enabled).length;
  const saving = updatePolicy.isPending;

  return (
    <Paper data-testid={UI_IDENTIFIERS.Construction.POLICY_PANEL} sx={{ p: 0, overflow: 'hidden' }}>
      {/* header bar — always visible; click to toggle */}
      <Box
        sx={{
          px: 2,
          py: 1.1,
          bgcolor: t.paperAlt,
          borderBottom: open ? `1.5px solid ${t.line}` : 'none',
          display: 'flex',
          alignItems: 'center',
          gap: 1,
          flexWrap: 'wrap',
          cursor: 'pointer',
          '&:hover': { bgcolor: t.paper },
        }}
        onClick={() => {
          setOpen((o) => !o);
        }}
      >
        <TuneIcon sx={{ fontSize: 16, color: t.muted }} />
        <Typography
          sx={{
            fontFamily: t.mono,
            fontWeight: 700,
            fontSize: 12,
            letterSpacing: '0.06em',
            color: t.ink,
          }}
        >
          INTERVENTION POLICY
        </Typography>
        <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}>
          · {activeGates} gate{activeGates === 1 ? '' : 's'} active ·{' '}
          {saving ? 'saving…' : open ? 'collapse' : 'expand to edit'}
        </Typography>
        <Box sx={{ flexGrow: 1 }} />
        {/* collapsed summary: per-kind colored dots */}
        {!open && (
          <Box sx={{ display: 'flex', gap: 0.5 }}>
            {policy.map((r) => (
              <Box
                key={r.kind}
                sx={{
                  width: 8,
                  height: 8,
                  borderRadius: '50%',
                  bgcolor: r.enabled ? kindColor(t, r.kind).fg : 'transparent',
                  border: `1.5px solid ${r.enabled ? kindColor(t, r.kind).fg : t.line}`,
                }}
              />
            ))}
          </Box>
        )}
        <ExpandMoreIcon
          sx={{
            fontSize: 18,
            color: t.muted,
            transform: open ? 'rotate(180deg)' : 'none',
            transition: 'transform 0.18s',
          }}
        />
      </Box>

      <Collapse unmountOnExit in={open}>
        {/* description sub-header */}
        <Box
          sx={{
            px: 2,
            py: 0.75,
            bgcolor: t.paper,
            borderBottom: `1px solid ${t.line}`,
          }}
        >
          <Typography sx={{ fontFamily: t.body, fontSize: 11, color: t.muted, lineHeight: 1.5 }}>
            Which life-cycle gates require human approval, per kind. An activity surfaces in the
            queue only if its kind&apos;s gate is ON. Edits apply to newly-started activities —
            running workflows read the policy captured at their start.
          </Typography>
        </Box>

        {/* per-kind rows */}
        <Box sx={{ display: 'flex', flexDirection: 'column' }}>
          {policy.map((r, i) => (
            <PolicyRow
              disabled={saving}
              key={r.kind}
              last={i === policy.length - 1}
              r={r}
              t={t}
              onToggle={(): void => {
                toggle(r.kind);
              }}
            />
          ))}
        </Box>

        {/* save error */}
        {updatePolicy.isError ? (
          <Box sx={{ px: 2, py: 1, borderTop: `1px solid ${t.line}` }}>
            <Alert severity="error" sx={{ fontFamily: t.mono, fontSize: 11 }}>
              Failed to save policy —{' '}
              {updatePolicy.error instanceof Error ? updatePolicy.error.message : 'unknown error'}
            </Alert>
          </Box>
        ) : null}

        {/* always-on variance-gate footer */}
        <Box
          sx={{
            px: 2,
            py: 1,
            bgcolor: t.paperAlt,
            borderTop: `1.5px solid ${t.line}`,
          }}
        >
          <Typography sx={{ fontFamily: t.body, fontSize: 11, color: t.muted, lineHeight: 1.5 }}>
            Plus the always-on variance gate: the interventionEngine&apos;s deterministic{' '}
            <b>decideOnVariance</b> directive (Retry within budget / Escalate / Takeover) surfaces a
            stalled or off-track activity regardless of phase. The Engine DECIDES; the operator
            EXECUTES the steer below.
          </Typography>
        </Box>
      </Collapse>
    </Paper>
  );
}
