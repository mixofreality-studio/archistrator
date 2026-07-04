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
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import IconButton from '@mui/material/IconButton';
import Tooltip from '@mui/material/Tooltip';
import ChatBubbleOutlineIcon from '@mui/icons-material/ChatBubbleOutline';
import { toMissionView } from '../contracts/adapters';
import type { ArtifactModelEnvelope } from '../contracts/types';
import { CommentableList } from './comments/CommentableList';
import { useComments, missionObjectiveAnchor, missionProseAnchor } from './comments/CommentContext';
import { useTokens } from '../utilities/theme/ThemeContext';

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
  const { setAnchor } = useComments();
  return (
    <Box sx={{ mb: 3.5 }}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1.5 }}>
        <SectionHeading>{heading}</SectionHeading>
        <Tooltip title={`Comment on ${heading}`}>
          <IconButton
            aria-label={`Comment on ${heading}`}
            size="small"
            sx={{
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
      </Box>
      <Typography
        data-artifact-kind="mission"
        data-commentable={section}
        sx={{ fontSize: '0.98rem', lineHeight: 1.65, color: t.ink, fontFamily: t.body }}
      >
        {text}
      </Typography>
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
                <Typography
                  component="span"
                  sx={{ color: t.ink, fontFamily: t.body, lineHeight: 1.6 }}
                >
                  {o.statement}
                </Typography>
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
