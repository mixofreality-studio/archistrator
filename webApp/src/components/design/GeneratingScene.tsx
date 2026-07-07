/**
 * The "AI at work" loader shown while an artifact is drafting/redrafting. A small
 * drafting-desk scene (worker at a board, self-drawing blueprint lines, turning
 * gears) plus an HONEST indeterminate status line + the standing "design job running
 * in your GitHub Actions" affordance. Recolored from tokens.
 *
 * HONESTY (QA F15 gap 2c): the server exposes only a single coarse stage
 * (drafting/redrafting) — it does NOT report per-phase DRAFT/VALIDATE/CRITIQUE
 * progress. The former animated ticker rendered cumulative ✓ checkmarks on those
 * phases purely off a wall-clock timer (and rotating quips asserting specific machine
 * activities like "Machine checking cross-artifact rules…"), which LIED that work had
 * completed while nothing had. Both are removed: this scene now shows only what is
 * TRUE — an indeterminate "drafting" indicator and the tracked-CI-job explanation.
 * The drafting-desk illustration stays as clearly-decorative chrome.
 *
 * Drafting is ASYNC: the draft is produced by a GitHub Action running in the USER's CI
 * (minutes per draft), not an inline server call. The standing affordance (+ optional
 * link to the repo's Actions tab) makes the minutes-long wait read as a tracked job,
 * never a hung spinner.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Link from '@mui/material/Link';
import OpenInNewIcon from '@mui/icons-material/OpenInNew';
import { useTokens } from '../../utilities/theme/ThemeContext';
import type { Tokens } from '../../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

export function GeneratingScene({
  artifact,
  actionsUrl,
  amendingRevision,
}: {
  artifact: string;
  /**
   * Optional deep-link to the repo's GitHub Actions tab where the design job runs.
   * When absent (we don't always hold the repo host), the affordance still explains
   * the CI wait — it just omits the link rather than fabricating one.
   */
  actionsUrl?: string;
  /**
   * When set, this drafting run is amending an ALREADY-COMMITTED artifact (revision
   * N). Surfaces an honest framing line so the committed header + generating scene
   * don't read as unexplained: the committed version stays current until the new
   * revision is approved. Omitted for a first-time draft.
   */
  amendingRevision?: number | undefined;
}): ReactNode {
  const t = useTokens();

  return (
    <Box
      data-testid={UI_IDENTIFIERS.DesignExperience.GENERATING_SCENE}
      sx={{
        border: `1.5px solid ${t.line}`,
        borderRadius: t.radius / 8 + 0.5,
        bgcolor: t.paper,
        px: 3,
        py: 5,
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        gap: 3,
        overflow: 'hidden',
        position: 'relative',
        // Same centered-card treatment as DraftFailedPanel: a capped width centered in
        // the content area, so the loader is a tidy centered card at wide viewports
        // rather than a full-bleed band with its content adrift in whitespace.
        width: '100%',
        maxWidth: 720,
        mx: 'auto',
      }}
    >
      <Typography
        sx={{ fontFamily: t.mono, fontSize: 12, letterSpacing: '0.16em', color: t.muted }}
      >
        GENERATING · {artifact.toUpperCase()}
      </Typography>

      {/* Amendment-in-flight framing: when the slot is already committed, the header
          still reads COMMITTED and this scene is drafting the NEXT revision — say so
          honestly, so the committed header + generating scene don't look contradictory. */}
      {amendingRevision !== undefined ? (
        <Box
          data-testid={UI_IDENTIFIERS.DesignExperience.AMENDING_NOTICE}
          sx={{
            maxWidth: 520,
            px: 2,
            py: 1,
            borderLeft: `3px solid ${t.accent}`,
            bgcolor: t.committedBg,
            borderRadius: t.radius / 8 + 0.5,
          }}
        >
          <Typography sx={{ fontSize: 12.5, color: t.ink, lineHeight: 1.5 }}>
            {amendingRevision >= 1
              ? `Amending committed revision ${String(amendingRevision)} — the committed version remains current until this revision is approved.`
              : 'Amending the committed version — it remains current until this revision is approved.'}
          </Typography>
        </Box>
      ) : null}

      <DraftingDesk t={t} />

      {/* HONEST indeterminate indicator: a single "drafting" pill that only claims what
          is true (a draft job is in progress), with no fabricated per-phase checkmarks. */}
      <Box
        sx={{
          fontFamily: t.mono,
          fontWeight: 700,
          fontSize: 11,
          letterSpacing: '0.12em',
          px: 1.25,
          py: 0.5,
          border: `1.5px solid ${t.accent}`,
          borderRadius: t.radius / 8 + 0.5,
          color: t.accent,
          animation: 'pulse 1.4s ease-in-out infinite',
          '@keyframes pulse': { '0%,100%': { opacity: 1 }, '50%': { opacity: 0.5 } },
        }}
      >
        DRAFTING…
      </Box>

      {/* Async-job affordance: the draft runs as a GitHub Action in the user's CI
          (minutes), so we say so explicitly — never a hung-looking spinner. */}
      <Box
        data-testid={UI_IDENTIFIERS.DesignExperience.CI_JOB_NOTICE}
        sx={{
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          gap: 0.5,
          mt: 0.5,
          px: 2,
          py: 1.25,
          borderTop: `1.5px solid ${t.line}`,
          width: '100%',
          textAlign: 'center',
        }}
      >
        <Typography sx={{ fontSize: 12.5, color: t.muted, maxWidth: 480, lineHeight: 1.5 }}>
          The design job is running in your repository&apos;s GitHub Actions. This takes a few
          minutes — you can leave and come back; this view updates itself.
        </Typography>
        {actionsUrl !== undefined && actionsUrl.length > 0 ? (
          <Link
            data-testid={UI_IDENTIFIERS.DesignExperience.CI_JOB_LINK}
            href={actionsUrl}
            rel="noopener"
            sx={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: 0.5,
              fontSize: 12.5,
              fontFamily: t.mono,
            }}
            target="_blank"
          >
            View the run in GitHub Actions
            <OpenInNewIcon sx={{ fontSize: 14 }} />
          </Link>
        ) : null}
      </Box>
    </Box>
  );
}

function DraftingDesk({ t }: { t: Tokens }): ReactNode {
  const a = t.accent;
  const a2 = t.accent2;
  return (
    <Box
      sx={{
        '@keyframes spin': { to: { transform: 'rotate(360deg)' } },
        '@keyframes spinr': { to: { transform: 'rotate(-360deg)' } },
        '@keyframes draw': { to: { strokeDashoffset: 0 } },
        '@keyframes arm': {
          '0%,100%': { transform: 'rotate(-6deg)' },
          '50%': { transform: 'rotate(8deg)' },
        },
        '@keyframes blink': { '0%,90%,100%': { opacity: 1 }, '95%': { opacity: 0.2 } },
        '@keyframes float': {
          '0%,100%': { transform: 'translateY(0)' },
          '50%': { transform: 'translateY(-4px)' },
        },
      }}
    >
      <svg fill="none" height="190" viewBox="0 0 340 190" width="340">
        <rect
          fill={t.paperAlt}
          height="120"
          rx="3"
          stroke={t.line}
          strokeWidth="1.5"
          width="180"
          x="22"
          y="28"
        />
        {Array.from({ length: 8 }).map((_, i) => (
          <line
            key={`v${String(i)}`}
            stroke={a}
            strokeOpacity="0.12"
            strokeWidth="1"
            x1={22 + i * 22.5}
            x2={22 + i * 22.5}
            y1="28"
            y2="148"
          />
        ))}
        {Array.from({ length: 5 }).map((_, i) => (
          <line
            key={`h${String(i)}`}
            stroke={a}
            strokeOpacity="0.12"
            strokeWidth="1"
            x1="22"
            x2="202"
            y1={28 + i * 24}
            y2={28 + i * 24}
          />
        ))}
        <path
          d="M44 120 L44 60 L104 60 L104 92 L150 92 L150 50 L184 50"
          stroke={a}
          strokeDasharray="320"
          strokeDashoffset="320"
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeWidth="2.5"
          style={{ animation: 'draw 2.4s ease-in-out infinite alternate' }}
        />
        <path
          d="M70 120 L70 96 L120 96"
          stroke={a2}
          strokeDasharray="90"
          strokeDashoffset="90"
          strokeLinecap="round"
          strokeWidth="2"
          style={{ animation: 'draw 2.4s 0.4s ease-in-out infinite alternate' }}
        />
        <circle cx="104" cy="60" fill={a} r="3.5" style={{ animation: 'blink 2.4s infinite' }} />
        <circle
          cx="150"
          cy="92"
          fill={a2}
          r="3.5"
          style={{ animation: 'blink 2.4s 0.6s infinite' }}
        />
        <g
          style={{ animation: 'spin 4s linear infinite', transformOrigin: '250px 44px' }}
          transform="translate(250 44)"
        >
          <Gear fill={a} r={16} />
        </g>
        <g
          style={{ animation: 'spinr 3s linear infinite', transformOrigin: '278px 64px' }}
          transform="translate(278 64)"
        >
          <Gear fill={a2} r={11} />
        </g>
        <g style={{ animation: 'float 3s ease-in-out infinite' }}>
          <rect fill={t.line} height="8" rx="2" width="86" x="232" y="138" />
          <rect fill={t.line} height="26" width="6" x="240" y="146" />
          <rect fill={t.line} height="26" width="6" x="304" y="146" />
          <rect
            fill={t.paperAlt}
            height="34"
            rx="2"
            stroke={a}
            strokeWidth="1.5"
            width="44"
            x="236"
            y="104"
          />
          <rect fill={a} height="26" rx="6" width="20" x="286" y="112" />
          <circle cx="296" cy="104" fill={t.ink} r="9" />
          <line
            stroke={t.ink}
            strokeLinecap="round"
            strokeWidth="4"
            style={{ animation: 'arm 1.2s ease-in-out infinite', transformOrigin: '288px 120px' }}
            x1="288"
            x2="262"
            y1="120"
            y2="122"
          />
        </g>
        <text
          fill={a}
          fontFamily={t.mono}
          fontSize="18"
          style={{ animation: 'blink 1.4s infinite' }}
          x="170"
          y="176"
        >
          ⌁
        </text>
      </svg>
    </Box>
  );
}

function Gear({ r, fill }: { r: number; fill: string }): ReactNode {
  const teeth = 8;
  const inner = r * 0.55;
  const points = Array.from({ length: teeth * 2 }).map((_, i) => {
    const ang = (Math.PI * i) / teeth;
    const rad = i % 2 === 0 ? r : r * 0.78;
    return `${String(Math.cos(ang) * rad)},${String(Math.sin(ang) * rad)}`;
  });
  return (
    <>
      <polygon fill={fill} fillOpacity="0.85" points={points.join(' ')} />
      <circle cx="0" cy="0" fill="none" r={inner} stroke="#0008" strokeWidth="2" />
    </>
  );
}
