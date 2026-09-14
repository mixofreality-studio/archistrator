import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import OpenInNewIcon from '@mui/icons-material/OpenInNew';
import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import type { ProducedArtifactRow } from '../../../contracts/types';
import type { Tokens } from '../../../utilities/theme/themes';
import type { ArtifactRendererProps } from '../artifactRenderers';
import { NO_SURFACES_LABEL, noSurfacesSentence } from './frontendSurfacesCopy.ts';

/**
 * FrontendArtifactView — the renderer for FRONTEND (U-SPA*) activities.
 *
 * Renders two kind-specific produced artifacts (see server DeriveProduced):
 *   - `ui-design` — the UI-design CONCEPT, its `note` shown as structured prose
 *     (personas / screens / layout / flows). Multiple paragraphs supported.
 *   - `ui-code`   — the built UI. Its `source` carries the SPA ROUTE (a
 *     "/project/..." path), offered as an "Open in a new tab" link. A screenshot
 *     image path (.png/.jpg/.svg/.webp) is shown inline.
 *
 * NO LIVE IFRAME. The app refuses to be framed (CSP `frame-ancestors 'none'` +
 * `X-Frame-Options: DENY`, commit a4130340), so a same-origin frame of a route
 * can only ever render the browser's refusal page. It was also only honest for
 * the dogfood project, where the project being viewed IS the app serving the
 * console. A sandboxed mock (`srcdoc`, opaque origin) replaces it with the SPA
 * slice (architect design-renderer-data §2.3); until then the route opens in a
 * tab of its own.
 *
 * With no ui-design or ui-code record, the designer's §5.5 empty state says so:
 * the console never guesses routes.
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
      <Typography
        sx={{
          fontFamily: t.mono,
          fontWeight: 700,
          fontSize: 11,
          letterSpacing: '0.06em',
          color: t.ink,
        }}
      >
        UI DESIGN CONCEPT
      </Typography>
      <Typography
        sx={{ fontFamily: t.body, fontWeight: 700, fontSize: 13.5, color: t.ink, mt: 0.4 }}
      >
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
        <Typography
          sx={{
            fontFamily: t.mono,
            fontWeight: 700,
            fontSize: 11,
            letterSpacing: '0.06em',
            color: t.ink,
          }}
        >
          BUILT SURFACE
        </Typography>
        <Typography sx={{ fontFamily: t.body, fontWeight: 700, fontSize: 13, color: t.ink }}>
          {art.title}
        </Typography>
      </Box>

      {hasRoute ? (
        <Box sx={{ mt: 1, display: 'flex', flexDirection: 'column', gap: 0.5 }}>
          <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}>
            {source}
          </Typography>
          <Box>
            <Button
              component="a"
              data-testid={UI_IDENTIFIERS.Construction.FRONTEND_OPEN_LINK}
              endIcon={<OpenInNewIcon sx={{ fontSize: 14 }} />}
              href={source}
              rel="noreferrer"
              size="small"
              sx={{
                py: 0.25,
                fontFamily: t.mono,
                fontSize: 11,
                textTransform: 'none',
                color: t.accent,
              }}
              target="_blank"
              variant="outlined"
            >
              Open in a new tab
            </Button>
          </Box>
          <Typography sx={{ fontFamily: t.body, fontSize: 11.5, color: t.muted }}>
            The running app, not a design-time mock. It is not framed here: the app refuses to be
            framed, and a framed route would only show that refusal.
          </Typography>
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
        <Typography
          sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted, mt: 0.75, fontStyle: 'italic' }}
        >
          No route is recorded on this ui-code artifact, so there is nothing to open.
        </Typography>
      )}
    </Paper>
  );
}

export function FrontendArtifactView({ vm, t }: ArtifactRendererProps): ReactNode {
  return <FrontendSurfaces componentId={undefined} produced={vm.row.produced ?? []} t={t} />;
}

/**
 * The SPA's built surfaces from its produced records, or the §5.5 absence. The
 * pane places this on the Construction task WHATEVER the attempt state (a Not
 * started row included): no surface is recorded either way, and the empty state
 * says so rather than an unknown body that never mentions surfaces.
 */
export function FrontendSurfaces({
  produced,
  componentId,
  t,
}: {
  produced: readonly ProducedArtifactRow[];
  componentId: string | undefined;
  t: Tokens;
}): ReactNode {
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
        // The designer's §5.5: a real gap (awaiting ink on the label), calm copy.
        <Box
          data-testid={UI_IDENTIFIERS.Construction.FRONTEND_NO_SURFACES}
          sx={{ display: 'flex', flexDirection: 'column', gap: 0.5 }}
        >
          <Typography
            sx={{
              fontFamily: t.mono,
              fontWeight: 700,
              fontSize: 10,
              letterSpacing: '0.08em',
              color: t.awaitingFg,
            }}
          >
            {NO_SURFACES_LABEL}
          </Typography>
          <Typography sx={{ fontFamily: t.body, fontSize: 12.5, color: t.ink, lineHeight: 1.5 }}>
            {noSurfacesSentence(componentId)}
          </Typography>
        </Box>
      ) : null}
    </Box>
  );
}
