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
import ArrowBackIcon from '@mui/icons-material/ArrowBack';
import LockOutlinedIcon from '@mui/icons-material/LockOutlined';
import BuildOutlinedIcon from '@mui/icons-material/BuildOutlined';
import { getRouteApi, useNavigate } from '@tanstack/react-router';
import { AppShell } from '../components/AppShell';
import { EconomicsStrip } from '../components/HomeBaseParts';
import { ReviewPolicyControl } from '../components/ReviewPolicyControl';
import { ArtifactPane } from '../components/ArtifactPane';
import { StageChip } from '../components/StageChip';
import { ErrorAlert } from '../components/shared/ErrorAlert';
import { CommentProvider } from '../components/comments/CommentContext';
import { CommittedSlotsProvider } from '../components/CommittedSlotsContext';
import { StructureFindingsProvider } from '../components/flow/StructureFindingsContext';
import { DeploymentHealthProvider } from '../components/flow/DeploymentHealthContext';
import { StaleBasisMarker } from '../components/design/StaleBasisChip';
import { ApiError } from '../contracts/errors';
import { useDesignHealth } from '../hooks/useDeliveryQueries';
import { useProject } from '../hooks/useDeliveryQueries';
import { useCapabilities } from '../hooks/useCapabilities';
import { useOperatedAppId } from '../hooks/useOperatedAppId';
import { useDeploymentHealth } from '../hooks/useDeploymentHealth';
import { operationsEnabled } from '../utilities/capabilities';
import { useSetProjectExecutionPolicy, useStartProject } from '../hooks/useDeliveryMutations';
import { useUser } from '../utilities/auth/UserContext';
import { currentPhaseOf, toArtifactTableOfContents } from '../contracts/adapters';
import type { ProjectStateWithGit } from '../contracts/types';
import { PHASE1_ORDER } from '../contracts/methodMetadata';
import { PLAN_PATH, planSearch } from '../contracts/routePaths';
import { useTokens } from '../utilities/theme/ThemeContext';
import type { Tokens } from '../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';

const routeApi = getRouteApi('/project/$projectId/home');

export function HomeBase(): ReactNode {
  const { projectId } = routeApi.useParams();
  const { data: project, isLoading, error, refetch } = useProject(projectId);

  // Ghost project: the catalog row exists (its repo was adopted) but the
  // head-state create never completed, so the project read 404s. Instead of a
  // raw error banner with no way out, show an honest recovery card.
  const notFound = error instanceof ApiError && error.status === 404;

  return (
    <AppShell projectId={projectId}>
      <Box
        data-testid={UI_IDENTIFIERS.HomeBase.SCREEN}
        sx={{ maxWidth: 1240, mx: 'auto', px: { xs: 2, md: 4 }, py: 4 }}
      >
        {notFound ? (
          <GhostProjectPanel
            error={error}
            projectId={projectId}
            onFinished={() => {
              void refetch();
            }}
          />
        ) : (
          <>
            <ErrorAlert error={error} />
            {isLoading ? (
              <Box sx={{ display: 'flex', justifyContent: 'center', py: 8 }}>
                <CircularProgress />
              </Box>
            ) : null}
            {project !== undefined && <HomeBaseBody project={project} projectId={projectId} />}
          </>
        )}
      </Box>
    </AppShell>
  );
}

/**
 * Recovery card for a ghost project — its GitHub repo was adopted but the
 * head-state initialization never finished, so `get-project` 404s. Offers the
 * idempotent CreateProject mutation as a one-click "finish setup" (owner is the
 * authenticated catalog scope, name is the projectId — the same call the landing
 * page makes), a way back to the catalog, and the raw technical detail tucked
 * into a collapsed <details> so it stays available but secondary.
 */
function GhostProjectPanel({
  projectId,
  error,
  onFinished,
}: {
  projectId: string;
  error: ApiError;
  onFinished: () => void;
}): ReactNode {
  const t = useTokens();
  const navigate = useNavigate();
  const createProject = useStartProject();
  const owner = useUser().sub;

  const finishSetup = (): void => {
    if (createProject.isPending) return;
    // Ghost-recovery re-init of an existing project: adoption is idempotent and the
    // operating model is already set, so pass selfOperated (the no-op default that
    // issues no set-operating-model call) rather than re-choosing it here.
    // This is the GHOST-RECOVERY path, and it names the EXISTING projectId — so it
    // takes StartProject's adopt branch (`projectID != nil`), which is idempotent and
    // is the one create-shaped call the REST route can express. See
    // StartProjectVars.projectId for why a brand-new project cannot.
    createProject.mutate(
      { projectId, name: projectId, owner, operatingModel: 'selfOperated', start: false },
      {
        onSuccess: () => {
          onFinished();
        },
      }
    );
  };

  return (
    <Paper
      data-testid={UI_IDENTIFIERS.HomeBase.GHOST_PANEL}
      sx={{
        maxWidth: 640,
        mx: 'auto',
        mt: 6,
        p: { xs: 3, md: 4 },
        display: 'flex',
        flexDirection: 'column',
        gap: 2,
        borderStyle: 'dashed',
      }}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.25 }}>
        <BuildOutlinedIcon sx={{ fontSize: 22, color: t.accent }} />
        <Typography component="h1" sx={{ color: t.ink }} variant="h5">
          This project isn&rsquo;t finished setting up
        </Typography>
      </Box>
      <Typography sx={{ color: t.muted, fontSize: 14.5, lineHeight: 1.6 }}>
        The repository for <strong>{projectId}</strong> was adopted, but its initial project state
        was never written — so there&rsquo;s nothing to show yet. This usually means the first setup
        step didn&rsquo;t complete. You can finish it now; it&rsquo;s safe to run again.
      </Typography>
      <ErrorAlert error={createProject.error} />
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, flexWrap: 'wrap' }}>
        <Button
          color="primary"
          data-testid={UI_IDENTIFIERS.HomeBase.GHOST_FINISH_SETUP}
          disabled={createProject.isPending}
          startIcon={<BuildOutlinedIcon />}
          variant="contained"
          onClick={finishSetup}
        >
          {createProject.isPending ? 'Finishing setup…' : 'Finish setup'}
        </Button>
        <Button
          color="inherit"
          data-testid={UI_IDENTIFIERS.HomeBase.GHOST_BACK}
          startIcon={<ArrowBackIcon />}
          sx={{ color: t.muted }}
          variant="text"
          onClick={() => void navigate({ to: '/' })}
        >
          Back to projects
        </Button>
      </Box>
      <Box
        component="details"
        sx={{
          mt: 0.5,
          fontFamily: t.mono,
          fontSize: 12,
          color: t.muted,
          '& summary': { cursor: 'pointer', userSelect: 'none' },
        }}
      >
        <Box component="summary">Technical detail</Box>
        <Box sx={{ mt: 1, whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>
          {`${error.message} (${error.code}, HTTP ${String(error.status)})`}
        </Box>
      </Box>
    </Paper>
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
  const setReviewPolicy = useSetProjectExecutionPolicy(projectId);
  // Live Design-Health findings for the architecture diagram's structure-finding
  // overlays (StructureFindingsProvider around the ArtifactPane below). Loading /
  // error → undefined → the diagram renders overlay-free.
  const { data: designHealth } = useDesignHealth(projectId);
  // The ArtifactPane's Architecture Deployment lens' live health tint (see
  // DeploymentHealthProvider below) — the same dormant-by-default arrangement the
  // two design containers use: nothing fires unless the operations capability is
  // on (D9) and the derived operated-app id has landed, and an absent overlay
  // renders the diagram untinted, never red.
  const operatedAppId = useOperatedAppId(projectId);
  const { data: deploymentHealth } = useDeploymentHealth(
    operatedAppId ?? '',
    operationsEnabled(useCapabilities())
  );

  // SYSTEM-DESIGN ONLY — the Phase-1 artifacts still in the drafting sequence
  // (PHASE1_ORDER), in Method order. The retired-in-place kinds are filtered out by
  // the same PHASE1_ORDER membership test, so an old project's committed
  // scrubbedRequirements / operationalConcepts / standardCheck slots simply don't
  // appear as TOC rows. No project-design (network/solutions/SDP) or construction
  // artifacts here.
  const toc = useMemo(() => {
    const all = toArtifactTableOfContents(project);
    const order = PHASE1_ORDER as readonly string[];
    return all
      .filter((a) => order.includes(a.kind))
      .sort((a, b) => order.indexOf(a.kind) - order.indexOf(b.kind));
  }, [project]);

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
  const currentPhase = currentPhaseOf(project);

  const openPlan = (): void => {
    void navigate({ to: PLAN_PATH, params: { projectId }, search: () => planSearch('list') });
  };

  return (
    <>
      <Box sx={{ display: 'flex', alignItems: 'flex-end', gap: 2, mb: 3, flexWrap: 'wrap' }}>
        <Box>
          <Typography sx={{ color: t.muted }} variant="overline">
            {project.owner}
          </Typography>
          <Typography component="h1" sx={{ color: t.ink }} variant="h3">
            {project.name}
          </Typography>
        </Box>
        <Box sx={{ flexGrow: 1 }} />
        {/* The header's "Resume <phase>" button is gone (stage 5 Task 13). It
            opened the design rail / the construction console — three doors for
            three phases — and every one of those routes now redirects to the
            plan. Retargeting it would have put a SECOND "open the plan" control
            two rows above the plan card, saying the same phase name the card's
            own eyebrow says. One door. */}
      </Box>

      <EconomicsStrip project={project} />

      {/* ONE card, not three (stage 5 §7.4). The three phase cards described a
          project that moved through System Design → Project Design →
          Construction as three separate places; the plan is the one place now,
          and Requirements / Architecture / Project Design are simply its first
          three activities. The headline state is the project's current phase —
          the same fact the top-right button already names — so the card says
          where the project is and opens the one surface that shows it. */}
      <Box sx={{ mb: 3, mt: 3 }}>
        <Paper
          data-testid={UI_IDENTIFIERS.HomeBase.OPEN_PLAN}
          sx={{ p: 2.5, display: 'flex', alignItems: 'center', gap: 2, flexWrap: 'wrap' }}
        >
          <Box sx={{ flexGrow: 1, minWidth: 0 }}>
            <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 13, color: t.muted }}>
              {`PHASE ${String(currentPhase.index)} · ${currentPhase.title.toUpperCase()}`}
            </Typography>
            <Typography sx={{ color: t.ink }} variant="h6">
              Project plan
            </Typography>
            <Typography sx={{ color: t.muted }} variant="caption">
              {currentPhase.subtitle}
            </Typography>
          </Box>
          <Button endIcon={<ArrowForwardIcon />} variant="outlined" onClick={openPlan}>
            Open plan
          </Button>
        </Paper>
      </Box>

      {/* Review-policy preset dial (vibes / checkpoints / full) + the permanent
          deploy/spend/schema risk-floor note. Small setting; the committed value
          reads back from the project view (the mutation invalidates it). */}
      <ReviewPolicyControl
        error={setReviewPolicy.error}
        pending={setReviewPolicy.isPending}
        preset={project.reviewPolicy?.preset}
        onChoose={(preset) => {
          setReviewPolicy.mutate(preset);
        }}
      />

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
                  {selected.stateAddress}
                </Typography>
              </Box>
              {selected.stage === 'awaitingReview' && (
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
                    {/* Task 13: this used to open the phase's own design rail at
                        this artifact's step. The gate lives on the artifact's
                        ACTIVITY now, and which activity a slot belongs to is the
                        plan's own mapping (planTiles / taskArtifactFor), not this
                        screen's — so the link goes to the plan, whose TASKS lens
                        lists exactly the decisions that are owed. */}
                    <Box
                      component="span"
                      sx={{ textDecoration: 'underline', cursor: 'pointer' }}
                      onClick={openPlan}
                    >
                      Review &amp; decide →
                    </Box>
                  </Typography>
                </Box>
              )}
              {/* The system-design artifacts render via the shared ArtifactRenderer.
                  The Architecture ('system') section is enriched with the component
                  service contracts once they exist (serviceContracts threaded in).

                  This home base is a READ-ONLY rendering: commenting lives ONLY in
                  the design / construction review experiences. The provider is
                  mounted disabled so the shared artifact views (which read the
                  CommentContext) render zero comment affordances here — no header
                  comment icons, no per-row hover buttons, no selection popover, no
                  test probe, no extra tab stops.

                  Review-thread note (F41): the durable reviewThread lives on the
                  co-authoring SESSION view, not the committed head-state slots this
                  pane reads — so a read-only thread is NOT trivially reusable here
                  (it would need a per-slot session fetch). Skipped per the F41
                  gating: comment AFFORDANCES stay design-experience-only. */}
              <CommentProvider enabled={false}>
                <StructureFindingsProvider findings={designHealth?.findings}>
                  <DeploymentHealthProvider healthByKey={deploymentHealth}>
                    <CommittedSlotsProvider slots={project.slots}>
                      <ArtifactPane
                        artifact={selected}
                        envelope={selectedEnvelope}
                        serviceContracts={project.serviceContracts}
                        systemEnvelope={project.slots.find((s) => s.kind === 'system')?.model}
                        useCasesEnvelope={
                          project.slots.find((s) => s.kind === 'coreUseCases')?.model
                        }
                      />
                    </CommittedSlotsProvider>
                  </DeploymentHealthProvider>
                </StructureFindingsProvider>
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
                minWidth: 0,
              }}
            >
              {a.title}
            </Typography>
            {/* Non-blocking basis-drift signal on a committed row (compact form). */}
            {a.staleBasis === true ? (
              <>
                <Box sx={{ flexGrow: 1 }} />
                <StaleBasisMarker kind={a.kind} />
              </>
            ) : null}
          </Box>
        );
      })}
    </Paper>
  );
}
