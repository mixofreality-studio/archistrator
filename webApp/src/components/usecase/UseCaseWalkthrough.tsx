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
import { useMemo, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import Chip from '@mui/material/Chip';
import ArrowForwardIcon from '@mui/icons-material/ArrowForward';
import UndoIcon from '@mui/icons-material/Undo';
import RestartAltIcon from '@mui/icons-material/RestartAlt';
import FlagIcon from '@mui/icons-material/Flag';
import type { UseCaseView } from '../../contracts/adapters';
import { ActivityFlow, type ActivityHighlight } from './ActivityFlow';
import { laneColors } from './laneColors';
import { useTokens } from '../../utilities/theme/ThemeContext';

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

  const advance = (toId: string): void => {
    setPath((p) => [...p, toId]);
  };

  return (
    <Box sx={{ display: 'flex', gap: 2, flexDirection: { xs: 'column', md: 'row' } }}>
      {/* focused current step + controls */}
      <Box
        sx={{
          width: { xs: '100%', md: 400 },
          flexShrink: 0,
          display: 'flex',
          flexDirection: 'column',
          gap: 1.5,
        }}
      >
        <Paper sx={{ p: 2.5, display: 'flex', flexDirection: 'column', gap: 1.5, minHeight: 210 }}>
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
            {node !== undefined && (
              <Chip
                label={node.lane}
                size="small"
                sx={{
                  ml: 'auto',
                  height: 20,
                  fontFamily: t.mono,
                  fontSize: 10,
                  bgcolor: t.paperAlt,
                  borderLeft: `4px solid ${colors[node.lane] ?? t.muted}`,
                  borderRadius: 0.5,
                }}
              />
            )}
          </Box>

          <Typography
            sx={{
              fontFamily: t.body,
              fontWeight: 700,
              fontSize: 20,
              lineHeight: 1.25,
              color: t.ink,
            }}
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
            disabled={path.length <= 1}
            size="small"
            startIcon={<UndoIcon sx={{ fontSize: 15 }} />}
            sx={{ color: t.ink, textTransform: 'none' }}
            onClick={() => {
              setPath((p) => (p.length > 1 ? p.slice(0, -1) : p));
            }}
          >
            Back
          </Button>
          <Button
            disabled={path.length <= 1}
            size="small"
            startIcon={<RestartAltIcon sx={{ fontSize: 15 }} />}
            sx={{ color: t.ink, textTransform: 'none' }}
            onClick={() => {
              setPath(startId.length > 0 ? [startId] : []);
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
