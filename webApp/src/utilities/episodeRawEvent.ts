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
  /** tool_result-specific (one block of a "user" message's content array —
   *  see summarizeEvent's 'user' case / testdata's success_with_tools.jsonl
   *  line 8 and success_with_subagent.jsonl line 15). `content` on a
   *  tool_result block is EITHER a plain string OR an array of these same
   *  {type:'text', text} blocks — both shapes appear in the real fixtures. */
  tool_use_id?: string;
  is_error?: boolean;
  content?: string | StreamContentBlock[];
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
  /** Present on `system` events (init/hook_started/task_notification/…) and on
   *  the terminal `result` event (e.g. "success"). */
  subtype?: string;
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

// ---------------------------------------------------------------------------
// Per-event-type one-line summaries (2026-08-02 founder UI review: non-
// assistant rows rendered as bare chips — "system", "rate_limit_event" — with
// zero descriptive content). Every field path below is grounded in the real
// fixtures at server/internal/resourceaccess/agenticjob/testdata/streamjson/
// {success_with_tools,success_with_subagent,failure}.jsonl (see this module's
// test for line numbers), not inferred — that gap is exactly what C1/C2 bit
// us on last round.
// ---------------------------------------------------------------------------

/** Loosely-typed view of a raw event/sub-object for the generic scalar-key
 *  fallback below — StreamEvent only names the fields the rest of this module
 *  reads structurally; the fallback deliberately inspects whatever else is
 *  there, so it works over the raw JSON object rather than the narrow
 *  interface. */
type RawRecord = Record<string, unknown>;

function asRecord(value: unknown): RawRecord {
  return typeof value === 'object' && value !== null ? (value as RawRecord) : {};
}

function isScalar(value: unknown): value is string | number | boolean {
  return typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean';
}

function scalarPreview(value: string | number | boolean, max = 50): string {
  return typeof value === 'string' ? truncate(value, max) : String(value);
}

/** Fields present on virtually every raw event line (or already surfaced
 *  elsewhere — the chip, the subagent-span badge) — noise for the generic
 *  fallback, so always excluded regardless of event type. */
const STRUCTURAL_KEYS = new Set([
  'type',
  'subtype',
  'uuid',
  'session_id',
  'parent_tool_use_id',
  'timestamp',
  'request_id',
]);

/**
 * The "other subtypes" generic fallback: up to `max` top-level scalar
 * (string/number/boolean) fields off a raw event object, in declaration
 * order, skipping structural/noise keys. Covers system subtypes with no
 * bespoke handling below (hook_started, hook_response, task_started,
 * task_updated, thinking_tokens, …) and any rate_limit_event whose shape
 * varies from the captured fixture. Individually truncated — "compact",
 * never a raw content dump (hook_response's `output`/`stdout` can be large;
 * declaration order alone keeps those from ever displacing the earlier,
 * genuinely-compact fields like hook_id/hook_name/hook_event within the
 * 3-key cap).
 */
function genericScalarSummary(raw: RawRecord, max = 3): string | undefined {
  const entries: [string, string | number | boolean][] = [];
  for (const [k, v] of Object.entries(raw)) {
    if (!STRUCTURAL_KEYS.has(k) && isScalar(v)) entries.push([k, v]);
    if (entries.length >= max) break;
  }
  if (entries.length === 0) return undefined;
  return entries.map(([k, v]) => `${k}=${scalarPreview(v)}`).join(' · ');
}

/**
 * `system` events: bespoke summaries for `init` (model, tool count, cwd — all
 * present on testdata line
 * success_with_subagent.jsonl:5/success_with_tools.jsonl:5) and
 * `task_notification` (usage.total_tokens is present on
 * success_with_subagent.jsonl:14; subagent_type is NOT present on that event
 * in the captured fixture — only on the sibling task_started/user-subagent-
 * turn events — so it's read defensively and simply omitted here, same as
 * the brief's own "if present" framing; `status` is included too since it's
 * real, present, and otherwise the row would show tokens with no outcome).
 * Every other subtype falls through to the generic scalar-key summary.
 */
function summarizeSystemEvent(raw: RawRecord): string | undefined {
  const subtype = typeof raw['subtype'] === 'string' ? raw['subtype'] : undefined;
  if (subtype === 'init') {
    const parts: string[] = [];
    const model = raw['model'];
    const tools = raw['tools'];
    const cwd = raw['cwd'];
    if (typeof model === 'string') parts.push(`model ${model}`);
    if (Array.isArray(tools)) parts.push(`${String(tools.length)} tools`);
    if (typeof cwd === 'string') parts.push(`cwd ${cwd}`);
    return parts.length > 0 ? parts.join(' · ') : undefined;
  }
  if (subtype === 'task_notification') {
    const parts: string[] = [];
    const status = raw['status'];
    const subagentType = raw['subagent_type'];
    if (typeof status === 'string') parts.push(status);
    if (typeof subagentType === 'string') parts.push(`subagent ${subagentType}`);
    const usage = asRecord(raw['usage']);
    const totalTokens = usage['total_tokens'];
    if (typeof totalTokens === 'number') {
      parts.push(`${totalTokens.toLocaleString()} tokens`);
    }
    return parts.length > 0 ? parts.join(' · ') : undefined;
  }
  return genericScalarSummary(raw);
}

/**
 * `rate_limit_event`: the meaningful fields live one level down, under
 * `rate_limit_info` (status/rateLimitType/overageStatus — present on every
 * captured fixture line, e.g. success_with_tools.jsonl:9). Falls back to a
 * generic scalar-key summary (of `rate_limit_info` if present, else the
 * top-level event) if that shape doesn't hold.
 */
function summarizeRateLimitEvent(raw: RawRecord): string | undefined {
  const info = asRecord(raw['rate_limit_info']);
  const parts: string[] = [];
  const status = info['status'];
  const rateLimitType = info['rateLimitType'];
  const overageStatus = info['overageStatus'];
  if (typeof status === 'string') parts.push(`status ${status}`);
  if (typeof rateLimitType === 'string') parts.push(rateLimitType);
  if (typeof overageStatus === 'string') parts.push(`overage ${overageStatus}`);
  if (parts.length > 0) return parts.join(' · ');
  return genericScalarSummary(Object.keys(info).length > 0 ? info : raw);
}

/** The character length of a tool_result block's `content` — a plain string
 *  (success_with_tools.jsonl:8) or an array of {type:'text', text} blocks
 *  (success_with_subagent.jsonl:15, the subagent's own result feeding back)
 *  — summed across any text blocks. Never returns the content itself. */
function toolResultContentLength(content: string | StreamContentBlock[] | undefined): number {
  if (typeof content === 'string') return content.length;
  if (Array.isArray(content)) {
    return content.reduce(
      (sum, block) => sum + (typeof block.text === 'string' ? block.text.length : 0),
      0
    );
  }
  return 0;
}

/**
 * `user` events: one summary per tool_result content block — "tool_result ·
 * ok|error · <n> chars". `is_error` is read defensively (absent → ok, matching
 * the real fixture, which omits it entirely on a successful result); never
 * renders the content itself, only its length.
 */
function summarizeUserEvent(parsed: StreamEvent | undefined): string | undefined {
  const content = parsed?.message?.content;
  if (!Array.isArray(content)) return undefined;
  const results = content.filter((b) => b.type === 'tool_result');
  if (results.length === 0) return undefined;
  return results
    .map((b) => {
      const len = toolResultContentLength(b.content);
      return `tool_result · ${b.is_error === true ? 'error' : 'ok'} · ${String(len)} chars`;
    })
    .join(' · ');
}

const MAX_TEXT_EXCERPT_CHARS = 80;

/**
 * `assistant` events with a text block and NO tool_use (a tool_use event
 * already gets its own dedicated name+args row below — see `toolUseBlocks`):
 * a truncated excerpt of the text (this is the local trace viewer; a ~80-char
 * ellipsized excerpt of the assistant's own words is not the "never render
 * tool args' full file contents" concern that gates `argsSummary`).
 */
function summarizeAssistantEvent(parsed: StreamEvent | undefined): string | undefined {
  if (toolUseBlocks(parsed).length > 0) return undefined;
  const content = parsed?.message?.content;
  if (!Array.isArray(content)) return undefined;
  const textBlock = content.find((b) => b.type === 'text' && typeof b.text === 'string');
  const text = textBlock?.text;
  return text !== undefined ? truncate(text, MAX_TEXT_EXCERPT_CHARS) : undefined;
}

function humanizeDurationMs(ms: number): string {
  if (ms < 1000) return `${String(Math.round(ms))}ms`;
  const seconds = ms / 1000;
  if (seconds < 60) return `${seconds.toFixed(1)}s`;
  return `${String(Math.floor(seconds / 60))}m ${String(Math.round(seconds % 60))}s`;
}

/**
 * `result` (the terminal event): duration_ms (humanized), subtype, and
 * is_error — appended alongside the existing turn-total usage readout rather
 * than replacing it (all three fields are present on every captured fixture's
 * result line, e.g. success_with_tools.jsonl:12).
 */
function summarizeResultEvent(raw: RawRecord): string | undefined {
  const parts: string[] = [];
  const subtype = raw['subtype'];
  const durationMs = raw['duration_ms'];
  if (typeof subtype === 'string') parts.push(subtype);
  if (typeof durationMs === 'number') parts.push(humanizeDurationMs(durationMs));
  if (raw['is_error'] === true) parts.push('ERROR');
  return parts.length > 0 ? parts.join(' · ') : undefined;
}

/**
 * The one-line descriptive summary for a TimelineEvent, dispatched by its
 * top-level `eventType`. Returns `undefined` for an event type with nothing
 * to add (an unparseable/undecodable raw payload, or an assistant tool_use
 * event already covered by its own dedicated row) — the caller renders
 * nothing extra in that case, i.e. "keep current behavior".
 */
export function summarizeEvent(
  eventType: string,
  parsed: StreamEvent | undefined
): string | undefined {
  const raw = asRecord(parsed);
  switch (eventType) {
    case 'system':
      return summarizeSystemEvent(raw);
    case 'rate_limit_event':
      return summarizeRateLimitEvent(raw);
    case 'user':
      return summarizeUserEvent(parsed);
    case 'assistant':
      return summarizeAssistantEvent(parsed);
    case 'result':
      return summarizeResultEvent(raw);
    default:
      return undefined;
  }
}

/**
 * The event-type Chip's label — "system · init", "system · hook_started", …
 * for `system` events (per the founder review: the subtype must be visible
 * without expanding anything), the plain `eventType` for everything else
 * (the `result` event's subtype/duration/is_error instead ride the
 * turn-totals line via `summarizeEvent`, not the chip).
 */
export function eventChipLabel(eventType: string, parsed: StreamEvent | undefined): string {
  if (eventType === 'system') {
    const subtype = parsed?.subtype;
    if (typeof subtype === 'string' && subtype.length > 0) return `${eventType} · ${subtype}`;
  }
  return eventType;
}
