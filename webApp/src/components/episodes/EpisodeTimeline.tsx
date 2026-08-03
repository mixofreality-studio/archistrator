/**
 * Pure, props-only per-episode timeline — row-click target from EpisodesPanel.
 * Renders: a filter-by-event-type Select (dropdown per the repo's UI selection
 * convention — dynamic counts get a dropdown, not tabs), then one row per
 * (filtered) TimelineEvent: per-turn tokens when the event carries usage,
 * tool_use rows (tool name + a truncated, metadata-only args summary — NEVER
 * the full args/file contents), and a subagent-span marker when the event ran
 * under a parent_tool_use_id.
 *
 * Gap episodes (outcome === 'gap') come back with a VALID timeline whose
 * `events` array is empty — this renders the gap state (gapReason) instead of
 * an empty-looking panel; a non-gap episode with zero events (e.g. a very
 * early cancellation) gets an honest "no events recorded" notice instead of a
 * fabricated gap reason.
 *
 * `raw` is `json.RawMessage` on the wire, represented as `raw?: string | null`
 * on the app TimelineEvent (see contracts/types.ts / contracts/wire.ts) — every
 * read here null-checks before JSON.parse and never throws on malformed JSON
 * (the CLI's stream-json shape is TOLERANT by design; the miner comment in
 * server/internal/resourceaccess/agenticjob/agenticjobaccess.go documents the
 * exact event shape this parses).
 */
import { useMemo, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import FormControl from '@mui/material/FormControl';
import Select from '@mui/material/Select';
import MenuItem from '@mui/material/MenuItem';
import CircularProgress from '@mui/material/CircularProgress';
import type { EpisodeTimeline as EpisodeTimelineModel, TimelineEvent } from '../../contracts/types';
import { useTokens } from '../../utilities/theme/ThemeContext';
import type { Tokens } from '../../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

const ALL_EVENT_TYPES = '__all__';
const MAX_ARG_SUMMARY_CHARS = 160;

// ---------------------------------------------------------------------------
// stream-json raw-event parsing (tolerant — see file header)
// ---------------------------------------------------------------------------

interface StreamContentBlock {
  type?: string;
  name?: string;
  input?: unknown;
  text?: string;
}

interface StreamUsage {
  input_tokens?: number;
  output_tokens?: number;
  cache_read_input_tokens?: number;
  cache_creation_input_tokens?: number;
}

interface StreamMessage {
  usage?: StreamUsage;
  content?: StreamContentBlock[] | string;
}

interface StreamEvent {
  parent_tool_use_id?: string;
  message?: StreamMessage;
  usage?: StreamUsage;
}

function parseRaw(raw: string | null | undefined): StreamEvent | undefined {
  if (raw === null || raw === undefined || raw.length === 0) return undefined;
  try {
    // JSON.parse is `any`-typed; every downstream read is optional-chained, so
    // an unexpected shape (or a bare primitive) degrades to "no fields found"
    // rather than a crash — this cast is the one honest boundary trust here.
    return JSON.parse(raw) as StreamEvent;
  } catch {
    return undefined;
  }
}

function toolUseBlocks(parsed: StreamEvent | undefined): StreamContentBlock[] {
  const content = parsed?.message?.content;
  if (!Array.isArray(content)) return [];
  return content.filter((b) => b.type === 'tool_use');
}

function eventUsage(parsed: StreamEvent | undefined): StreamUsage | undefined {
  return parsed?.message?.usage ?? parsed?.usage;
}

function truncate(value: string, max: number): string {
  return value.length > max ? `${value.slice(0, max)}…` : value;
}

/** A metadata-only summary of a tool_use block's input — key names + short
 *  scalar previews, truncated. Never the full args object (which can carry
 *  full file contents for Write/Edit). */
function argsSummary(input: unknown): string {
  if (input === undefined || input === null) return '';
  if (typeof input === 'string') return truncate(input, MAX_ARG_SUMMARY_CHARS);
  if (typeof input === 'number' || typeof input === 'boolean') {
    return truncate(String(input), MAX_ARG_SUMMARY_CHARS);
  }
  if (typeof input !== 'object') return typeof input;
  const entries = Object.entries(input as Record<string, unknown>);
  const summary = entries
    .map(([k, v]) => {
      const preview = typeof v === 'string' ? truncate(v, 40) : typeof v;
      return `${k}=${preview}`;
    })
    .join(', ');
  return truncate(summary, MAX_ARG_SUMMARY_CHARS);
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export interface EpisodeTimelineProps {
  timeline: EpisodeTimelineModel | undefined;
  loading: boolean;
}

export function EpisodeTimeline({ timeline, loading }: EpisodeTimelineProps): ReactNode {
  const t = useTokens();
  const [filter, setFilter] = useState<string>(ALL_EVENT_TYPES);

  const events = useMemo(() => timeline?.events ?? [], [timeline]);
  const eventTypes = useMemo(
    () => [...new Set(events.map((e) => e.eventType))].sort((a, b) => a.localeCompare(b)),
    [events]
  );
  const filtered =
    filter === ALL_EVENT_TYPES ? events : events.filter((e) => e.eventType === filter);

  if (loading) {
    return (
      <Box
        data-testid={UI_IDENTIFIERS.Episodes.TIMELINE}
        sx={{ display: 'flex', alignItems: 'center', gap: 1, p: 2 }}
      >
        <CircularProgress size={16} />
        <Typography sx={{ fontFamily: t.mono, fontSize: 11.5, color: t.muted }}>
          loading timeline…
        </Typography>
      </Box>
    );
  }

  if (timeline === undefined) {
    return null;
  }

  const record = timeline.record;
  const isGap = events.length === 0 && record.outcome === 'gap';
  const isHonestEmpty = events.length === 0 && !isGap;

  return (
    <Paper
      data-testid={UI_IDENTIFIERS.Episodes.TIMELINE}
      sx={{ p: 1.5, display: 'flex', flexDirection: 'column', gap: 1, bgcolor: t.paperAlt }}
    >
      {isGap ? (
        <Box sx={{ p: 1.5, border: `1.5px dashed ${t.line}`, borderRadius: 1, bgcolor: t.paper }}>
          <Typography
            sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12, color: t.awaitingFg }}
          >
            GAP — no episode ran
          </Typography>
          <Typography sx={{ fontFamily: t.body, fontSize: 12.5, color: t.muted, mt: 0.5 }}>
            {record.gapReason !== undefined && record.gapReason.length > 0
              ? record.gapReason
              : 'The stream stopped before the CLI reported how it ended — no terminal event was ever observed for this episode.'}
          </Typography>
        </Box>
      ) : isHonestEmpty ? (
        <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted, p: 1 }}>
          no events recorded for this episode.
        </Typography>
      ) : (
        <>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <Typography
              sx={{ fontFamily: t.mono, fontSize: 10, letterSpacing: '0.08em', color: t.muted }}
            >
              EVENT TYPE
            </Typography>
            <FormControl size="small" sx={{ minWidth: 160 }}>
              <Select
                data-testid={UI_IDENTIFIERS.Episodes.TIMELINE_FILTER}
                sx={{ fontFamily: t.mono, fontSize: 12 }}
                value={filter}
                onChange={(e) => {
                  setFilter(e.target.value);
                }}
              >
                <MenuItem sx={{ fontFamily: t.mono, fontSize: 12 }} value={ALL_EVENT_TYPES}>
                  all ({String(events.length)})
                </MenuItem>
                {eventTypes.map((et) => (
                  <MenuItem key={et} sx={{ fontFamily: t.mono, fontSize: 12 }} value={et}>
                    {et} ({String(events.filter((e) => e.eventType === et).length)})
                  </MenuItem>
                ))}
              </Select>
            </FormControl>
            <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}>
              {String(filtered.length)} of {String(events.length)} events
            </Typography>
          </Box>

          <Box
            sx={{
              display: 'flex',
              flexDirection: 'column',
              gap: 0.5,
              maxHeight: 360,
              overflowY: 'auto',
            }}
          >
            {filtered.map((event) => (
              <TimelineRow event={event} key={event.seq} t={t} />
            ))}
          </Box>
        </>
      )}
    </Paper>
  );
}

// ---------------------------------------------------------------------------
// One row
// ---------------------------------------------------------------------------

function TimelineRow({ event, t }: { event: TimelineEvent; t: Tokens }): ReactNode {
  const parsed = parseRaw(event.raw);
  const tools = toolUseBlocks(parsed);
  const usage = eventUsage(parsed);
  const parentToolUseId = parsed?.parent_tool_use_id;

  return (
    <Box
      sx={{
        display: 'flex',
        flexDirection: 'column',
        gap: 0.35,
        px: 1,
        py: 0.6,
        borderRadius: 1,
        border: `1px solid ${t.line}`,
        bgcolor: t.paper,
      }}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, flexWrap: 'wrap' }}>
        <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.muted, minWidth: 24 }}>
          #{event.seq}
        </Typography>
        <Chip
          label={event.eventType}
          size="small"
          sx={{
            height: 16,
            fontSize: 9,
            fontFamily: t.mono,
            bgcolor: t.chatArchitectBg,
            color: t.chatArchitectFg,
          }}
        />
        {parentToolUseId !== undefined && parentToolUseId.length > 0 && (
          <Chip
            label="subagent span"
            size="small"
            sx={{
              height: 16,
              fontSize: 9,
              fontFamily: t.mono,
              bgcolor: t.chatPmBg,
              color: t.chatPmFg,
            }}
          />
        )}
        {usage !== undefined && (
          <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.muted }}>
            in {String(usage.input_tokens ?? 0)} · out {String(usage.output_tokens ?? 0)}
          </Typography>
        )}
      </Box>
      {tools.map((tool, i) => (
        <Box key={`${String(event.seq)}-${String(i)}`} sx={{ pl: 3.5 }}>
          <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, fontWeight: 700, color: t.ink }}>
            tool_use · {tool.name ?? 'unknown'}
          </Typography>
          <Typography
            sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.muted, wordBreak: 'break-word' }}
          >
            {argsSummary(tool.input)}
          </Typography>
        </Box>
      ))}
    </Box>
  );
}
