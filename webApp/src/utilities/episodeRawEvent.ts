/**
 * Parsing/summary helpers over one episode TimelineEvent's `raw` payload — the
 * Claude Agent SDK stream-json line the server captured for that event.
 *
 * WIRE SHAPE (2026-08-02 review finding C1 — corrected from the original,
 * wrong "it's a JSON string" assumption): the server's `raw` field is
 * `*json.RawMessage`, and `json.RawMessage.MarshalJSON` writes its bytes
 * VERBATIM into the parent JSON. So `raw` on the wire is a nested JSON OBJECT —
 * `{"seq":1,"eventType":"assistant","raw":{"type":"assistant","message":{...}}}` —
 * never a JSON-encoded string, and `omitempty` means it is simply ABSENT (not
 * present-as-null) when there is no payload. See
 * server/internal/manager/construction/constructionmanager.go's
 * `episodeTimelineEvents` (one full stream-json trace line per event, carried
 * through verbatim) for the server-side ground truth. `parseRawEvent` below
 * still accepts a JSON-encoded STRING too, defensively, in case a future
 * transport re-encodes it — but the object path is the one the real wire
 * exercises today, and is what the fixture tests
 * (episodeRawEvent.test.ts) assert against.
 *
 * PARENT_TOOL_USE_ID (2026-08-02 review finding C2): the CLI emits this as an
 * EXPLICIT `null` on virtually every main-loop event — not merely absent.
 * `parentToolUseId` below null-checks properly (`== null` covers both `null`
 * and `undefined`) rather than assuming "present and truthy or the key isn't
 * there at all".
 *
 * Grounded in the miner's own documented event shape,
 * server/internal/resourceaccess/agenticjob/agenticjobaccess.go's
 * streamEvent/streamMessage/streamContentBlock structs, and verified against
 * real captured lines in
 * server/internal/resourceaccess/agenticjob/testdata/streamjson/
 * {success_with_tools,success_with_subagent}.jsonl (see this module's test).
 *
 * Lives in `utilities` (not colocated in the `EpisodeTimeline` component) so a
 * `node --test` unit can import it directly without crossing the
 * components-layer boundary (eslint.platform.config.js's layer DAG only lets
 * `components` import `utilities`, never the reverse).
 */

export interface StreamContentBlock {
  type?: string;
  name?: string;
  input?: unknown;
  text?: string;
}

export interface StreamUsage {
  input_tokens?: number;
  output_tokens?: number;
  cache_read_input_tokens?: number;
  cache_creation_input_tokens?: number;
}

export interface StreamMessage {
  /** The turn identity — several assistant events share one id and repeat that
   *  turn's CUMULATIVE usage (see `eventMessageId`'s doc comment). */
  id?: string;
  model?: string;
  usage?: StreamUsage;
  content?: StreamContentBlock[] | string;
}

export interface StreamEvent {
  parent_tool_use_id?: string | null;
  message?: StreamMessage;
  usage?: StreamUsage;
}

/**
 * Normalizes a TimelineEvent's `raw` payload (typed `unknown` at the app
 * boundary — see contracts/types.ts) to the parsed StreamEvent. Accepts EITHER
 * shape the wire can carry it in: a JSON object (the real shape —
 * `json.RawMessage` embeds verbatim) used directly, or a JSON-encoded string
 * (defensive fallback) parsed with `JSON.parse`. Never throws; an
 * unparseable/unexpected/absent payload degrades to `undefined` rather than
 * crashing the timeline.
 */
export function parseRawEvent(raw: unknown): StreamEvent | undefined {
  if (raw === null || raw === undefined) return undefined;
  // `StreamEvent`'s fields are all optional, so any narrowed `object` is
  // structurally assignable to it already — no assertion needed; the
  // function's own return-type annotation is what does the (honest,
  // boundary-trust) widening for every caller, same as contracts/wire.ts's
  // mapTimelineEvent.
  if (typeof raw === 'object') return raw;
  if (typeof raw === 'string' && raw.length > 0) {
    try {
      const parsed: unknown = JSON.parse(raw);
      return typeof parsed === 'object' && parsed !== null ? parsed : undefined;
    } catch {
      return undefined;
    }
  }
  return undefined;
}

export function toolUseBlocks(parsed: StreamEvent | undefined): StreamContentBlock[] {
  const content = parsed?.message?.content;
  if (!Array.isArray(content)) return [];
  return content.filter((b) => b.type === 'tool_use');
}

export function eventUsage(parsed: StreamEvent | undefined): StreamUsage | undefined {
  return parsed?.message?.usage ?? parsed?.usage;
}

/**
 * The turn identity (`message.id`) a per-turn usage reading belongs to.
 * stream-json emits SEVERAL assistant events for a single turn (one per
 * content block — a text block, then the tool_use block, …), each carrying
 * the SAME message.id and the SAME CUMULATIVE usage for that turn, not a
 * delta (see the miner's own doc comment in
 * server/internal/resourceaccess/agenticjob/agenticjobaccess.go). Callers use
 * this to dedupe/label per-turn usage rather than presenting it as if summing
 * across events were correct.
 */
export function eventMessageId(parsed: StreamEvent | undefined): string | undefined {
  return parsed?.message?.id;
}

/**
 * The parent_tool_use_id, null-safe (2026-08-02 review finding C2): the wire
 * emits this as an explicit `null` on virtually every main-loop event, not
 * merely absent — `== null` below covers both `null` and `undefined`, and an
 * empty string is likewise treated as "no parent".
 */
export function parentToolUseId(parsed: StreamEvent | undefined): string | undefined {
  const id = parsed?.parent_tool_use_id;
  return id == null || id.length === 0 ? undefined : id;
}

const MAX_ARG_SUMMARY_CHARS = 160;
const MAX_VALUE_PREVIEW_CHARS = 60;

/**
 * Keys whose values are safe to preview verbatim — path/name/id-like metadata,
 * never file contents. Everything else (2026-08-02 review minor (a): the
 * original version previewed 40 chars of ANY string value, which leaked file
 * content for Write/Edit's `content`/`old_string`/`new_string` — exactly what
 * the "never render full file contents" rule forbids) renders as just the key
 * plus a length/type hint.
 */
const PREVIEWABLE_KEYS = new Set([
  'file_path',
  'path',
  'name',
  'command',
  'pattern',
  'url',
  'id',
  'description',
  'subagent_type',
  'query',
]);

function truncate(value: string, max: number): string {
  return value.length > max ? `${value.slice(0, max)}…` : value;
}

function valuePreview(key: string, value: unknown): string {
  if (!PREVIEWABLE_KEYS.has(key)) {
    if (typeof value === 'string') return `<${String(value.length)} chars>`;
    if (Array.isArray(value)) return `<${String(value.length)} items>`;
    return `<${typeof value}>`;
  }
  if (typeof value === 'string') return truncate(value, MAX_VALUE_PREVIEW_CHARS);
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  return typeof value;
}

/**
 * A metadata-only summary of a tool_use block's input — key names, with
 * previewed values ONLY for path/name/id-like keys (never content-bearing
 * ones), truncated overall. Never the full args object.
 */
export function argsSummary(input: unknown): string {
  if (input === undefined || input === null) return '';
  if (typeof input === 'string')
    return truncate(`<${String(input.length)} chars>`, MAX_ARG_SUMMARY_CHARS);
  if (typeof input === 'number' || typeof input === 'boolean') {
    return truncate(String(input), MAX_ARG_SUMMARY_CHARS);
  }
  if (typeof input !== 'object') return typeof input;
  const entries = Object.entries(input as Record<string, unknown>);
  const summary = entries.map(([k, v]) => `${k}=${valuePreview(k, v)}`).join(', ');
  return truncate(summary, MAX_ARG_SUMMARY_CHARS);
}
