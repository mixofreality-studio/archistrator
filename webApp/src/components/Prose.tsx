/**
 * Renders Method artifact markdown as clean, readable prose — the "living
 * document" feel. NEVER raw JSON: the screens project the typed model into
 * markdown via adapters.toMarkdown first, and this component themes it.
 * Ported from the frozen UX mock (react-markdown + remark-gfm).
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { useTokens } from '../theme/ThemeContext';
import { UI_IDENTIFIERS } from '../constants/UIIdentifiers';

export function Prose({
  markdown,
  source,
  artifactKind,
}: {
  markdown: string;
  source?: string | undefined;
  artifactKind?: string | undefined;
}): ReactNode {
  const t = useTokens();
  return (
    <Box
      data-artifact-kind={artifactKind}
      data-commentable={source}
      data-testid={UI_IDENTIFIERS.HomeBase.ARTIFACT_PROSE}
      sx={{
        color: t.ink,
        '& h2': {
          fontFamily: t.display,
          fontWeight: 800,
          fontSize: '1.5rem',
          letterSpacing: '-0.015em',
          mt: 0,
          mb: 1.5,
        },
        '& h3': {
          fontFamily: t.mono,
          fontWeight: 700,
          fontSize: '0.78rem',
          letterSpacing: '0.16em',
          textTransform: 'uppercase',
          color: t.muted,
          mt: 3.5,
          mb: 1.25,
        },
        '& p': { fontSize: '0.98rem', lineHeight: 1.65, my: 1.25 },
        '& ul, & ol': { pl: 3, my: 1.25 },
        '& li': { fontSize: '0.96rem', lineHeight: 1.6, mb: 0.75 },
        '& strong': { fontWeight: 700 },
        '& a': { color: t.accent2, textDecorationThickness: '1.5px' },
        '& code': {
          fontFamily: t.mono,
          fontSize: '0.82em',
          bgcolor: t.paperAlt,
          px: 0.6,
          py: 0.15,
          borderRadius: 1,
        },
        '& blockquote': {
          borderLeft: `3px solid ${t.accent}`,
          bgcolor: t.awaitingBg,
          color: t.awaitingFg,
          m: 0,
          mt: 2,
          px: 2,
          py: 0.5,
          '& p': { fontSize: '0.9rem' },
        },
        '& table': { borderCollapse: 'collapse', width: '100%', my: 2, fontSize: '0.9rem' },
        '& th': {
          fontFamily: t.mono,
          fontSize: '0.74rem',
          letterSpacing: '0.08em',
          textTransform: 'uppercase',
          textAlign: 'left',
          borderBottom: `1.5px solid ${t.line}`,
          p: 1,
        },
        '& td': { borderBottom: `1px solid ${t.line}`, p: 1, verticalAlign: 'top' },
        '& hr': { border: 0, borderTop: `1px solid ${t.line}`, my: 3 },
      }}
    >
      <ReactMarkdown remarkPlugins={[remarkGfm]}>{markdown}</ReactMarkdown>
    </Box>
  );
}
