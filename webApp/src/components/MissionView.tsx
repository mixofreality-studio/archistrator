/**
 * The Mission artifact: Vision + Mission Statement as genuine narrative prose,
 * Business Objectives as a keyboard-navigable, item-commentable list.
 *
 * Vision and Mission Statement are the ONLY surfaces in the app that keep the
 * drag-select-to-quote affordance (they are continuous prose, not enumerable
 * items) — each carries `data-commentable`/`data-artifact-kind` so SelectionPopover
 * arms a section anchor, clamped to that block — AND a per-section "Comment on
 * this" button so a reviewer who wants to comment on the whole paragraph doesn't
 * have to select text. Business Objectives render through CommentableList with a
 * per-objective button anchoring `$.objectives[n]`. Bound to adapters.toMissionView.
 *
 * Ch.-5 traceability (reverse join): when the committed Deployment & Operations
 * Model carries objectiveLinks, each objective row gains a quiet "realized by:"
 * chip line naming the knobs that cite its number — chip-links to the Deployment
 * & Operations step. The committed slot arrives via CommittedSlotsContext (the
 * cross-slot delivery channel); objectives no knob cites render nothing extra.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import IconButton from '@mui/material/IconButton';
import Tooltip from '@mui/material/Tooltip';
import ChatBubbleOutlineIcon from '@mui/icons-material/ChatBubbleOutline';
import { toDeploymentOperationsView, toMissionView } from '../contracts/adapters';
import { KNOB_LABELS, realizingKnobs, type ObjectiveLinks } from '../contracts/deploymentOpsLogic';
import type { ArtifactModelEnvelope } from '../contracts/types';
import { CommentableList } from './comments/CommentableList';
import { useComments, missionObjectiveAnchor, missionProseAnchor } from './comments/CommentContext';
import { useCommittedSlotEnvelope } from './CommittedSlotsContext';
import { StepLink } from './shared/StepLink';
import { useTokens } from '../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';

function SectionHeading({ children }: { children: ReactNode }): ReactNode {
  const t = useTokens();
  return (
    <Typography
      sx={{
        fontFamily: t.display,
        fontWeight: 800,
        fontSize: '1.5rem',
        letterSpacing: '-0.015em',
        mb: 1.5,
      }}
    >
      {children}
    </Typography>
  );
}

/** A narrative-prose section: heading + text (clamped drag-select) + a section button. */
function ProseSection({
  heading,
  text,
  section,
}: {
  heading: string;
  text: string;
  section: 'vision' | 'mission';
}): ReactNode {
  const t = useTokens();
  const { setAnchor, enabled, anchor: armedAnchor, comments } = useComments();
  // Reveal the section's comment button exactly like CommentableList reveals a row's:
  // hidden at rest, shown on hover / keyboard focus-within of the CONTENT ROW (the prose
  // paragraph + its button — NOT the title), and kept persistently visible when this
  // section is the armed anchor or already carries a comment. Opacity-only (never
  // display/visibility) so the button stays in the tab order + a11y tree.
  const thisPath = missionProseAnchor(section);
  const isArmed = armedAnchor?.jsonPath === thisPath;
  const hasComments = comments.some((c) => c.anchor?.jsonPath === thisPath);
  const revealed = isArmed || hasComments;
  return (
    <Box sx={{ mb: 3.5 }}>
      <SectionHeading>{heading}</SectionHeading>
      {/* Content row — mirrors a CommentableList row: the prose grows on the left, the
          comment button sits at the top-right, revealed only when the CONTENT (not the
          title) is hovered / focus-within / armed / already commented. */}
      <Box
        sx={{
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
        <Typography
          // Comment-anchoring markers only when commenting is active — on a read-only
          // surface the prose carries no comment scaffolding at all.
          data-artifact-kind={enabled ? 'mission' : undefined}
          data-commentable={enabled ? section : undefined}
          sx={{
            flexGrow: 1,
            minWidth: 0,
            fontSize: '0.98rem',
            lineHeight: 1.65,
            color: t.ink,
            fontFamily: t.body,
          }}
        >
          {text}
        </Typography>
        {enabled ? (
          <Tooltip title={`Comment on ${heading}`}>
            <IconButton
              aria-label={`Comment on ${heading}`}
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
                  label: heading,
                  source: `Mission · ${heading}`,
                  jsonPath: missionProseAnchor(section),
                });
              }}
            >
              <ChatBubbleOutlineIcon sx={{ fontSize: 15 }} />
            </IconButton>
          </Tooltip>
        ) : null}
      </Box>
    </Box>
  );
}

/**
 * The quiet "realized by" line under one objective: the Deployment & Operations
 * knobs whose objectiveLinks cite this objective's number, as chip-links to that
 * step. Nothing renders when no knob cites it (coverage is the server's job) or
 * when the committed state predates objectiveLinks.
 */
function RealizedBy({ objNumber, links }: { objNumber: number; links: ObjectiveLinks }): ReactNode {
  const t = useTokens();
  const knobs = realizingKnobs(links, objNumber);
  if (knobs.length === 0) return null;
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Mission.realizedBy(objNumber)}
      sx={{ display: 'flex', flexWrap: 'wrap', alignItems: 'baseline', gap: 0.6, mt: 0.5 }}
    >
      <Typography
        component="span"
        sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted, letterSpacing: '0.04em' }}
      >
        realized by:
      </Typography>
      {knobs.map((knob) => (
        <StepLink
          key={knob}
          kind="operationalConcepts"
          label={`${KNOB_LABELS[knob]} — realizes objective ${String(objNumber)}`}
          sx={{
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
          testId={UI_IDENTIFIERS.Mission.realizedLink(objNumber, knob)}
          underline="none"
        >
          {KNOB_LABELS[knob]}
        </StepLink>
      ))}
    </Box>
  );
}

export function MissionView({
  envelope,
}: {
  envelope: ArtifactModelEnvelope | undefined;
}): ReactNode {
  const t = useTokens();
  const { vision, objectives, mission } = toMissionView(envelope);
  const objs = objectives ?? [];

  // The committed Deployment & Operations slot (cross-slot delivery channel) —
  // its objectiveLinks drive the per-objective "realized by" reverse join.
  // Undefined without a provider / before that slot commits → rows render bare.
  const depOpsEnvelope = useCommittedSlotEnvelope('operationalConcepts');
  const objectiveLinks = toDeploymentOperationsView(depOpsEnvelope)?.objectiveLinks;

  if (vision === '' && objs.length === 0 && mission === '') {
    return (
      <Box sx={{ py: 6, textAlign: 'center', color: t.muted, fontFamily: t.mono }}>
        No mission drafted yet.
      </Box>
    );
  }

  return (
    <Box>
      {vision !== '' && <ProseSection heading="Vision" section="vision" text={vision} />}

      {objs.length > 0 && (
        <Box sx={{ mb: 3.5 }}>
          <SectionHeading>Business Objectives</SectionHeading>
          <CommentableList
            ariaLabel="Business objectives"
            getAnchor={(o, i) => ({
              kind: 'node',
              label: `Objective ${String(o.number)}`,
              source: `Mission · objective ${String(o.number)}`,
              jsonPath: missionObjectiveAnchor(i),
            })}
            getKey={(o, i) => `obj-${String(o.number)}-${String(i)}`}
            getLabel={(o) => `objective ${String(o.number)}`}
            items={objs}
            renderItem={(o) => (
              <Box sx={{ display: 'flex', gap: 1.25, alignItems: 'baseline' }}>
                <Typography
                  component="span"
                  sx={{
                    fontFamily: t.mono,
                    fontSize: 12,
                    fontWeight: 700,
                    color: t.accent2,
                    flexShrink: 0,
                  }}
                >
                  {o.number}.
                </Typography>
                <Box sx={{ minWidth: 0 }}>
                  <Typography
                    component="span"
                    sx={{ color: t.ink, fontFamily: t.body, lineHeight: 1.6 }}
                  >
                    {o.statement}
                  </Typography>
                  <RealizedBy links={objectiveLinks} objNumber={o.number} />
                </Box>
              </Box>
            )}
          />
        </Box>
      )}

      {mission !== '' && (
        <ProseSection heading="Mission Statement" section="mission" text={mission} />
      )}
    </Box>
  );
}
