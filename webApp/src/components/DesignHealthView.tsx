/**
 * Design Health — the step-8 replacement (Wave-2 reshape 3). The old Standard Check
 * was a committed teardown artifact; Design Health is a render-on-read dashboard fed
 * by the `getDesignHealth` read-model (useDesignHealth), joining three things:
 *
 *   1. LIVE findings — the ~40 mechanical methodcheck rules, recomputed on read and
 *      NEVER committed, grouped by rule FAMILY (the ruleId prefix, e.g. DH-GRAPH-* →
 *      "DH-GRAPH") with a per-family severity summary; each finding an MUI Alert
 *      coloured by severity (mirrors the GatePanel findings idiom).
 *   2. WAIVERS — the committed CheckItem waivers relocated onto their host artifacts,
 *      in plain language.
 *   3. ATTESTATIONS — the committed semantic-property attestations, each with evidence.
 *
 * The seal banner on top is derived CLIENT-SIDE (the endpoint is a pure health read)
 * from the live findings × the project's review-policy preset: under `vibes` the phase
 * auto-seals so the banner stays green/informational; under `checkpoints`/`full` it
 * asks for explicit ratification when clean and blocks when any error finding exists.
 * A drift note shows when the health was evaluated against a head-state older than the
 * project's current version (the checks may be stale).
 *
 * Self-fetching (reads projectId from the route, like OperationalConceptsView), so
 * ArtifactRenderer wires it as the step-8 body with no prop plumbing.
 */
import { useMemo, type ReactNode } from 'react';
import Alert from '@mui/material/Alert';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { useParams } from '@tanstack/react-router';
import type { CheckItem, Finding } from '../contracts/types';
import { useDesignHealth } from '../hooks/useDesignHealth';
import { useProject } from '../hooks/useProject';
import { useTokens } from '../utilities/theme/ThemeContext';
import type { Tokens } from '../utilities/theme/themes';

// ── Seal composition (client-side; endpoint stays a pure health read) ────────────────

type SealSeverity = 'success' | 'info' | 'warning' | 'error';
interface SealBannerModel {
  severity: SealSeverity;
  heading: string;
  message: string;
}

/**
 * The policy-conditional seal, from live findings × review-policy preset:
 *   - vibes (or an absent/unknown preset — the auto-seal default): green when clean;
 *     informational when there are error findings (vibes never blocks — the phase
 *     seals automatically, the findings are advisory).
 *   - checkpoints / full: warning "awaiting your ratification" when clean; error
 *     "blocked" when any error-severity finding exists.
 */
function composeSeal(findings: Finding[], preset: string | undefined): SealBannerModel {
  const hasError = findings.some((f) => f.severity === 'error');
  const gated = preset === 'checkpoints' || preset === 'full';
  if (!gated) {
    return hasError
      ? {
          severity: 'info',
          heading: 'Live checks found issues',
          message:
            'Under vibes oversight this phase seals automatically — review the findings below; none block progress.',
        }
      : {
          severity: 'success',
          heading: 'Design is healthy',
          message: 'All live checks pass. Under vibes oversight this phase seals automatically.',
        };
  }
  return hasError
    ? {
        severity: 'error',
        heading: 'Blocked — errors must be resolved',
        message:
          'Error-severity findings must be resolved or waived before this phase can seal under your oversight policy.',
      }
    : {
        severity: 'warning',
        heading: 'Awaiting your ratification',
        message:
          'All live checks pass. Under your oversight policy this phase needs your explicit sign-off to seal.',
      };
}

/** The rule family = the ruleId prefix up to its second segment (DH-GRAPH-NO-UPCALL → DH-GRAPH). */
function familyOf(ruleId: string): string {
  const secondDash = ruleId.indexOf('-', ruleId.indexOf('-') + 1);
  return secondDash > 0 ? ruleId.slice(0, secondDash) : ruleId;
}

// ── Render ───────────────────────────────────────────────────────────────────────

function SectionLabel({ children }: { children: ReactNode }): ReactNode {
  const t = useTokens();
  return (
    <Typography
      sx={{
        fontFamily: t.mono,
        fontWeight: 700,
        fontSize: '0.78rem',
        letterSpacing: '0.16em',
        textTransform: 'uppercase',
        color: t.muted,
        mb: 1.5,
      }}
    >
      {children}
    </Typography>
  );
}

/** The live findings, grouped by rule family, each family with a severity summary. */
function LiveFindings({ findings }: { findings: Finding[] }): ReactNode {
  const t = useTokens();
  const families = useMemo(() => {
    const byFamily = new Map<string, Finding[]>();
    for (const f of findings) {
      const key = familyOf(f.ruleId);
      const list = byFamily.get(key) ?? [];
      list.push(f);
      byFamily.set(key, list);
    }
    return [...byFamily.entries()].sort((a, b) => a[0].localeCompare(b[0]));
  }, [findings]);

  if (findings.length === 0) {
    return (
      <Alert severity="success" sx={{ alignItems: 'flex-start' }}>
        All live checks passed — no findings against the current design.
      </Alert>
    );
  }

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
      {families.map(([family, list]) => {
        const errors = list.filter((f) => f.severity === 'error').length;
        const warnings = list.filter((f) => f.severity === 'warning').length;
        return (
          <Box key={family}>
            <Typography
              sx={{ fontFamily: t.mono, fontSize: 12, fontWeight: 700, color: t.ink, mb: 0.75 }}
            >
              {family}
              <Box component="span" sx={{ ml: 1, color: t.muted, fontWeight: 400 }}>
                {errors > 0 ? `${String(errors)} error${errors === 1 ? '' : 's'}` : ''}
                {errors > 0 && warnings > 0 ? ' · ' : ''}
                {warnings > 0 ? `${String(warnings)} warning${warnings === 1 ? '' : 's'}` : ''}
              </Box>
            </Typography>
            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.75 }}>
              {list.map((f, i) => (
                <Alert
                  key={`${f.ruleId}-${String(i)}`}
                  severity={f.severity}
                  sx={{ alignItems: 'flex-start' }}
                >
                  <Typography sx={{ fontFamily: t.mono, fontSize: 11, fontWeight: 700 }}>
                    {f.ruleId}
                    {f.location !== undefined && f.location.section.length > 0 ? (
                      <Box component="span" sx={{ ml: 1, color: t.muted, fontWeight: 400 }}>
                        {f.location.section}
                      </Box>
                    ) : null}
                  </Typography>
                  <Typography sx={{ fontFamily: t.body, fontSize: '0.9rem', lineHeight: 1.5 }}>
                    {f.message}
                  </Typography>
                </Alert>
              ))}
            </Box>
          </Box>
        );
      })}
    </Box>
  );
}

/** The committed waivers, in plain language. */
function WaiverLedger({ waivers, t }: { waivers: CheckItem[]; t: Tokens }): ReactNode {
  if (waivers.length === 0) {
    return (
      <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>
        No standing waivers.
      </Typography>
    );
  }
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
      {waivers.map((w, i) => (
        <Box
          key={`${w.section}-${String(i)}`}
          sx={{ border: `1.5px solid ${t.line}`, borderRadius: 2, p: 1.5 }}
        >
          <Box sx={{ display: 'flex', gap: 1, alignItems: 'center', mb: 0.25 }}>
            <Box
              component="span"
              sx={{
                fontFamily: t.mono,
                fontSize: 10,
                fontWeight: 700,
                letterSpacing: '0.06em',
                px: 0.75,
                py: 0.15,
                borderRadius: 0.75,
                bgcolor: t.awaitingBg,
                color: t.awaitingFg,
              }}
            >
              WAIVED
            </Box>
            <Typography sx={{ fontFamily: t.mono, fontSize: 12, fontWeight: 700, color: t.ink }}>
              {w.section}
            </Typography>
          </Box>
          <Typography
            sx={{ fontFamily: t.body, fontSize: '0.92rem', color: t.ink, lineHeight: 1.5 }}
          >
            {w.guideline}
          </Typography>
          {w.justification.length > 0 && (
            <Typography
              sx={{
                fontFamily: t.body,
                fontSize: '0.85rem',
                fontStyle: 'italic',
                color: t.muted,
                mt: 0.5,
                lineHeight: 1.5,
              }}
            >
              Why waived: {w.justification}
            </Typography>
          )}
        </Box>
      ))}
    </Box>
  );
}

/** The committed semantic-property attestations, each with its evidence. */
function AttestationList({ attestations, t }: { attestations: CheckItem[]; t: Tokens }): ReactNode {
  if (attestations.length === 0) {
    return (
      <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>
        No attestations recorded.
      </Typography>
    );
  }
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.25 }}>
      {attestations.map((a, i) => (
        <Box key={`${a.section}-${String(i)}`}>
          <Typography sx={{ fontFamily: t.mono, fontSize: 12, fontWeight: 700, color: t.ink }}>
            {a.section}
          </Typography>
          <Typography
            sx={{ fontFamily: t.body, fontSize: '0.92rem', color: t.ink, lineHeight: 1.5 }}
          >
            {a.guideline}
          </Typography>
          {a.justification.length > 0 && (
            <Typography
              sx={{
                fontFamily: t.body,
                fontSize: '0.85rem',
                fontStyle: 'italic',
                color: t.muted,
                mt: 0.35,
                lineHeight: 1.5,
              }}
            >
              Evidence: {a.justification}
            </Typography>
          )}
        </Box>
      ))}
    </Box>
  );
}

export function DesignHealthView(): ReactNode {
  const t = useTokens();
  const params = useParams({ strict: false });
  const projectId = typeof params.projectId === 'string' ? params.projectId : '';
  const { data: health, isLoading } = useDesignHealth(projectId);
  const { data: project } = useProject(projectId);

  const preset = project?.reviewPolicy?.preset;
  const seal = useMemo(
    () => (health !== undefined ? composeSeal(health.findings, preset) : undefined),
    [health, preset]
  );

  // Drift: the health was computed against a head-state older than the project's
  // current version, so the checks may not reflect the latest committed changes. The
  // project version is the monotonic head-state revision the endpoint stamps into
  // evaluatedAtRevision.
  const drifted =
    health !== undefined && project !== undefined && health.evaluatedAtRevision < project.version;

  if (isLoading) {
    return (
      <Box sx={{ py: 6, textAlign: 'center', color: t.muted, fontFamily: t.mono }}>
        Checking design health…
      </Box>
    );
  }

  if (health === undefined || seal === undefined) {
    return (
      <Box sx={{ py: 6, textAlign: 'center', color: t.muted, fontFamily: t.mono }}>
        Design health is not available yet.
      </Box>
    );
  }

  return (
    <Box>
      <Alert severity={seal.severity} sx={{ alignItems: 'flex-start', mb: 3 }}>
        <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12.5 }}>
          {seal.heading}
          <Box component="span" sx={{ ml: 1, color: t.muted, fontWeight: 400 }}>
            oversight: {preset ?? 'vibes'}
          </Box>
        </Typography>
        <Typography sx={{ fontFamily: t.body, fontSize: '0.9rem', lineHeight: 1.5, mt: 0.25 }}>
          {seal.message}
        </Typography>
      </Alert>

      {drifted ? (
        <Alert severity="warning" sx={{ alignItems: 'flex-start', mb: 3 }}>
          <Typography sx={{ fontFamily: t.body, fontSize: '0.9rem', lineHeight: 1.5 }}>
            These checks were evaluated against an earlier version of the design (revision{' '}
            {String(health.evaluatedAtRevision)}, now at {String(project.version)}). Re-open the
            step to re-run them against the latest changes.
          </Typography>
        </Alert>
      ) : null}

      <Box sx={{ mb: 4 }}>
        <SectionLabel>Live checks</SectionLabel>
        <LiveFindings findings={health.findings} />
      </Box>

      <Box sx={{ mb: 4 }}>
        <SectionLabel>Waivers</SectionLabel>
        <WaiverLedger t={t} waivers={health.waivers} />
      </Box>

      <Box>
        <SectionLabel>Attestations</SectionLabel>
        <AttestationList attestations={health.attestations} t={t} />
      </Box>
    </Box>
  );
}
