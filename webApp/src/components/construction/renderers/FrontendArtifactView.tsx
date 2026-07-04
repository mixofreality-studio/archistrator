import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import OpenInNewIcon from '@mui/icons-material/OpenInNew';
import { UI_IDENTIFIERS } from '../../../constants/UIIdentifiers';
import type { ProducedArtifactRow } from '../../../api/types';
import type { Tokens } from '../../../theme/themes';
import type { ArtifactRendererProps } from '../artifactRenderers';

/**
 * FrontendArtifactView — the Artifacts-tab renderer for FRONTEND (U-SPA*) activities.
 *
 * Renders two kind-specific produced artifacts (see server DeriveProduced):
 *   - `ui-design` — the UI-design CONCEPT, its `note` shown as structured prose
 *     (personas / screens / layout / flows). Multiple paragraphs supported.
 *   - `ui-code`   — the built UI. Its `source` carries the SPA preview ROUTE (a
 *     "/project/..." path); for this dogfooded project the built UI is the running
 *     app, so the route is framed as a LIVE same-origin iframe with an "open" link.
 *     A screenshot image path (.png/.jpg/.svg/.webp) is supported as a fallback.
 *
 * Tolerates OLD stub data: if no ui-design/ui-code artifacts are present (e.g. a
 * legacy generic `code` stub or an empty produced[]), it degrades to an honest
 * "no UI artifacts yet" note rather than breaking.
 */

const IMAGE_RE = /\.(png|jpe?g|svg|webp|gif)$/i;

function isRoute(source: string): boolean {
  return source.startsWith('/');
}

function isImage(source: string): boolean {
  return IMAGE_RE.test(source);
}

/** Split a note into paragraphs on blank lines / single newlines for prose display. */
function paragraphs(note: string): string[] {
  return note
    .split(/\n{1,}/)
    .map((s) => s.trim())
    .filter((s) => s.length > 0);
}

function ConceptSection({ art, t }: { art: ProducedArtifactRow; t: Tokens }): ReactNode {
  const paras = paragraphs(art.note);
  return (
    <Paper sx={{ p: 1.5, borderLeft: `4px solid ${art.produced ? t.committedDot : t.line}` }}>
      <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11, letterSpacing: '0.06em', color: t.ink }}>
        UI DESIGN CONCEPT
      </Typography>
      <Typography sx={{ fontFamily: t.body, fontWeight: 700, fontSize: 13.5, color: t.ink, mt: 0.4 }}>
        {art.title}
      </Typography>
      {paras.length > 0 ? (
        paras.map((p, i) => (
          <Typography
            key={`${String(i)}-${p.slice(0, 12)}`}
            sx={{ fontFamily: t.body, fontSize: 12.5, color: t.ink, lineHeight: 1.5, mt: 0.6 }}
          >
            {p}
          </Typography>
        ))
      ) : (
        <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.muted, mt: 0.5 }}>
          The concept — personas, screens, layout, and flows — will render here once authored.
        </Typography>
      )}
    </Paper>
  );
}

function PreviewSection({ art, t }: { art: ProducedArtifactRow; t: Tokens }): ReactNode {
  const source = art.source;
  const hasRoute = source.length > 0 && isRoute(source) && !isImage(source);
  const hasImage = source.length > 0 && isImage(source);

  return (
    <Paper sx={{ p: 1.5, borderLeft: `4px solid ${art.produced ? t.committedDot : t.line}` }}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
        <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11, letterSpacing: '0.06em', color: t.ink }}>
          UI PREVIEW
        </Typography>
        <Typography sx={{ fontFamily: t.body, fontWeight: 700, fontSize: 13, color: t.ink }}>
          {art.title}
        </Typography>
        <Box sx={{ flexGrow: 1 }} />
        {hasRoute ? (
          <Button
            component="a"
            endIcon={<OpenInNewIcon sx={{ fontSize: 14 }} />}
            href={source}
            rel="noreferrer"
            size="small"
            sx={{ py: 0.25, fontFamily: t.mono, fontSize: 11, textTransform: 'none', color: t.accent }}
            target="_blank"
          >
            Open {source}
          </Button>
        ) : null}
      </Box>

      {hasRoute ? (
        <Box
          sx={{
            mt: 1,
            border: `1.5px solid ${t.line}`,
            borderRadius: 1,
            overflow: 'hidden',
            bgcolor: t.paperAlt,
            height: 480,
          }}
        >
          <Box
            component="iframe"
            data-testid={UI_IDENTIFIERS.Construction.FRONTEND_PREVIEW_FRAME}
            src={source}
            sx={{ width: '100%', height: '100%', border: 0, display: 'block' }}
            title={`Live preview · ${source}`}
          />
        </Box>
      ) : hasImage ? (
        <Box sx={{ mt: 1 }}>
          <Box
            alt={art.title}
            component="img"
            src={source}
            sx={{ maxWidth: '100%', borderRadius: 1, border: `1.5px solid ${t.line}` }}
          />
        </Box>
      ) : (
        <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted, mt: 0.75, fontStyle: 'italic' }}>
          No preview route recorded yet — the ui-code artifact carries a &quot;/project/…&quot; SPA route (or
          a screenshot path) that frames here once backfilled.
        </Typography>
      )}
    </Paper>
  );
}

export function FrontendArtifactView({ vm, t }: ArtifactRendererProps): ReactNode {
  const produced = vm.row.produced ?? [];
  const designs = produced.filter((a) => a.kind === 'ui-design');
  const codes = produced.filter((a) => a.kind === 'ui-code');

  const hasUiArtifacts = designs.length > 0 || codes.length > 0;

  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.FRONTEND_VIEW}
      sx={{ display: 'flex', flexDirection: 'column', gap: 1.5, minWidth: 0 }}
    >
      {designs.map((art, i) => (
        <ConceptSection art={art} key={`design-${String(i)}`} t={t} />
      ))}
      {codes.map((art, i) => (
        <PreviewSection art={art} key={`code-${String(i)}`} t={t} />
      ))}

      {!hasUiArtifacts ? (
        <Paper sx={{ p: 1.5 }}>
          <Typography sx={{ fontFamily: t.body, fontSize: 12.5, color: t.muted, lineHeight: 1.5 }}>
            No UI artifacts recorded yet for this surface. A frontend activity records a ui-design concept
            and a ui-code preview (a live SPA route) once construction produces them; this legacy entry
            predates that shape.
          </Typography>
        </Paper>
      ) : null}
    </Box>
  );
}
