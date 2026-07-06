/**
 * The right-column activity detail for the Artifacts tab. Renders the activity
 * header (id, kind badge, status chip, name, phase), lifecycle strip (construction
 * phase + produced/pending counts), and ArtifactCard list for every ProducedArtifactRow.
 * Deep views (the mock's ServiceContractView / DesignLoop / TestingArtifactView)
 * are honest pointers only — title + source + open-in-corpus note — NOT the mock's
 * fabricated interactive content.
 *
 * Ported from ux-mock ArtifactsTab.tsx ActivityHeader + LifecycleStrip + ArtifactCard,
 * bound to real ConstructionRow / ProducedArtifactRow data.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Chip from '@mui/material/Chip';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import CheckCircleIcon from '@mui/icons-material/CheckCircle';
import RadioButtonUncheckedIcon from '@mui/icons-material/RadioButtonUnchecked';
import type { Tokens } from '../../utilities/theme/themes';
import type {
  ArtifactModelEnvelope,
  ConstructionRow,
  ProducedArtifactRow,
  ProjectStateWithGit,
} from '../../contracts/types';
import type { BuildStatus } from '../../contracts/constructionAdapters';
import { contractForActivity } from '../../contracts/serviceContracts';
import { StatusChip } from './status';
import { KindBadge, KIND_META } from './KindBadge';
import { ServiceContractView } from './ServiceContractView';
import type { ArtifactActivityVM } from './ArtifactActivityList';
import { classify } from './artifactClassification';
import { artifactRenderers } from './artifactRenderers';

// ---------------------------------------------------------------------------
// ActivityHeader
// ---------------------------------------------------------------------------

function ActivityHeader({ t, vm }: { t: Tokens; vm: ArtifactActivityVM }): ReactNode {
  const status = vm.row.status as BuildStatus;
  return (
    <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1.5, flexWrap: 'wrap' }}>
      <Box sx={{ minWidth: 0 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, flexWrap: 'wrap' }}>
          <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.accent }}>
            {vm.activityId}
          </Typography>
          <KindBadge kind={vm.row.kind} t={t} />
          <StatusChip size="xs" status={status} t={t} />
        </Box>
        <Typography
          sx={{
            fontFamily: t.display,
            fontWeight: 800,
            fontSize: 24,
            color: t.ink,
            lineHeight: 1.15,
            mt: 0.25,
          }}
        >
          {vm.name}
        </Typography>
        <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}>
          phase · {vm.row.phase}
        </Typography>
      </Box>
    </Box>
  );
}

// ---------------------------------------------------------------------------
// LifecycleStrip
// ---------------------------------------------------------------------------

/**
 * Derived lifecycle phases — MONOTONIC by the status ordinal. The build status is
 * the authority: an `integrated` activity shows every prior phase done; `in-review`
 * shows Designed+Built done; `in-construction` shows Designed done. Artifact counts
 * may only ENRICH (mark an earlier phase done sooner) — they can never contradict the
 * ordinal by leaving Designed/Built pending while Reviewed/Integrated read done.
 */
function lifecyclePhases(row: ConstructionRow): { name: string; done: boolean }[] {
  const produced = row.produced ?? [];
  const hasAny = produced.length > 0;
  const hasProduced = produced.some((a) => a.produced);
  const kindLabel = KIND_META[row.kind].label;
  const prefix = kindLabel[0] ?? '?';

  // Status ordinal: in-construction(0) < in-review(1) < integrated(2).
  const ord = row.status === 'integrated' ? 2 : row.status === 'in-review' ? 1 : 0;

  return [
    // Designed: any construction status implies design is done; artifacts enrich.
    { name: `${prefix}:Designed`, done: ord >= 0 || hasAny },
    // Built: code complete at the review gate (ord>=1); a produced artifact enriches.
    { name: `${prefix}:Built`, done: ord >= 1 || hasProduced },
    // Reviewed / Integrated: gated purely on the ordinal — never on artifact counts.
    { name: `${prefix}:Reviewed`, done: ord >= 2 },
    { name: `${prefix}:Integrated`, done: ord >= 2 },
  ];
}

function PhaseChip({
  active,
  done,
  name,
  t,
}: {
  active: boolean;
  done: boolean;
  name: string;
  t: Tokens;
}): ReactNode {
  return (
    <Box
      sx={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 0.35,
        px: 0.6,
        py: 0.2,
        borderRadius: t.radius / 8 + 0.5,
        bgcolor: done ? t.committedBg : active ? t.awaitingBg : 'transparent',
        border: `1.5px solid ${done ? t.committedDot : active ? t.accent : t.line}`,
      }}
    >
      {done ? (
        <CheckCircleIcon sx={{ fontSize: 12, color: t.committedDot }} />
      ) : (
        <RadioButtonUncheckedIcon sx={{ fontSize: 12, color: active ? t.accent : t.muted }} />
      )}
      <Typography
        sx={{
          fontFamily: t.mono,
          fontSize: 9.5,
          fontWeight: 700,
          color: done ? t.committedFg : active ? t.awaitingFg : t.muted,
        }}
      >
        {name}
      </Typography>
    </Box>
  );
}

function LifecycleStrip({ t, vm }: { t: Tokens; vm: ArtifactActivityVM }): ReactNode {
  const phases = lifecyclePhases(vm.row);
  const produced = vm.row.produced ?? [];
  const doneCount = produced.filter((a) => a.produced).length;
  const totalCount = produced.length;

  return (
    <Paper sx={{ p: 1.5 }}>
      <Typography
        sx={{
          fontFamily: t.mono,
          fontSize: 9.5,
          letterSpacing: '0.08em',
          color: t.muted,
          mb: 0.75,
        }}
      >
        {KIND_META[vm.row.kind].label.toUpperCase()} LIFE CYCLE · {doneCount}/{totalCount} artifacts
        produced
      </Typography>
      <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.5, alignItems: 'center' }}>
        {phases.map((p, i) => (
          <Box key={p.name} sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
            <PhaseChip
              active={!p.done && i === phases.findIndex((x) => !x.done)}
              done={p.done}
              name={p.name}
              t={t}
            />
            {i < phases.length - 1 ? (
              <Typography sx={{ color: t.muted, fontSize: 10 }}>›</Typography>
            ) : null}
          </Box>
        ))}
      </Box>
    </Paper>
  );
}

// ---------------------------------------------------------------------------
// ArtifactCard
// ---------------------------------------------------------------------------

function ArtifactCard({ art, t }: { art: ProducedArtifactRow; t: Tokens }): ReactNode {
  return (
    <Paper sx={{ p: 1.5, borderLeft: `4px solid ${art.produced ? t.committedDot : t.line}` }}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
        <Chip
          label={art.kind}
          size="small"
          sx={{ height: 18, fontSize: 8.5, bgcolor: t.chatArchitectBg, color: t.chatArchitectFg }}
        />
        <Typography sx={{ fontFamily: t.body, fontWeight: 700, fontSize: 13, color: t.ink }}>
          {art.title}
        </Typography>
        <Box sx={{ flexGrow: 1 }} />
        <Chip
          label={art.produced ? 'PRODUCED' : 'NOT YET'}
          size="small"
          sx={{
            height: 18,
            fontSize: 8.5,
            color: art.produced ? t.committedFg : t.muted,
            bgcolor: art.produced ? t.committedBg : 'transparent',
            border: `1px solid ${art.produced ? t.committedDot : t.line}`,
          }}
        />
      </Box>
      {/* source as mono sub-label */}
      {art.source.length > 0 ? (
        <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.muted, mt: 0.3 }}>
          {art.source}
        </Typography>
      ) : null}
      {/* note */}
      {art.note.length > 0 ? (
        <Typography
          sx={{ fontFamily: t.body, fontSize: 12, color: t.ink, lineHeight: 1.45, mt: 0.4 }}
        >
          {art.note}
        </Typography>
      ) : null}
      {/* honest pointer to deeper content — not fabricated interactive views */}
      {art.produced ? (
        <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.muted, mt: 0.5 }}>
          Open in corpus · artifact available at source above once the corpus access port lands.
        </Typography>
      ) : null}
    </Paper>
  );
}

// ---------------------------------------------------------------------------
// ArtifactActivityDetail (public export)
// ---------------------------------------------------------------------------

export function ArtifactActivityDetail({
  project,
  systemEnvelope,
  t,
  vm,
}: {
  project?: ProjectStateWithGit | undefined;
  systemEnvelope?: ArtifactModelEnvelope | undefined;
  t: Tokens;
  vm: ArtifactActivityVM;
}): ReactNode {
  const artifacts = vm.row.produced ?? [];

  // Resolve the contract for any activity — a Client (or any other kind) that
  // exposes a service contract should render it just as a SERVICE kind does.
  const contract = contractForActivity(project, vm.activityId);

  // For the artifact card list: when a contract resolved, skip the service-contract
  // artifact (ServiceContractView renders it richly above). Split code artifacts
  // from the rest so we can group them under a distinct heading.
  const nonContractArtifacts =
    contract !== undefined ? artifacts.filter((a) => a.kind !== 'service-contract') : artifacts;

  const codeArtifacts = nonContractArtifacts.filter((a) => a.kind === 'code');
  const otherArtifacts = nonContractArtifacts.filter((a) => a.kind !== 'code');

  // Per-type renderer dispatch: a registered renderer for this activity's
  // classification takes over the body; otherwise the generic contract view +
  // honest-pointer cards below. (Contract-bearing types keep the service path.)
  const Renderer = artifactRenderers[classify(vm.row)];

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, minWidth: 0 }}>
      <ActivityHeader t={t} vm={vm} />
      <LifecycleStrip t={t} vm={vm} />

      {Renderer !== undefined ? (
        <Renderer project={project} systemEnvelope={systemEnvelope} t={t} vm={vm} />
      ) : (
        <>
          {/* Primary artifact: the rich ServiceContractView for any contract-bearing activity */}
          {contract !== undefined ? (
            <>
              <Typography
                sx={{
                  fontFamily: t.mono,
                  fontWeight: 700,
                  fontSize: 11,
                  letterSpacing: '0.06em',
                  color: t.ink,
                }}
              >
                SERVICE CONTRACT
              </Typography>
              <ServiceContractView contract={contract} systemEnvelope={systemEnvelope} />
            </>
          ) : null}

          {/* CODE & INTEGRATION STATUS — code artifacts grouped under their own heading */}
          {codeArtifacts.length > 0 ? (
            <>
              <Typography
                sx={{
                  fontFamily: t.mono,
                  fontWeight: 700,
                  fontSize: 11,
                  letterSpacing: '0.06em',
                  color: t.ink,
                }}
              >
                {`CODE & INTEGRATION STATUS · ${String(codeArtifacts.filter((a) => a.produced).length)} of ${String(codeArtifacts.length)}`}
              </Typography>
              {codeArtifacts.map((art, i) => (
                <ArtifactCard art={art} key={`${art.title}-${String(i)}`} t={t} />
              ))}
            </>
          ) : contract !== undefined ? (
            <Typography
              sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted, fontStyle: 'italic' }}
            >
              No code artifacts recorded for this activity yet.
            </Typography>
          ) : null}

          {/* Remaining produced artifact cards (non-code, non-service-contract) */}
          {otherArtifacts.length > 0 ? (
            <>
              <Typography
                sx={{
                  fontFamily: t.mono,
                  fontWeight: 700,
                  fontSize: 11,
                  letterSpacing: '0.06em',
                  color: t.ink,
                }}
              >
                {`PRODUCED ARTIFACTS · ${String(otherArtifacts.filter((a) => a.produced).length)} of ${String(otherArtifacts.length)}`}
              </Typography>
              {otherArtifacts.map((art, i) => (
                <ArtifactCard art={art} key={`${art.title}-${String(i)}`} t={t} />
              ))}
            </>
          ) : null}

          {/* No-artifact state only when there's no contract view and nothing else */}
          {nonContractArtifacts.length === 0 && contract === undefined ? (
            <Typography sx={{ color: t.muted, fontSize: 12.5 }}>
              No artifacts recorded yet for this activity.
            </Typography>
          ) : null}
        </>
      )}
    </Box>
  );
}
