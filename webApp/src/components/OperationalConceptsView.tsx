/**
 * The Deployment & Operations Model viewer (slot 6, wire kind `operationalConcepts`).
 *
 * The artifact was re-scoped (Wave-2): platform DOCTRINE moved to a read-only method
 * asset, and only the customer's per-project SELECTIONS + trust story + the deployment
 * picture stay committed here. This screen renders, top to bottom:
 *
 *   (a) the ratifiable per-project knobs (scenario / venue / review policy / scaling)
 *       as customer-summary cards over a collapsed engineer-detail line — each
 *       comment-anchorable at its typed field,
 *   (b) the three customer trust summaries (billing / usage-metering / data-ownership)
 *       prominently, each comment-anchorable,
 *   (c) the supported infrastructure building blocks, each comment-anchorable,
 *   (d) the deployment topology per profile (existing DeploymentFlow; profile choice
 *       survives remounts via module viewMemory), and
 *   (e) a read-only "Platform runtime doctrine" REFERENCE — the app cannot read the
 *       platform asset at runtime (no server endpoint), so it points at where the
 *       doctrine lives rather than duplicating it; never commentable / ratifiable.
 *
 * Instances in (d) are coloured by their System component's Method layer, so the
 * section joins against the committed System artifact from the project head-state.
 */
import { useMemo, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import ButtonBase from '@mui/material/ButtonBase';
import IconButton from '@mui/material/IconButton';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import ToggleButton from '@mui/material/ToggleButton';
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup';
import ChatBubbleOutlineIcon from '@mui/icons-material/ChatBubbleOutline';
import { useParams } from '@tanstack/react-router';
import {
  listDeploymentProfiles,
  toDeploymentOperationsView,
  toMissionView,
} from '../contracts/adapters';
import {
  linkedObjectives,
  type DeploymentKnob,
  type LinkedObjective,
  type ObjectiveLinks,
} from '../contracts/deploymentOpsLogic';
import type {
  ArtifactModelEnvelope,
  DeploymentProfile,
  HealthState,
  Objective,
} from '../contracts/types';
import { useProject } from '../hooks/useProject';
import { useCapabilities } from '../hooks/useCapabilities';
import { useDeploymentHealth } from '../hooks/useDeploymentHealth';
import { operationsEnabled } from '../utilities/capabilities';
import { useTokens } from '../utilities/theme/ThemeContext';
import {
  useComments,
  deploymentOpsFieldAnchor,
  trustSummaryAnchor,
  infraBlockAnchor,
} from './comments/CommentContext';
import { CommentableList } from './comments/CommentableList';
import { DeploymentFlow } from './flow/DeploymentFlow';
import { StepLink } from './shared/StepLink';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';

const PROFILE_LABEL: Record<DeploymentProfile, string> = {
  cloud: 'Cloud',
  local: 'Local',
  test: 'Test',
};

/**
 * Module-level memory of the last-picked deployment profile. This view can remount
 * (a background refetch / render-branch flip), which would otherwise snap the picker
 * back to the first profile. Persisting the choice outside the component instance
 * keeps it put across remounts; a stale profile self-heals via the guard below (it
 * falls back to the first present profile when the stored one isn't in the model).
 */
const viewMemory: { profile: DeploymentProfile | '' } = { profile: '' };

/** Uppercase mono section label, matching the other artifact screens. */
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

/**
 * Ch.-5 traceability chips on a per-project knob: "Obj N" links to the Mission
 * step for each business objective the knob's objectiveLinks cite, the joined
 * objective statement as tooltip. Renders nothing when the committed state
 * predates objectiveLinks (empty chips) — graceful older-state degradation.
 */
function ObjectiveChips({
  knob,
  chips,
}: {
  knob: DeploymentKnob;
  chips: LinkedObjective[];
}): ReactNode {
  const t = useTokens();
  if (chips.length === 0) return null;
  return (
    <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.6, mt: 0.6 }}>
      {chips.map((c) => {
        const link = (
          <StepLink
            kind="mission"
            label={`Realizes business objective ${String(c.number)}`}
            sx={{
              display: 'inline-block',
              px: 0.7,
              py: 0.15,
              borderRadius: 1,
              border: `1px solid ${t.line}`,
              bgcolor: t.paperAlt,
              fontFamily: t.mono,
              fontSize: 10,
              color: t.accent2,
              letterSpacing: '0.02em',
              '&:hover': { borderColor: t.accent, textDecoration: 'underline' },
            }}
            testId={UI_IDENTIFIERS.Deployment.objectiveLink(knob, c.number)}
            underline="none"
          >
            Obj {c.number}
          </StepLink>
        );
        // Tooltip needs a ref-holding child; StepLink is a plain function
        // component, so a span wrapper carries the ref (the disabled-button idiom).
        return c.statement !== '' ? (
          <Tooltip key={c.number} title={c.statement}>
            <Box component="span">{link}</Box>
          </Tooltip>
        ) : (
          <Box component="span" key={c.number}>
            {link}
          </Box>
        );
      })}
    </Box>
  );
}

/**
 * One ratifiable per-project selection: the customer summary as the readable main
 * line, an expandable engineer-detail line beneath, and a comment button anchored at
 * the typed field (revealed on hover / focus-within / when armed, exactly like the
 * CommentableList rows and Mission's ProseSection).
 */
function KnobCard({
  label,
  field,
  summary,
  detail,
  objectives,
}: {
  label: string;
  field: 'deploymentScenario' | 'constructionVenue' | 'reviewPolicyRef' | 'scalingPolicy';
  summary: string;
  detail: string;
  /** The knob's objectiveLinks join (empty on older states — renders nothing). */
  objectives: LinkedObjective[];
}): ReactNode {
  const t = useTokens();
  const { setAnchor, enabled, anchor: armedAnchor, comments } = useComments();
  const [open, setOpen] = useState(false);
  const thisPath = deploymentOpsFieldAnchor(field);
  const isArmed = armedAnchor?.jsonPath === thisPath;
  const hasComments = comments.some((c) => c.anchor?.jsonPath === thisPath);
  const revealed = isArmed || hasComments;
  return (
    <Box
      sx={{
        border: `1.5px solid ${isArmed ? t.accent : t.line}`,
        borderRadius: 2,
        p: 1.5,
        bgcolor: t.paper,
        display: 'flex',
        alignItems: 'flex-start',
        gap: 1,
        '& .commentable-section-action': {
          opacity: revealed ? 1 : 0,
          transition: 'opacity 120ms',
        },
        '&:hover .commentable-section-action, &:focus-within .commentable-section-action': {
          opacity: 1,
        },
        '@media (hover: none)': { '& .commentable-section-action': { opacity: 1 } },
      }}
    >
      <Box sx={{ flexGrow: 1, minWidth: 0 }}>
        <Typography
          sx={{
            fontFamily: t.mono,
            fontSize: 10,
            fontWeight: 700,
            letterSpacing: '0.1em',
            textTransform: 'uppercase',
            color: t.muted,
            mb: 0.4,
          }}
        >
          {label}
        </Typography>
        <Typography sx={{ fontFamily: t.body, fontSize: '0.98rem', color: t.ink, lineHeight: 1.5 }}>
          {summary}
        </Typography>
        <ObjectiveChips chips={objectives} knob={field} />
        <ButtonBase
          aria-expanded={open}
          sx={{
            mt: 0.5,
            justifyContent: 'flex-start',
            fontFamily: t.mono,
            fontSize: 10.5,
            fontWeight: 700,
            color: t.muted,
            letterSpacing: '0.03em',
            '&:hover': { color: t.ink },
          }}
          onClick={() => {
            setOpen((o) => !o);
          }}
        >
          {open ? '▾' : '▸'} engineer detail
        </ButtonBase>
        {open ? (
          <Typography
            sx={{
              mt: 0.4,
              fontFamily: t.mono,
              fontSize: 11.5,
              color: t.muted,
              lineHeight: 1.5,
              pl: 1.25,
              borderLeft: `2px solid ${t.line}`,
            }}
          >
            {detail}
          </Typography>
        ) : null}
      </Box>
      {enabled ? (
        <Tooltip title={`Comment on ${label}`}>
          <IconButton
            aria-label={`Comment on ${label}`}
            className="commentable-section-action"
            size="small"
            sx={{
              flexShrink: 0,
              color: t.accentText,
              bgcolor: t.accent,
              border: `1.5px solid ${t.line}`,
              borderRadius: 1,
              '&:hover': { bgcolor: t.accent2 },
            }}
            onClick={() => {
              setAnchor({
                kind: 'node',
                label,
                source: `Deployment & Operations · ${label}`,
                jsonPath: thisPath,
              });
            }}
          >
            <ChatBubbleOutlineIcon sx={{ fontSize: 15 }} />
          </IconButton>
        </Tooltip>
      ) : null}
    </Box>
  );
}

/** The read-only doctrine reference (not per-project state, never commentable). */
function DoctrineReference(): ReactNode {
  const t = useTokens();
  const [open, setOpen] = useState(false);
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Deployment.DOCTRINE}
      sx={{ mt: 4, border: `1.5px dashed ${t.line}`, borderRadius: 2, p: 1.5, bgcolor: t.paperAlt }}
    >
      <ButtonBase
        aria-expanded={open}
        sx={{ width: '100%', justifyContent: 'flex-start', textAlign: 'left' }}
        onClick={() => {
          setOpen((o) => !o);
        }}
      >
        <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12.5, color: t.ink }}>
          {open ? '▾' : '▸'} Platform runtime doctrine — how systems built here run
        </Typography>
      </ButtonBase>
      {open ? (
        <Box sx={{ mt: 1 }}>
          <Typography
            sx={{ fontFamily: t.body, fontSize: '0.9rem', color: t.muted, lineHeight: 1.55 }}
          >
            The fixed engineering rules that govern how every system built on this platform runs —
            communication topology, closed layering, durable-execution substrate, generated clients,
            git-as-state storage, and the system-wide pricing policy. These are platform decisions,
            identical across all projects and not part of your ratifiable choices, so they are shown
            for reference only.
          </Typography>
          <Typography
            sx={{ mt: 0.75, fontFamily: t.mono, fontSize: 11, color: t.muted, lineHeight: 1.5 }}
          >
            Lives in the{' '}
            <Box component="span" sx={{ color: t.ink }}>
              platform-runtime-doctrine
            </Box>{' '}
            method asset (platform-owned, published alongside the Method skills).
          </Typography>
        </Box>
      ) : null}
    </Box>
  );
}

/** Customer-summary copy for a per-project selection (PM spec §6). */
function scenarioSummary(scenario: string): string {
  return scenario === 'deployedOperated'
    ? 'We build, host, and operate your system for you.'
    : 'We build and hand off your system — you run it on your own infrastructure.';
}

function venueSummary(kind: string, host: string): string {
  const where =
    kind === 'localMachine'
      ? 'your own machine'
      : host.length > 0
        ? `your CI (${host})`
        : 'your CI';
  return `Your code is built on ${where}, on your own account.`;
}

function reviewSummary(ref: string): string {
  const base =
    ref === 'vibes'
      ? 'Oversight is fully automatic — we draft and commit each step for you.'
      : ref === 'checkpoints'
        ? 'Oversight pauses for you at key checkpoints.'
        : ref === 'full'
          ? 'A human approves every gate.'
          : `Oversight: ${ref}.`;
  return `${base} High-risk changes always get a human.`;
}

function scalingSummary(scaleToZero: boolean): string {
  return `We scale your app to demand${scaleToZero ? ', down to zero when it is idle' : ''}.`;
}

export function OperationalConceptsView({
  envelope,
  height = 520,
}: {
  envelope: ArtifactModelEnvelope | undefined;
  height?: number;
}): ReactNode {
  const t = useTokens();
  const params = useParams({ strict: false });
  const projectId = typeof params.projectId === 'string' ? params.projectId : '';
  const { data: project } = useProject(projectId);

  // The committed System artifact, joined for instance layer/name colouring.
  const systemEnvelope = useMemo(
    () => project?.slots.find((s) => s.kind === 'system')?.model,
    [project]
  );

  // The committed Mission artifact, joined so each knob's objectiveLinks chips
  // can carry the objective statement as tooltip (same head-state join as above).
  const missionObjectives: Objective[] = useMemo(() => {
    const missionEnvelope = project?.slots.find((s) => s.kind === 'mission')?.model;
    return toMissionView(missionEnvelope).objectives ?? [];
  }, [project]);

  // The deployment diagram's live health overlay (Task 12, spec D10). Gated on
  // the operations capability (D9 — the local profile has no operations surface
  // to query). There is no committed projectId <-> operatedAppId correlation
  // anywhere in the system yet — RegisterOperatedApp has zero production callers
  // today (see task-11-report.md's identical finding for the Operations console
  // route) — so inventing one here would repeat the exact
  // fail-open-guard-keyed-to-nothing-wired pattern this plan's history has
  // already rejected twice. Until a future task wires real operated-app
  // registration and a way to look up its ID from a project, this stays empty:
  // useDeploymentHealth's own `enabled` guard keeps the query from ever firing,
  // and the diagram renders exactly as it does today.
  const operatedAppId = '';
  const capabilities = useCapabilities();
  const healthQuery = useDeploymentHealth(operatedAppId, operationsEnabled(capabilities));
  const healthByKey: Record<string, HealthState> = healthQuery.data ?? {};

  const model = useMemo(() => toDeploymentOperationsView(envelope), [envelope]);

  const profiles = useMemo(() => listDeploymentProfiles(envelope), [envelope]);
  const [profile, setProfile] = useState<DeploymentProfile | ''>(viewMemory.profile);
  const activeProfile =
    profile !== '' && profiles.some((p) => p.profile === profile) ? profile : profiles[0]?.profile;
  const chooseProfile = (next: DeploymentProfile): void => {
    viewMemory.profile = next;
    setProfile(next);
  };

  if (model === undefined) {
    return (
      <Typography sx={{ fontFamily: t.mono, fontSize: 12.5, color: t.muted }}>
        No deployment &amp; operations model drafted yet.
      </Typography>
    );
  }

  const scaling = model.scalingPolicy;
  const infra = model.infraBuildingBlocks;
  // Per-knob objectiveLinks joins ([] when the committed state predates the field).
  const links: ObjectiveLinks = model.objectiveLinks;
  const objFor = (knob: DeploymentKnob): LinkedObjective[] =>
    linkedObjectives(links, knob, missionObjectives);
  const trust = [
    { key: 'billing' as const, label: 'Billing', text: model.trustSummaries.billing },
    {
      key: 'usageMetering' as const,
      label: 'Usage metering',
      text: model.trustSummaries.usageMetering,
    },
    {
      key: 'dataOwnership' as const,
      label: 'Data ownership',
      text: model.trustSummaries.dataOwnership,
    },
  ];

  return (
    <Box>
      {/* (a) Ratifiable per-project selections. */}
      <Box data-testid={UI_IDENTIFIERS.Deployment.KNOBS}>
        <SectionLabel>Your deployment &amp; operations choices</SectionLabel>
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.25 }}>
          <KnobCard
            detail={`Scenario flag: ${model.deploymentScenario}. Governs which operate/bill components instantiate; the component graph is identical across scenarios.`}
            field="deploymentScenario"
            label="Deployment scenario"
            objectives={objFor('deploymentScenario')}
            summary={scenarioSummary(model.deploymentScenario)}
          />
          <KnobCard
            detail={`Venue: ${model.constructionVenue.kind}${
              model.constructionVenue.repositoryHost.length > 0
                ? ` · ${model.constructionVenue.repositoryHost}`
                : ''
            }. Dispatched behind the agenticJobAccess verbs — switching venue never changes a Manager.${
              model.constructionVenue.note.length > 0 ? ` ${model.constructionVenue.note}` : ''
            }`}
            field="constructionVenue"
            label="Construction venue"
            objectives={objFor('constructionVenue')}
            summary={venueSummary(
              model.constructionVenue.kind,
              model.constructionVenue.repositoryHost
            )}
          />
          <KnobCard
            detail={`Preset: ${model.reviewPolicyRef}. Routes which construction phases require a human sign-off; the high-risk floor is platform-fixed and non-overridable.`}
            field="reviewPolicyRef"
            label="Review policy"
            objectives={objFor('reviewPolicyRef')}
            summary={reviewSummary(model.reviewPolicyRef)}
          />
          {scaling !== undefined ? (
            <KnobCard
              detail={`min ${String(scaling.minInstances)} · max ${String(scaling.maxInstances)} instances · target ${String(scaling.targetUtilizationPct)}% utilization · scale-to-zero ${scaling.scaleToZero ? 'on' : 'off'}. AutoscalerEngine tunables.`}
              field="scalingPolicy"
              label="Scaling"
              objectives={objFor('scalingPolicy')}
              summary={scalingSummary(scaling.scaleToZero)}
            />
          ) : null}
        </Box>
      </Box>

      {/* (b) Customer trust summaries. */}
      <Box data-testid={UI_IDENTIFIERS.Deployment.TRUST} sx={{ mt: 4 }}>
        <SectionLabel>What we promise you</SectionLabel>
        <CommentableList
          ariaLabel="Trust summaries"
          gap={0.5}
          getAnchor={(item) => ({
            kind: 'node',
            label: item.label,
            source: `Deployment & Operations · ${item.label}`,
            jsonPath: trustSummaryAnchor(item.key),
          })}
          getKey={(item) => item.key}
          getLabel={(item) => item.label}
          getLabelKind={() => 'promise'}
          items={trust}
          renderItem={(item) => (
            <Box>
              <Typography
                sx={{
                  fontFamily: t.mono,
                  fontSize: 10,
                  fontWeight: 700,
                  letterSpacing: '0.1em',
                  textTransform: 'uppercase',
                  color: t.accent2,
                  mb: 0.3,
                }}
              >
                {item.label}
              </Typography>
              <Typography
                sx={{ fontFamily: t.body, fontSize: '0.95rem', color: t.ink, lineHeight: 1.5 }}
              >
                {item.text}
              </Typography>
            </Box>
          )}
        />
      </Box>

      {/* (c) Supported infrastructure building blocks. */}
      {infra.length > 0 && (
        <Box data-testid={UI_IDENTIFIERS.Deployment.INFRA} sx={{ mt: 4 }}>
          <SectionLabel>Supported building blocks</SectionLabel>
          {/* The infraBuildingBlocks knob links objectives as a WHOLE (the wire map
              keys knob names, not individual blocks), so its chips sit once under
              the section label rather than per block row. */}
          {objFor('infraBuildingBlocks').length > 0 && (
            <Box sx={{ mt: -0.6, mb: 1.25 }}>
              <ObjectiveChips chips={objFor('infraBuildingBlocks')} knob="infraBuildingBlocks" />
            </Box>
          )}
          <CommentableList
            ariaLabel="Infrastructure building blocks"
            getAnchor={(item, i) => ({
              kind: 'node',
              label: item.name,
              source: `Deployment & Operations · ${item.name}`,
              jsonPath: infraBlockAnchor(i),
            })}
            getKey={(item, i) => item.name || `infra-${String(i)}`}
            getLabel={(item) => item.name}
            getLabelKind={() => 'building block'}
            items={infra}
            renderItem={(item) => (
              <Box sx={{ display: 'flex', gap: 1, alignItems: 'baseline', flexWrap: 'wrap' }}>
                <Typography
                  component="span"
                  sx={{ fontFamily: t.mono, fontSize: 12.5, fontWeight: 700, color: t.ink }}
                >
                  {item.name}
                </Typography>
                <Typography
                  component="span"
                  sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}
                >
                  {item.category}
                </Typography>
                {item.status.length > 0 && item.status !== 'active' && (
                  <Box
                    component="span"
                    sx={{
                      px: 0.6,
                      py: 0.1,
                      borderRadius: 1,
                      border: `1px solid ${t.line}`,
                      fontFamily: t.mono,
                      fontSize: 9.5,
                      textTransform: 'uppercase',
                      letterSpacing: '0.06em',
                      color: t.muted,
                    }}
                  >
                    {item.status}
                  </Box>
                )}
              </Box>
            )}
          />
        </Box>
      )}

      {/* (d) The deployment topology per profile. */}
      <Box sx={{ mt: 4 }}>
        <SectionLabel>Deployment</SectionLabel>
        {profiles.length === 0 ? (
          <Typography sx={{ fontFamily: t.mono, fontSize: 12.5, color: t.muted }}>
            No deployment topology.
          </Typography>
        ) : (
          <>
            <ToggleButtonGroup
              exclusive
              color="primary"
              data-testid={UI_IDENTIFIERS.Deployment.PROFILE_SWITCH}
              size="small"
              sx={{ mb: 1.5 }}
              value={activeProfile ?? ''}
              onChange={(_e, next: DeploymentProfile | null) => {
                if (next !== null) chooseProfile(next);
              }}
            >
              {profiles.map((p) => (
                <ToggleButton key={p.profile} sx={{ fontFamily: t.mono }} value={p.profile}>
                  {p.title.length > 0 ? p.title : PROFILE_LABEL[p.profile]}
                </ToggleButton>
              ))}
            </ToggleButtonGroup>

            {activeProfile !== undefined && (
              <DeploymentFlow
                healthByKey={healthByKey}
                height={height}
                opEnvelope={envelope}
                profile={activeProfile}
                systemEnvelope={systemEnvelope}
              />
            )}
          </>
        )}
      </Box>

      {/* (e) Read-only platform doctrine reference. */}
      <DoctrineReference />
    </Box>
  );
}
