/**
 * Home base (route `/project/$projectId/home`): a project's SYSTEM-DESIGN
 * overview — a read-only living document of the eight Phase-1 artifacts
 * (Mission … Standard Check) and nothing about the project-design or
 * construction implementation. A LEFT named navigator selects the section; the
 * RIGHT pane renders it via the shared ArtifactRenderer. The Architecture
 * section is enriched with the component service contracts (interfaces +
 * diagrams) once they have been established in construction.
 */
import { useMemo, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import Chip from '@mui/material/Chip';
import CircularProgress from '@mui/material/CircularProgress';
import ArrowForwardIcon from '@mui/icons-material/ArrowForward';
import LockOutlinedIcon from '@mui/icons-material/LockOutlined';
import { getRouteApi, useNavigate } from '@tanstack/react-router';
import { AppShell } from '../components/AppShell';
import { EconomicsStrip } from '../components/HomeBaseParts';
import { ArtifactPane } from '../components/ArtifactPane';
import { StageChip } from '../components/StageChip';
import { ErrorAlert } from '../components/shared/ErrorAlert';
import { CommentProvider } from '../components/comments/CommentContext';
import { useProject } from '../hooks/useProject';
import {
  toArtifactTableOfContents,
  toPhaseCards,
  type PhaseCardView,
  type PhaseId,
} from '../api/adapters';
import type { ProjectStateWithGit } from '../api/types';
import { PHASE1_ORDER } from '../constants/MethodMetadata';
import { useTokens } from '../theme/ThemeContext';
import type { Tokens } from '../theme/themes';
import { UI_IDENTIFIERS } from '../constants/UIIdentifiers';

const routeApi = getRouteApi('/project/$projectId/home');

/** Defensive fallback so the header never crashes on an empty phase list. */
const FALLBACK_PHASE: PhaseCardView = {
  id: 'systemDesign',
  index: 1,
  title: 'System Design',
  subtitle: '',
  done: 0,
  total: 0,
  locked: false,
  active: true,
};

/** The experience route per phase: design experiences for 1–2, the console for 3. */
type PhaseRoute =
  | '/project/$projectId/design/system'
  | '/project/$projectId/design/project'
  | '/project/$projectId/construction';

const PHASE_DESIGN_ROUTE: Record<PhaseId, PhaseRoute | null> = {
  systemDesign: '/project/$projectId/design/system',
  projectDesign: '/project/$projectId/design/project',
  construction: '/project/$projectId/construction',
};

export function HomeBase(): ReactNode {
  const { projectId } = routeApi.useParams();
  const { data: project, isLoading, error } = useProject(projectId);

  return (
    <AppShell projectId={projectId}>
      <Box
        data-testid={UI_IDENTIFIERS.HomeBase.SCREEN}
        sx={{ maxWidth: 1240, mx: 'auto', px: { xs: 2, md: 4 }, py: 4 }}
      >
        <ErrorAlert error={error} />
        {isLoading ? (
          <Box sx={{ display: 'flex', justifyContent: 'center', py: 8 }}>
            <CircularProgress />
          </Box>
        ) : null}
        {project !== undefined && <HomeBaseBody project={project} projectId={projectId} />}
      </Box>
    </AppShell>
  );
}

function HomeBaseBody({
  projectId,
  project,
}: {
  projectId: string;
  project: ProjectStateWithGit;
}): ReactNode {
  const t = useTokens();
  const navigate = useNavigate();

  // SYSTEM-DESIGN ONLY — the eight Phase-1 artifacts, in Method order. No
  // project-design (network/solutions/SDP) or construction artifacts here.
  const toc = useMemo(() => {
    const all = toArtifactTableOfContents(project);
    const order = PHASE1_ORDER as readonly string[];
    return all
      .filter((a) => order.includes(a.kind))
      .sort((a, b) => order.indexOf(a.kind) - order.indexOf(b.kind));
  }, [project]);

  const phases = useMemo(() => toPhaseCards(project), [project]);

  // Default selection: first committed, else first non-empty, else first.
  const defaultKind =
    toc.find((a) => a.stage === 'committed')?.kind ??
    toc.find((a) => a.stage !== 'empty')?.kind ??
    toc[0]?.kind ??
    null;
  const [selectedKind, setSelectedKind] = useState<string | null>(defaultKind);
  const selected = toc.find((a) => a.kind === selectedKind) ?? toc[0];
  const selectedEnvelope = project.slots.find((s) => s.kind === selected?.kind)?.model;

  const committedCount = toc.filter((a) => a.stage === 'committed').length;
  const currentPhase = phases.find((p) => p.active) ?? phases[0] ?? FALLBACK_PHASE;
  const designRoute = PHASE_DESIGN_ROUTE[currentPhase.id];

  const openDesign = (route: PhaseRoute): void => {
    void navigate({ to: route, params: { projectId } });
  };

  return (
    <>
      <Box sx={{ display: 'flex', alignItems: 'flex-end', gap: 2, mb: 3, flexWrap: 'wrap' }}>
        <Box>
          <Typography sx={{ color: t.muted }} variant="overline">
            {project.owner}
          </Typography>
          <Typography sx={{ color: t.ink }} variant="h3">
            {project.name}
          </Typography>
        </Box>
        <Box sx={{ flexGrow: 1 }} />
        {designRoute !== null && (
          <Button
            color="primary"
            data-testid={UI_IDENTIFIERS.HomeBase.RESUME_DESIGN}
            endIcon={<ArrowForwardIcon />}
            size="large"
            variant="contained"
            onClick={() => {
              openDesign(designRoute);
            }}
          >
            {/* Phase-specific: "Resume Construction" / "Resume Project Design" / … */}
            {committedCount > 0 ? `Resume ${currentPhase.title}` : `Enter ${currentPhase.title}`}
          </Button>
        )}
      </Box>

      <EconomicsStrip project={project} />

      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, mb: 1.5, mt: 3 }}>
        <Typography sx={{ color: t.ink }} variant="h5">
          System design
        </Typography>
        <Chip
          label={`${String(committedCount)}/${String(toc.length)} committed`}
          size="small"
          sx={{ bgcolor: t.committedBg, color: t.committedFg }}
        />
      </Box>

      {/* Two-column: LEFT named navigator (section names, not green dots) +
          RIGHT artifact body. */}
      <Box
        sx={{
          display: 'flex',
          gap: 2.5,
          alignItems: 'flex-start',
          flexDirection: { xs: 'column', md: 'row' },
        }}
      >
        <Box
          data-testid={UI_IDENTIFIERS.HomeBase.ARTIFACT_TOC}
          sx={{
            width: { xs: '100%', md: 248 },
            flexShrink: 0,
            position: { md: 'sticky' },
            top: { md: 16 },
          }}
        >
          <ArtifactNav
            items={toc}
            selectedKind={selected?.kind ?? null}
            t={t}
            onSelect={(kind) => {
              setSelectedKind(kind);
            }}
          />
        </Box>

        <Paper sx={{ flexGrow: 1, minWidth: 0, p: { xs: 2.5, md: 4 }, minHeight: 420 }}>
          {selected !== undefined && (
            <>
              <Box
                sx={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 1.5,
                  mb: 2,
                  pb: 2,
                  borderBottom: `1px solid ${t.line}`,
                }}
              >
                <Typography sx={{ color: t.ink }} variant="h4">
                  {selected.title}
                </Typography>
                <StageChip stage={selected.stage} />
                <Box sx={{ flexGrow: 1 }} />
                <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>
                  {selected.file}
                </Typography>
              </Box>
              {selected.stage === 'awaitingReview' && designRoute !== null && (
                <Box
                  sx={{
                    mb: 2,
                    p: 1.25,
                    bgcolor: t.awaitingBg,
                    border: `1.5px solid ${t.line}`,
                    borderRadius: 1,
                  }}
                >
                  <Typography
                    sx={{
                      fontFamily: t.mono,
                      fontSize: 12,
                      color: t.awaitingFg,
                      display: 'flex',
                      alignItems: 'center',
                      gap: 1,
                    }}
                  >
                    <LockOutlinedIcon sx={{ fontSize: 14 }} /> This draft is awaiting your gate.
                    <Box
                      component="span"
                      sx={{ textDecoration: 'underline', cursor: 'pointer' }}
                      onClick={() => {
                        openDesign(designRoute);
                      }}
                    >
                      Review &amp; decide →
                    </Box>
                  </Typography>
                </Box>
              )}
              {/* The system-design artifacts render via the shared ArtifactRenderer.
                  The Architecture ('system') section is enriched with the component
                  service contracts once they exist (serviceContracts threaded in). */}
              <CommentProvider>
                <ArtifactPane
                  artifact={selected}
                  envelope={selectedEnvelope}
                  serviceContracts={project.serviceContracts}
                />
              </CommentProvider>
            </>
          )}
        </Paper>
      </Box>
    </>
  );
}

/** The left-hand named section navigator (replaces the horizontal SlimSpine). */
function ArtifactNav({
  items,
  selectedKind,
  onSelect,
  t,
}: {
  items: ReturnType<typeof toArtifactTableOfContents>;
  selectedKind: string | null;
  onSelect: (kind: string) => void;
  t: Tokens;
}): ReactNode {
  return (
    <Paper sx={{ p: 0.75, display: 'flex', flexDirection: 'column', gap: 0.25 }}>
      {items.map((a) => {
        const active = a.kind === selectedKind;
        const dot =
          a.stage === 'committed'
            ? t.committedDot
            : a.stage === 'awaitingReview'
              ? t.awaitingFg
              : t.line;
        return (
          <Box
            data-testid={UI_IDENTIFIERS.HomeBase.tocRow(a.kind)}
            key={a.kind}
            role="button"
            sx={{
              display: 'flex',
              alignItems: 'center',
              gap: 1,
              px: 1.25,
              py: 0.9,
              borderRadius: 1,
              cursor: 'pointer',
              borderLeft: `3px solid ${active ? t.accent : 'transparent'}`,
              bgcolor: active ? t.paperAlt : 'transparent',
              '&:hover': { bgcolor: t.paperAlt },
            }}
            tabIndex={0}
            onClick={() => {
              onSelect(a.kind);
            }}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                onSelect(a.kind);
              }
            }}
          >
            <Box sx={{ width: 8, height: 8, borderRadius: '50%', flexShrink: 0, bgcolor: dot }} />
            <Typography
              sx={{
                fontSize: 13.5,
                fontWeight: active ? 700 : 500,
                color: active ? t.ink : t.muted,
                whiteSpace: 'nowrap',
                overflow: 'hidden',
                textOverflow: 'ellipsis',
              }}
            >
              {a.title}
            </Typography>
          </Box>
        );
      })}
    </Paper>
  );
}
