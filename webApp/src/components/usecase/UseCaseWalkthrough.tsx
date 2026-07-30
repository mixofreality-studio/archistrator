/**
 * Choose-your-path walkthrough of a use case's activity diagram. One node is in
 * focus at a time (large + readable — which also sidesteps the fit-to-view
 * shrink that makes the whole-graph view illegible); the reader advances with a
 * "Next" step, and at every decision/fork the outgoing edges become branch
 * buttons (labeled by their guard) so the reader picks the path. The route taken
 * is a breadcrumb (click to rewind) and is mirrored live onto the activity
 * diagram beside it as a "you-are-here" map (current node ringed, path
 * emphasized, rest dimmed). Bound to a UseCaseView (adapters.toCoreUseCasesView).
 */
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import Chip from '@mui/material/Chip';
import IconButton from '@mui/material/IconButton';
import Tooltip from '@mui/material/Tooltip';
import ArrowForwardIcon from '@mui/icons-material/ArrowForward';
import ChatBubbleOutlineIcon from '@mui/icons-material/ChatBubbleOutline';
import UndoIcon from '@mui/icons-material/Undo';
import RestartAltIcon from '@mui/icons-material/RestartAlt';
import FlagIcon from '@mui/icons-material/Flag';
import type { UseCaseView } from '../../contracts/adapters';
import { ActivityFlow, type ActivityHighlight } from './ActivityFlow';
import { useComments, activityNodeAnchor } from '../comments/CommentContext';
import { laneColors } from './laneColors';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

type NodeView = UseCaseView['nodes'][number];
type EdgeView = UseCaseView['edges'][number];

/** Human header for a node kind. Actions have no header — their label speaks. */
const KIND_HEADER: Record<string, string> = {
  start: 'Start',
  end: 'End',
  merge: 'Paths merge',
  fork: 'Parallel split',
  join: 'Parallel join',
  decision: 'Decision',
  switch: 'Decision',
};

/** Fallback text when a node (start/end/merge) carries no label of its own. */
function nodeText(n: NodeView): string {
  if (n.label.trim().length > 0) return n.label;
  return KIND_HEADER[n.kind] ?? n.kind;
}

/** A branch button's label: the guard (brackets stripped) or the destination. */
function branchLabel(e: EdgeView, nodesById: Map<string, NodeView>): string {
  const g = e.guard.replace(/[[\]]/g, '').trim();
  if (g.length > 0) return g;
  const tgt = nodesById.get(e.to);
  return tgt !== undefined ? nodeText(tgt) : 'this path';
}

export function UseCaseWalkthrough({
  uc,
  useCaseIndex,
  height = 580,
}: {
  uc: UseCaseView;
  useCaseIndex: number;
  height?: number;
}): ReactNode {
  const t = useTokens();
  const colors = laneColors(t, uc.lanes);
  const { setAnchor, enabled, anchor: armedAnchor, comments } = useComments();

  const { nodesById, outEdges, startId } = useMemo(() => {
    const byId = new Map<string, NodeView>(uc.nodes.map((n) => [n.id, n]));
    const outs = new Map<string, EdgeView[]>();
    for (const e of uc.edges) {
      const arr = outs.get(e.from) ?? [];
      arr.push(e);
      outs.set(e.from, arr);
    }
    const start = uc.nodes.find((n) => n.kind === 'start')?.id ?? uc.nodes[0]?.id ?? '';
    return { nodesById: byId, outEdges: outs, startId: start };
  }, [uc]);

  const [path, setPath] = useState<string[]>(startId.length > 0 ? [startId] : []);

  // Container-aware layout: MUI viewport breakpoints can't see that this widget
  // lives inside a narrow hero (a 300px meta sidebar already eats the row), which
  // is what squeezed the you-are-here map down to a ~100px strip with clipped
  // text. Measure our *own* width and, when the row can't give the map a usable
  // share, stack the map full-width beneath the controls instead.
  const rootRef = useRef<HTMLDivElement>(null);
  // Focus target after every step change: the controls that were clicked (Next /
  // a branch / Back / Restart) can unmount or disable on advance, which would
  // silently drop keyboard focus to <body>. The step title is the stable landing
  // spot, and its role="status" live region announces the new step to AT.
  const stepTitleRef = useRef<HTMLElement>(null);
  const [wide, setWide] = useState(true);
  useEffect(() => {
    const el = rootRef.current;
    if (el === null) return undefined;
    const ro = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (entry !== undefined) setWide(entry.contentRect.width >= 780);
    });
    ro.observe(el);
    return (): void => {
      ro.disconnect();
    };
  }, []);

  const highlight: ActivityHighlight = useMemo(() => {
    const visitedEdges = new Set<string>();
    for (let k = 0; k < path.length - 1; k++) {
      visitedEdges.add(`${path[k] ?? ''}-${path[k + 1] ?? ''}`);
    }
    return {
      current: path[path.length - 1] ?? '',
      visitedNodes: new Set(path),
      visitedEdges,
    };
  }, [path]);

  if (uc.nodes.length === 0) {
    return (
      <Box sx={{ py: 6, textAlign: 'center', color: t.muted, fontFamily: t.mono }}>
        This use case has no activity diagram to walk through.
      </Box>
    );
  }

  const currentId = path[path.length - 1] ?? startId;
  const node = nodesById.get(currentId);
  const outs = outEdges.get(currentId) ?? [];
  const kindHeader = node !== undefined ? KIND_HEADER[node.kind] : undefined;
  const isBranch = outs.length > 1;
  const isEnd = outs.length === 0;

  // Per-step comment button, revealed like a CommentableList row's: hidden at rest,
  // shown on hover / keyboard focus-within of the step card, and pinned visible when
  // this step is the armed anchor or already carries a comment. It arms the SAME
  // anchor as the card's drag-select-to-quote path (activityNodeAnchor on the current
  // node) so a reviewer needn't select text to comment on the whole step.
  const stepAnchorPath = activityNodeAnchor(useCaseIndex, currentId);
  const stepArmed = armedAnchor?.jsonPath === stepAnchorPath;
  const stepHasComments = comments.some((c) => c.anchor?.jsonPath === stepAnchorPath);
  const stepRevealed = stepArmed || stepHasComments;

  const advance = (toId: string): void => {
    setPath((p) => [...p, toId]);
    stepTitleRef.current?.focus();
  };

  return (
    <Box ref={rootRef} sx={{ display: 'flex', gap: 2, flexDirection: wide ? 'row' : 'column' }}>
      {/* focused current step + controls */}
      <Box
        sx={{
          width: wide ? 380 : '100%',
          flexShrink: 0,
          display: 'flex',
          flexDirection: 'column',
          gap: 1.5,
        }}
      >
        {/* data-commentable + data-comment-anchor: highlighting text in this card
            arms the SAME anchor as clicking the current node on the diagram. */}
        <Paper
          data-artifact-kind="coreUseCases"
          data-comment-anchor={stepAnchorPath}
          data-commentable={`${uc.name} · activity diagram`}
          sx={{
            p: 2.5,
            display: 'flex',
            flexDirection: 'column',
            gap: 1.5,
            minHeight: 210,
            '& .walkthrough-step-action': {
              opacity: stepRevealed ? 1 : 0,
              transition: 'opacity 120ms',
            },
            '&:hover .walkthrough-step-action, &:focus-within .walkthrough-step-action': {
              opacity: 1,
            },
            '@media (hover: none)': { '& .walkthrough-step-action': { opacity: 1 } },
          }}
        >
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <Typography
              sx={{
                fontFamily: t.mono,
                fontSize: 11,
                letterSpacing: '0.08em',
                textTransform: 'uppercase',
                color: t.muted,
              }}
            >
              {kindHeader !== undefined ? `${kindHeader} · ` : ''}step {path.length}
            </Typography>
            <Box sx={{ ml: 'auto', display: 'flex', alignItems: 'center', gap: 1 }}>
              {node !== undefined && (
                <Chip
                  label={node.lane}
                  size="small"
                  sx={{
                    height: 20,
                    fontFamily: t.mono,
                    fontSize: 10,
                    bgcolor: t.paperAlt,
                    borderLeft: `4px solid ${colors[node.lane] ?? t.muted}`,
                    borderRadius: 0.5,
                  }}
                />
              )}
              {enabled ? (
                <Tooltip title={`Comment on step ${String(path.length)}`}>
                  <IconButton
                    aria-label={`Comment on step ${String(path.length)}`}
                    className="walkthrough-step-action"
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
                        label: node !== undefined ? nodeText(node) : `Step ${String(path.length)}`,
                        source: `${uc.name} · activity diagram`,
                        jsonPath: stepAnchorPath,
                      });
                    }}
                  >
                    <ChatBubbleOutlineIcon sx={{ fontSize: 15 }} />
                  </IconButton>
                </Tooltip>
              ) : null}
            </Box>
          </Box>

          <Typography
            aria-live="polite"
            ref={stepTitleRef}
            role="status"
            sx={{
              fontFamily: t.body,
              fontWeight: 700,
              fontSize: 20,
              lineHeight: 1.25,
              color: t.ink,
            }}
            tabIndex={-1}
          >
            {node !== undefined ? nodeText(node) : '—'}
          </Typography>

          {/* controls */}
          {isEnd ? (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mt: 'auto' }}>
              <FlagIcon sx={{ fontSize: 18, color: t.committedDot }} />
              <Typography sx={{ color: t.muted, fontSize: 13, fontFamily: t.mono }}>
                End of this path.
              </Typography>
            </Box>
          ) : isBranch ? (
            <Box sx={{ mt: 'auto', display: 'flex', flexDirection: 'column', gap: 0.75 }}>
              <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}>
                Which branch?
              </Typography>
              {outs.map((e) => {
                const tgt = nodesById.get(e.to);
                return (
                  <Button
                    data-testid={UI_IDENTIFIERS.UseCaseCarousel.walkthroughBranch(
                      `${e.from}-${e.to}`
                    )}
                    key={`${e.from}-${e.to}`}
                    sx={{
                      justifyContent: 'flex-start',
                      textAlign: 'left',
                      textTransform: 'none',
                      color: t.ink,
                      borderColor: t.line,
                      '&:hover': { borderColor: t.accent, bgcolor: t.paperAlt },
                    }}
                    variant="outlined"
                    onClick={() => {
                      advance(e.to);
                    }}
                  >
                    <Box>
                      <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 13 }}>
                        {branchLabel(e, nodesById)}
                      </Typography>
                      <Typography sx={{ fontSize: 11, color: t.muted }}>
                        → {tgt !== undefined ? nodeText(tgt) : e.to}
                      </Typography>
                    </Box>
                  </Button>
                );
              })}
            </Box>
          ) : (
            <Button
              data-testid={UI_IDENTIFIERS.UseCaseCarousel.WALKTHROUGH_NEXT}
              endIcon={<ArrowForwardIcon />}
              sx={{ mt: 'auto', alignSelf: 'flex-start', textTransform: 'none' }}
              variant="contained"
              onClick={() => {
                const next = outs[0];
                if (next !== undefined) advance(next.to);
              }}
            >
              Next
            </Button>
          )}
        </Paper>

        {/* nav: back / restart */}
        <Box sx={{ display: 'flex', gap: 1 }}>
          <Button
            data-testid={UI_IDENTIFIERS.UseCaseCarousel.WALKTHROUGH_BACK}
            disabled={path.length <= 1}
            size="small"
            startIcon={<UndoIcon sx={{ fontSize: 15 }} />}
            sx={{ color: t.ink, textTransform: 'none' }}
            onClick={() => {
              setPath((p) => (p.length > 1 ? p.slice(0, -1) : p));
              stepTitleRef.current?.focus();
            }}
          >
            Back
          </Button>
          <Button
            data-testid={UI_IDENTIFIERS.UseCaseCarousel.WALKTHROUGH_RESTART}
            disabled={path.length <= 1}
            size="small"
            startIcon={<RestartAltIcon sx={{ fontSize: 15 }} />}
            sx={{ color: t.ink, textTransform: 'none' }}
            onClick={() => {
              setPath(startId.length > 0 ? [startId] : []);
              stepTitleRef.current?.focus();
            }}
          >
            Restart
          </Button>
        </Box>

        {/* breadcrumb trail (click a step to rewind) */}
        <Paper sx={{ p: 1.5, bgcolor: t.paperAlt }}>
          <Typography
            sx={{
              fontFamily: t.mono,
              fontSize: 10,
              letterSpacing: '0.08em',
              textTransform: 'uppercase',
              color: t.muted,
              mb: 0.75,
            }}
          >
            Path
          </Typography>
          <Box sx={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 0.5 }}>
            {path.map((id, idx) => {
              const n = nodesById.get(id);
              const last = idx === path.length - 1;
              return (
                <Box
                  key={`${id}-${String(idx)}`}
                  sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}
                >
                  {idx > 0 && <Typography sx={{ color: t.muted, fontSize: 11 }}>→</Typography>}
                  <Typography
                    data-testid={UI_IDENTIFIERS.UseCaseCarousel.walkthroughPathStep(idx)}
                    role="button"
                    sx={{
                      fontFamily: t.mono,
                      fontSize: 11,
                      cursor: 'pointer',
                      color: last ? t.accent : t.muted,
                      fontWeight: last ? 700 : 400,
                      '&:hover': { color: t.ink },
                    }}
                    tabIndex={0}
                    onClick={() => {
                      setPath((p) => p.slice(0, idx + 1));
                    }}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault();
                        setPath((p) => p.slice(0, idx + 1));
                      }
                    }}
                  >
                    {n !== undefined ? nodeText(n) : id}
                  </Typography>
                </Box>
              );
            })}
          </Box>
        </Paper>
      </Box>

      {/* live "you-are-here" map */}
      <Box sx={{ flexGrow: 1, minWidth: 0 }}>
        <ActivityFlow height={height} highlight={highlight} uc={uc} useCaseIndex={useCaseIndex} />
      </Box>
    </Box>
  );
}
