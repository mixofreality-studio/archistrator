/**
 * Core Use Cases as a carousel — the activity diagram is the hero. Bound to
 * adapters.toCoreUseCasesView (UseCaseView[]). A compact meta sidebar (name,
 * classification, swim-lanes) flanks a large React-Flow activity diagram. A
 * labeled Select + prev/next page through the use cases (dynamic, project-state-
 * driven count — can exceed a dozen — so a dropdown replaces the old tab strip).
 * Recolored from tokens.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import IconButton from '@mui/material/IconButton';
import Button from '@mui/material/Button';
import Chip from '@mui/material/Chip';
import FormControl from '@mui/material/FormControl';
import InputLabel from '@mui/material/InputLabel';
import Select from '@mui/material/Select';
import MenuItem from '@mui/material/MenuItem';
import ListSubheader from '@mui/material/ListSubheader';
import ToggleButton from '@mui/material/ToggleButton';
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup';
import ChevronLeftIcon from '@mui/icons-material/ChevronLeft';
import ChevronRightIcon from '@mui/icons-material/ChevronRight';
import ChatBubbleOutlineIcon from '@mui/icons-material/ChatBubbleOutline';
import { toCoreUseCasesView } from '../../contracts/adapters';
import type { ArtifactModelEnvelope } from '../../contracts/types';
import { ActivityFlow } from './ActivityFlow';
import { UseCaseWalkthrough } from './UseCaseWalkthrough';
import { laneColors } from './laneColors';

// Diagram-view mode survives the design-experience remount that would otherwise
// reset it every render, so a reader who chose "walkthrough" stays there while
// paging use cases. Module-scoped (see the diagram-view remount convention).
type UcViewMode = 'walkthrough' | 'diagram';
const viewMemory: { mode: UcViewMode } = { mode: 'walkthrough' };
// Aliased away from a `use*` name so the react-hooks lint heuristic doesn't
// mistake this plain anchor builder for a React hook.
import { useComments, useCaseAnchor as buildUseCaseAnchor } from '../comments/CommentContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';
import { useTokens } from '../../utilities/theme/ThemeContext';
import type { Tokens } from '../../utilities/theme/themes';

export function UseCaseCarousel({
  envelope,
}: {
  envelope: ArtifactModelEnvelope | undefined;
}): ReactNode {
  const t = useTokens();
  const { setAnchor, enabled } = useComments();
  const [i, setI] = useState(0);
  const [mode, setMode] = useState<UcViewMode>(viewMemory.mode);
  const useCases = toCoreUseCasesView(envelope).useCases;

  if (useCases.length === 0) {
    return (
      <Box sx={{ py: 6, textAlign: 'center', color: t.muted, fontFamily: t.mono }}>
        No core use cases drafted yet.
      </Box>
    );
  }

  const active = Math.min(i, useCases.length - 1);
  const uc = useCases[active];
  if (uc === undefined) return null;
  const colors = laneColors(t, uc.lanes);
  const go = (d: number): void => {
    setI((p) => (p + d + useCases.length) % useCases.length);
  };
  const isCore = uc.classification === 'core';
  // A variation shares its parent's activity diagram; resolve the parent so the
  // no-diagram surface can name it and offer a jump instead of a generic blank.
  // Committed data carries the parent NAME (not the slug id), so match id OR exact
  // name — both survive until the data is normalized (A5).
  const parentIndex =
    uc.variationOf.length > 0
      ? useCases.findIndex((u) => u.id === uc.variationOf || u.name === uc.variationOf)
      : -1;
  const parent = parentIndex >= 0 ? useCases[parentIndex] : undefined;
  const hasDiagram = uc.nodes.length > 0;

  // Core / variation partition for the grouped picker + the summary line. Each entry
  // keeps its ORIGINAL index (the Select value) so grouping never breaks navigation.
  const coreItems = useCases
    .map((u, idx) => ({ u, idx }))
    .filter(({ u }) => u.classification === 'core');
  const variationItems = useCases
    .map((u, idx) => ({ u, idx }))
    .filter(({ u }) => u.classification !== 'core');
  const parentNameOf = (u: (typeof useCases)[number]): string | undefined => {
    if (u.variationOf.length === 0) return undefined;
    return useCases.find((p) => p.id === u.variationOf || p.name === u.variationOf)?.name;
  };

  return (
    <Box>
      {/* use-case picker */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1.5 }}>
        <FormControl size="small" sx={{ flexGrow: 1, minWidth: 0 }}>
          <InputLabel id="use-case-picker-label" sx={{ fontFamily: t.mono }}>
            Use case
          </InputLabel>
          <Select
            label="Use case"
            labelId="use-case-picker-label"
            renderValue={(idx) => useCases[idx]?.name ?? ''}
            sx={{ fontFamily: t.mono, fontSize: 13 }}
            value={active}
            onChange={(e) => {
              setI(e.target.value);
            }}
          >
            {/* Grouped so the core/variation distinction (lost in a 28-item flat list)
                is visible; variations name the core they derive from (A6). */}
            {coreItems.length > 0 && (
              <ListSubheader sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}>
                Core · {coreItems.length}
              </ListSubheader>
            )}
            {coreItems.map(({ u, idx }) => (
              <MenuItem key={u.id} sx={{ fontFamily: t.mono, fontSize: 13 }} value={idx}>
                {u.name}
              </MenuItem>
            ))}
            {variationItems.length > 0 && (
              <ListSubheader sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}>
                Variations · {variationItems.length}
              </ListSubheader>
            )}
            {variationItems.map(({ u, idx }) => {
              const pName = parentNameOf(u);
              return (
                <MenuItem key={u.id} sx={{ fontFamily: t.mono, fontSize: 13, display: 'block' }} value={idx}>
                  <Box>{u.name}</Box>
                  {pName !== undefined ? (
                    <Box
                      component="span"
                      sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}
                    >
                      variation of {pName}
                    </Box>
                  ) : null}
                </MenuItem>
              );
            })}
          </Select>
        </FormControl>
        <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted, flexShrink: 0 }}>
          {active + 1} / {useCases.length}
        </Typography>
        <IconButton
          aria-label="Previous use case"
          size="small"
          sx={{ border: `1.5px solid ${t.line}`, borderRadius: 1, color: t.ink }}
          onClick={() => {
            go(-1);
          }}
        >
          <ChevronLeftIcon fontSize="small" />
        </IconButton>
        <IconButton
          aria-label="Next use case"
          size="small"
          sx={{ border: `1.5px solid ${t.line}`, borderRadius: 1, color: t.ink }}
          onClick={() => {
            go(1);
          }}
        >
          <ChevronRightIcon fontSize="small" />
        </IconButton>
        {enabled ? (
          <IconButton
            aria-label={`Comment on use case ${uc.name}`}
            data-testid={UI_IDENTIFIERS.Comments.USECASE_COMMENT}
            size="small"
            sx={{
              border: `1.5px solid ${t.line}`,
              borderRadius: 1,
              color: t.accentText,
              bgcolor: t.accent,
              flexShrink: 0,
              '&:hover': { bgcolor: t.accent2 },
            }}
            onClick={() => {
              setAnchor({
                kind: 'node',
                label: uc.name,
                source: `${uc.name} · use case`,
                jsonPath: buildUseCaseAnchor(active),
              });
            }}
          >
            <ChatBubbleOutlineIcon fontSize="small" />
          </IconButton>
        ) : null}
      </Box>

      {/* Summary of the corpus makeup — surfaces the core/variation split the flat
          count alone hides (A6). */}
      <Typography sx={{ fontFamily: t.mono, fontSize: 11.5, color: t.muted, mb: 1.5 }}>
        {coreItems.length} core of {useCases.length} use case{useCases.length === 1 ? '' : 's'}
        {variationItems.length > 0 ? ` · ${String(variationItems.length)} variations` : ''}
      </Typography>

      <Paper
        sx={{
          display: 'flex',
          alignItems: 'stretch',
          overflow: 'hidden',
          flexDirection: { xs: 'column', md: 'row' },
        }}
      >
        {/* meta sidebar */}
        <Box
          sx={{
            width: { xs: '100%', md: 300 },
            flexShrink: 0,
            p: 3,
            borderRight: { md: `1.5px solid ${t.line}` },
            bgcolor: t.paperAlt,
          }}
        >
          <Typography sx={{ color: t.muted }} variant="overline">
            {isCore ? 'Core Use Case' : 'Variation'}
          </Typography>
          <Typography sx={{ color: t.ink, lineHeight: 1.1, mt: 0.5, mb: 1.5 }} variant="h4">
            {uc.name}
          </Typography>
          <Chip
            label={isCore ? 'CORE' : 'NON-CORE'}
            size="small"
            sx={{
              bgcolor: isCore ? t.committedBg : t.awaitingBg,
              color: isCore ? t.committedFg : t.awaitingFg,
              mb: 2,
            }}
          />
          {!isCore && uc.rejectionReason.length > 0 && (
            <Typography sx={{ color: t.muted, fontSize: 13, lineHeight: 1.6, mb: 3 }}>
              {uc.rejectionReason}
            </Typography>
          )}
          <Typography sx={{ color: t.muted, mb: 1 }} variant="subtitle2">
            SWIMLANES
          </Typography>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.75 }}>
            {uc.lanes.map((l) => (
              <Box key={l} sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
                <Box
                  sx={{
                    width: 11,
                    height: 11,
                    bgcolor: colors[l],
                    border: `1.5px solid ${t.line}`,
                    flexShrink: 0,
                  }}
                />
                <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.ink }}>{l}</Typography>
              </Box>
            ))}
          </Box>
        </Box>

        {/* hero: walkthrough (choose-your-path) or the full diagram. When this use
            case owns no diagram, both tabs would render divergent "no diagram"
            copy — so we short-circuit to one unified, variation-aware empty state. */}
        <Box sx={{ flexGrow: 1, minWidth: 0, p: 1.5 }}>
          {hasDiagram ? (
            <>
              <Box sx={{ display: 'flex', justifyContent: 'flex-end', mb: 1.5 }}>
                <ToggleButtonGroup
                  exclusive
                  aria-label="Use case view mode"
                  size="small"
                  value={mode}
                  onChange={(_e, next: UcViewMode | null) => {
                    if (next !== null) {
                      viewMemory.mode = next;
                      setMode(next);
                    }
                  }}
                >
                  <ToggleButton
                    sx={{ fontFamily: t.mono, fontSize: 11, textTransform: 'none' }}
                    value="walkthrough"
                  >
                    Walkthrough
                  </ToggleButton>
                  <ToggleButton
                    sx={{ fontFamily: t.mono, fontSize: 11, textTransform: 'none' }}
                    value="diagram"
                  >
                    Full diagram
                  </ToggleButton>
                </ToggleButtonGroup>
              </Box>
              {mode === 'walkthrough' ? (
                <UseCaseWalkthrough height={560} key={uc.id} uc={uc} useCaseIndex={active} />
              ) : (
                <ActivityFlow height={580} uc={uc} useCaseIndex={active} />
              )}
            </>
          ) : (
            <NoDiagram
              isVariation={!isCore}
              parentName={parent?.name}
              t={t}
              onJumpToParent={
                parent !== undefined
                  ? (): void => {
                      setI(parentIndex);
                    }
                  : undefined
              }
            />
          )}
        </Box>
      </Paper>
    </Box>
  );
}

/**
 * The single, unified no-activity-diagram surface for a use case, replacing the two
 * divergent inner empty states (ActivityFlow / UseCaseWalkthrough). For a VARIATION
 * it names the parent it shares a diagram with and offers a jump; otherwise it shows
 * one neutral "no diagram yet" message.
 */
function NoDiagram({
  t,
  isVariation = false,
  parentName,
  onJumpToParent,
}: {
  t: Tokens;
  /** This is a variation use case (non-core) — it reuses a core use case's diagram
   *  rather than owning one, so the empty state must not read as a missing-diagram
   *  defect. True even when the specific parent link (variationOf) is absent. */
  isVariation?: boolean;
  /** The named parent this variation shares a diagram with, when resolvable. */
  parentName?: string | undefined;
  /** Jump to the parent use case (present only when the parent is resolvable). */
  onJumpToParent?: (() => void) | undefined;
}): ReactNode {
  const hasParent = parentName !== undefined && parentName.length > 0;
  // Honest copy: an absent activity diagram is a not-yet state that validation flags,
  // NOT a normal "variations reuse the parent" resting state (A7). Every use case is
  // expected to get its own diagram; a variation may additionally link its parent's.
  const message = 'No activity diagram yet for this use case.';
  const secondary = hasParent
    ? `As a variation of ${parentName}, it’s expected to get its own diagram — until then, walk ${parentName}’s.`
    : isVariation
      ? 'This variation is expected to get its own activity diagram.'
      : 'It’s expected to get one.';
  return (
    <Box
      sx={{
        height: 560,
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        textAlign: 'center',
        gap: 1.5,
        px: 3,
        border: `1.5px dashed ${t.line}`,
        borderRadius: t.radius / 8 + 0.5,
        bgcolor: t.bg,
      }}
    >
      <Typography sx={{ fontFamily: t.mono, fontSize: 13, color: t.ink, lineHeight: 1.6 }}>
        {message}
      </Typography>
      <Typography sx={{ fontFamily: t.body, fontSize: 12.5, color: t.muted, maxWidth: 380 }}>
        {secondary}
      </Typography>
      {onJumpToParent !== undefined ? (
        <Button
          size="small"
          sx={{ color: t.ink, borderColor: t.line, textTransform: 'none' }}
          variant="outlined"
          onClick={onJumpToParent}
        >
          Go to {parentName}
        </Button>
      ) : null}
    </Box>
  );
}
