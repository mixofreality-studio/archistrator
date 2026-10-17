/**
 * A run's log, read as lines — never as a terminal dump in a code well.
 *
 * A check's or a scenario's output is evidence a reviewer scans for the verdict
 * and the reason: which test passed, which failed, and the message that says why.
 * So each non-blank line is one row in the body face, numbered, with the lines
 * that carry a verdict marked in words and colour (PASS / FAIL / panic), and the
 * scroll bounded so a long log never pushes the review below the fold.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';

import type { Tokens } from '../../../utilities/theme/themes';

type Tone = 'pass' | 'fail' | 'plain';

/** The verdict a log line carries, if any (go test, Playwright, linters). */
function lineTone(line: string): Tone {
  const s = line.trim();
  if (/^(--- )?FAIL\b|^panic:|^Error:|\bFAIL$|^✘|^\s*×/.test(s)) return 'fail';
  if (/^(--- )?PASS\b|^ok\s|^✓|\bPASS$/.test(s)) return 'pass';
  return 'plain';
}

export function LogLines({
  text,
  t,
  label,
  testId,
}: {
  text: string;
  t: Tokens;
  /** The accessible name of the log (what ran). */
  label: string;
  testId?: string | undefined;
}): ReactNode {
  const lines = text.split('\n').filter((l) => l.trim().length > 0);
  const fails = lines.filter((l) => lineTone(l) === 'fail').length;
  return (
    <Box data-testid={testId} sx={{ display: 'flex', flexDirection: 'column', gap: 0.5 }}>
      <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.muted }}>
        {`${String(lines.length)} line${lines.length === 1 ? '' : 's'} of output${
          fails > 0 ? ` · ${String(fails)} reporting a failure` : ''
        }`}
      </Typography>
      {/* A focusable, named region: a keyboard user can scroll a long log. */}
      <Box
        aria-label={label}
        role="region"
        sx={{
          maxHeight: 360,
          overflow: 'auto',
          border: `1px solid ${t.line}`,
          borderRadius: 1,
          bgcolor: t.paper,
          '&:focus-visible': { outline: `2px solid ${t.accent}`, outlineOffset: 1 },
        }}
        tabIndex={0}
      >
        <Box component="ol" sx={{ m: 0, p: 0, listStyle: 'none' }}>
          {lines.map((l, i) => {
            const tone = lineTone(l);
            return (
              <Box
                component="li"
                // A log line's only identity is its position in the log.
                key={String(i)}
                sx={{
                  display: 'grid',
                  gridTemplateColumns: '3.5em minmax(0,1fr)',
                  columnGap: 1,
                  px: 1,
                  py: 0.25,
                  borderLeft: `3px solid ${
                    tone === 'fail' ? t.dangerFg : tone === 'pass' ? t.committedDot : 'transparent'
                  }`,
                  bgcolor: tone === 'fail' ? t.paperAlt : 'transparent',
                }}
              >
                <Typography
                  aria-hidden
                  sx={{ fontFamily: t.body, fontSize: 11, color: t.muted, textAlign: 'right' }}
                >
                  {String(i + 1)}
                </Typography>
                <Typography
                  sx={{
                    fontFamily: t.body,
                    fontSize: 12.5,
                    lineHeight: 1.45,
                    color: tone === 'fail' ? t.dangerFg : t.ink,
                    fontWeight: tone === 'plain' ? 400 : 600,
                    overflowWrap: 'anywhere',
                  }}
                >
                  {l.trim()}
                </Typography>
              </Box>
            );
          })}
        </Box>
      </Box>
    </Box>
  );
}
