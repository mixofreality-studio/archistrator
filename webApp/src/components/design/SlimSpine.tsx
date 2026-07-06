/**
 * Compact one-line progress spine across the phase's artifact steps. Each step is
 * a fixed-size pip joined by short rails: committed steps show a ✓, locked steps a
 * lock, the active step is labelled inline. Clicking a non-locked step navigates
 * to it (non-linear where allowed). Derived in the experience from the project
 * head-state slots + the active session.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Tooltip from '@mui/material/Tooltip';
import CheckIcon from '@mui/icons-material/Check';
import LockOutlinedIcon from '@mui/icons-material/LockOutlined';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';
import { StaleBasisMarker } from './StaleBasisChip';

/** One spine step: a phase artifact slot projected for the progress rail. */
export interface SpineStep {
  kind: string;
  title: string;
  committed: boolean;
  /** Locked = its prior step is not yet committed; not directly selectable. */
  locked: boolean;
  /** Committed but its upstream basis has since drifted — flags a reconcile marker. */
  stale?: boolean;
}

export function SlimSpine({
  steps,
  activeIndex,
  onSelect,
}: {
  steps: SpineStep[];
  activeIndex: number;
  onSelect: (i: number) => void;
}): ReactNode {
  const t = useTokens();
  return (
    <Box
      data-testid={UI_IDENTIFIERS.DesignExperience.SLIM_SPINE}
      sx={{ display: 'flex', alignItems: 'center', flexWrap: 'nowrap' }}
    >
      {steps.map((a, i) => {
        const done = a.committed;
        const active = i === activeIndex;
        const locked = a.locked && !active;
        return (
          <Box key={a.kind} sx={{ display: 'flex', alignItems: 'center', flexShrink: 0 }}>
            {i > 0 && (
              <Box
                sx={{ width: 18, height: 2, bgcolor: i <= activeIndex && done ? t.accent : t.line }}
              />
            )}
            {/* UX-P1-6/F61: key the Tooltip by the active step so a navigation
                (activeIndex change) REMOUNTS every tooltip, dismissing one left
                hanging open from the hover that preceded the click. */}
            <Tooltip
              disableHoverListener={active}
              key={`tt-${a.kind}-${String(activeIndex)}`}
              title={a.title}
            >
              <Box
                aria-label={a.title}
                data-testid={UI_IDENTIFIERS.DesignExperience.spineStep(a.kind)}
                role="button"
                sx={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 0.75,
                  cursor: locked ? 'not-allowed' : 'pointer',
                  px: active ? 1 : 0,
                  py: 0.4,
                  borderRadius: 99,
                  border: active ? `1.5px solid ${t.accent}` : '1.5px solid transparent',
                  bgcolor: active ? t.awaitingBg : 'transparent',
                  // UX-P1-5: an OUTLINE focus ring (not the app's global boxShadow
                  // ring, which the active pill's own bgcolor + border visually
                  // swallow). outlineOffset lifts it clear of the pill in every
                  // step state (committed / active / locked-but-selectable).
                  outline: 'none',
                  '&:focus-visible': {
                    outline: `2px solid ${t.accent}`,
                    outlineOffset: '2px',
                  },
                }}
                tabIndex={locked ? -1 : 0}
                onClick={() => {
                  if (!locked) onSelect(i);
                }}
                onKeyDown={(e) => {
                  if (locked) return;
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault();
                    onSelect(i);
                  }
                }}
              >
                {/* UX-P1-7/R8: the stale marker is a notification-dot anchored to
                    the OWNING circle's top-right corner (not an inline sibling that
                    read as ambiguously owned between adjacent pips). */}
                <Box sx={{ position: 'relative', display: 'flex', flexShrink: 0 }}>
                  <Box
                    sx={{
                      width: 22,
                      height: 22,
                      flexShrink: 0,
                      borderRadius: '50%',
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      fontFamily: t.mono,
                      fontWeight: 700,
                      fontSize: 11,
                      border: `1.5px solid ${t.line}`,
                      color: done || active ? t.accentText : t.muted,
                      bgcolor: done ? t.committedDot : active ? t.accent : 'transparent',
                      opacity: locked ? 0.6 : 1,
                    }}
                  >
                    {done ? (
                      <CheckIcon sx={{ fontSize: 13 }} />
                    ) : locked ? (
                      <LockOutlinedIcon sx={{ fontSize: 11 }} />
                    ) : (
                      i + 1
                    )}
                  </Box>
                  {a.stale === true ? (
                    <Box
                      sx={{
                        position: 'absolute',
                        top: -5,
                        right: -5,
                        display: 'flex',
                        borderRadius: '50%',
                        bgcolor: t.paperAlt,
                      }}
                    >
                      <StaleBasisMarker kind={a.kind} />
                    </Box>
                  ) : null}
                </Box>
                {active ? (
                  <Typography
                    sx={{
                      fontFamily: t.mono,
                      fontWeight: 700,
                      fontSize: 12,
                      color: t.awaitingFg,
                      whiteSpace: 'nowrap',
                    }}
                  >
                    {a.title}
                  </Typography>
                ) : null}
              </Box>
            </Tooltip>
          </Box>
        );
      })}
    </Box>
  );
}
