/**
 * An agent's prompt or charter, read as a DOCUMENT — never as a code well.
 *
 * Both are Markdown the platform writes for the agent: headings, steps, quoted
 * rules, emphasis, the odd inline name (`run_checks`, a file path). Shown in a
 * `<pre>` they read as source; rendered, they read as the brief the agent was
 * handed. Inline names keep a light chip (a short identifier is still a name),
 * and a fenced block — an example payload, a command — becomes a quoted
 * "Example" panel in the body face, so nothing renders as code.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import ReactMarkdown, { type Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';

import type { Tokens } from '../../utilities/theme/themes';

function components(t: Tokens): Components {
  return {
    // A fenced block: an example the agent was shown, quoted as prose.
    pre: ({ children }) => (
      <Box
        sx={{
          my: 1,
          px: 1.5,
          py: 1,
          borderLeft: `3px solid ${t.accent2}`,
          bgcolor: t.paperAlt,
          borderRadius: 1,
        }}
      >
        <Typography sx={{ fontFamily: t.body, fontSize: 11, fontWeight: 700, color: t.muted }}>
          Example
        </Typography>
        <Box
          sx={{
            fontFamily: t.body,
            fontSize: 12.5,
            whiteSpace: 'pre-line',
            overflowWrap: 'anywhere',
            '& code': { fontFamily: 'inherit', bgcolor: 'transparent', p: 0, fontSize: 'inherit' },
          }}
        >
          {children}
        </Box>
      </Box>
    ),
  };
}

export function PromptProse({
  markdown,
  label,
  t,
}: {
  markdown: string;
  /** The region's accessible name ("Prompt", "Charter of senior-developer"). */
  label: string;
  t: Tokens;
}): ReactNode {
  return (
    <Box
      aria-label={label}
      role="region"
      sx={{
        maxHeight: 520,
        overflow: 'auto',
        px: 1.5,
        py: 0.5,
        color: t.ink,
        bgcolor: t.paper,
        border: `1px solid ${t.line}`,
        borderRadius: 1,
        fontFamily: t.body,
        '& h1': { fontFamily: t.display, fontSize: 18, fontWeight: 800, mt: 1.5, mb: 1 },
        '& h2': { fontFamily: t.display, fontSize: 15.5, fontWeight: 800, mt: 2, mb: 0.75 },
        '& h3, & h4': { fontSize: 13.5, fontWeight: 700, mt: 1.5, mb: 0.5 },
        '& p': { fontSize: 13.5, lineHeight: 1.6, my: 0.75 },
        '& ul, & ol': { pl: 3, my: 0.75 },
        '& li': { fontSize: 13.5, lineHeight: 1.55, mb: 0.5 },
        '& strong': { fontWeight: 700 },
        '& a': { color: t.accent2 },
        '& code': {
          fontFamily: t.mono,
          fontSize: '0.85em',
          bgcolor: t.paperAlt,
          px: 0.5,
          borderRadius: 0.5,
          overflowWrap: 'anywhere',
        },
        '& blockquote': {
          borderLeft: `3px solid ${t.accent}`,
          bgcolor: t.awaitingBg,
          color: t.ink,
          m: 0,
          my: 1,
          px: 1.5,
          py: 0.25,
        },
        '& table': { borderCollapse: 'collapse', width: '100%', my: 1, fontSize: 13 },
        '& th, & td': { borderBottom: `1px solid ${t.line}`, p: 0.75, textAlign: 'left' },
        '& hr': { border: 0, borderTop: `1px solid ${t.line}`, my: 2 },
        '&:focus-visible': { outline: `2px solid ${t.accent}`, outlineOffset: 1 },
      }}
      tabIndex={0}
    >
      <ReactMarkdown components={components(t)} remarkPlugins={[remarkGfm]}>
        {markdown}
      </ReactMarkdown>
    </Box>
  );
}
