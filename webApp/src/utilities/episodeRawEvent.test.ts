/// <reference types="node" />
/**
 * Fixture-driven unit tests for episodeRawEvent.ts's stream-json parsing.
 *
 * The three constants below are REAL lines copied verbatim from
 * server/internal/resourceaccess/agenticjob/testdata/streamjson/
 * success_with_tools.jsonl and success_with_subagent.jsonl — the exact wire
 * content the server's agentic-job miner reads to build
 * EpisodeRecordView/TimelineEvent (see
 * server/internal/manager/construction/constructionmanager.go's
 * `episodeTimelineEvents`, which carries one such line through verbatim as
 * one TimelineEvent's `raw`). This is the test that would have caught the
 * 2026-08-02 review's C1 (raw is an embedded JSON OBJECT on the wire, never a
 * string) and C2 (parent_tool_use_id is emitted as an explicit `null` on
 * virtually every main-loop event, not merely absent) findings.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  argsSummary,
  eventChipLabel,
  eventMessageId,
  eventUsage,
  parentToolUseId,
  parseRawEvent,
  summarizeEvent,
  toolUseBlocks,
} from './episodeRawEvent.ts';

// server/internal/resourceaccess/agenticjob/testdata/streamjson/success_with_tools.jsonl, line 7
const WRITE_TOOL_USE_LINE =
  '{"type":"assistant","message":{"model":"claude-sonnet-5","id":"msg_011CdeZ44G5n3VmRyeeUj6Hk","type":"message","role":"assistant","content":[{"type":"tool_use","id":"toolu_01Rjuz3fMBrwLKR72uHAcnmH","name":"Write","input":{"file_path":"/scrubbed/hello.txt","content":"hi"},"caller":{"type":"direct"}}],"stop_reason":null,"stop_sequence":null,"stop_details":null,"usage":{"input_tokens":2,"cache_creation_input_tokens":5244,"cache_read_input_tokens":29178,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":5244},"output_tokens":1,"service_tier":"standard","inference_geo":"not_available"},"diagnostics":null,"context_management":null},"parent_tool_use_id":null,"session_id":"d0becfe3-d703-4311-8d8f-bf0ed7139dd7","uuid":"412d4a74-3161-47da-95db-c5b96cbdedb7","timestamp":"2026-08-02T20:28:51.710Z","request_id":"req_011CdeZ42b8Ls5w9eV9oNt4M"}';

// server/internal/resourceaccess/agenticjob/testdata/streamjson/success_with_subagent.jsonl, line 9
const AGENT_TOOL_USE_LINE =
  '{"type":"assistant","message":{"model":"claude-sonnet-5","id":"msg_011CdeZNZXiq8QnTfcAxZoC2","type":"message","role":"assistant","content":[{"type":"tool_use","id":"toolu_01DL6Gqvj23os5rsNRhFSTmy","name":"Agent","input":{"description":"Answer what is 2+2","prompt":"What is 2+2? Reply with just the numeric answer.","run_in_background":false},"caller":{"type":"direct"}}],"stop_reason":null,"stop_sequence":null,"stop_details":null,"usage":{"input_tokens":2,"cache_creation_input_tokens":5588,"cache_read_input_tokens":29178,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":5588},"output_tokens":2,"service_tier":"standard","inference_geo":"not_available"},"diagnostics":null,"context_management":null},"parent_tool_use_id":null,"session_id":"bda495be-48e6-4476-8117-9e8459107b7d","uuid":"404c0fec-5634-47d2-ad3c-d2119387122e","timestamp":"2026-08-02T20:33:03.686Z","request_id":"req_011CdeZNXPz762hZ1AhfzTiG"}';

// server/internal/resourceaccess/agenticjob/testdata/streamjson/success_with_subagent.jsonl, line 12
// — the subagent's own turn: parent_tool_use_id is the OUTER Agent tool_use's id.
const SUBAGENT_PARENTED_LINE =
  '{"type":"user","message":{"role":"user","content":[{"type":"text","text":"What is 2+2? Reply with just the numeric answer."}]},"parent_tool_use_id":"toolu_01DL6Gqvj23os5rsNRhFSTmy","session_id":"bda495be-48e6-4476-8117-9e8459107b7d","uuid":"b567fb4f-b567-487d-bb8c-b82afda56e2e","timestamp":"2026-08-02T20:33:05.433Z","subagent_type":"general-purpose","task_description":"Answer what is 2+2"}';

// The REAL wire shape (C1): raw is a JSON OBJECT embedded verbatim
// (json.RawMessage marshals its bytes directly into the parent) — so the
// fixture used as `TimelineEvent.raw` is the ALREADY-PARSED object, exactly
// what it is at runtime after the HTTP response body's own JSON.parse.
const writeEventRaw: unknown = JSON.parse(WRITE_TOOL_USE_LINE);
const agentEventRaw: unknown = JSON.parse(AGENT_TOOL_USE_LINE);
const subagentParentedRaw: unknown = JSON.parse(SUBAGENT_PARENTED_LINE);

void test('C1: parseRawEvent accepts the real wire shape — an embedded JSON OBJECT, not a string', () => {
  assert.equal(typeof writeEventRaw, 'object');
  const parsed = parseRawEvent(writeEventRaw);
  assert.notEqual(parsed, undefined);
  assert.equal(parsed?.message?.model, 'claude-sonnet-5');
});

void test('C1: parseRawEvent still accepts a JSON-encoded STRING (defensive fallback only — not the real wire shape)', () => {
  const parsed = parseRawEvent(WRITE_TOOL_USE_LINE);
  assert.notEqual(parsed, undefined);
  assert.deepEqual(parsed, writeEventRaw);
});

void test('parseRawEvent returns undefined for null/undefined/malformed input, never throws', () => {
  assert.equal(parseRawEvent(null), undefined);
  assert.equal(parseRawEvent(undefined), undefined);
  assert.equal(parseRawEvent('not json'), undefined);
  assert.equal(parseRawEvent(42), undefined);
});

void test('extracts the Write tool_use block from the real fixture line (success_with_tools.jsonl)', () => {
  const tools = toolUseBlocks(parseRawEvent(writeEventRaw));
  assert.equal(tools.length, 1);
  assert.equal(tools[0]?.name, 'Write');
});

void test('extracts the Agent (subagent dispatch) tool_use block from the real fixture line (success_with_subagent.jsonl)', () => {
  const tools = toolUseBlocks(parseRawEvent(agentEventRaw));
  assert.equal(tools.length, 1);
  assert.equal(tools[0]?.name, 'Agent');
});

void test('C2: both real main-loop fixture lines carry an EXPLICIT null parent_tool_use_id (not an absent key)', () => {
  assert.ok(WRITE_TOOL_USE_LINE.includes('"parent_tool_use_id":null'));
  assert.ok(AGENT_TOOL_USE_LINE.includes('"parent_tool_use_id":null'));
});

void test('C2: parentToolUseId reads that explicit null as "no parent" (never throws on it)', () => {
  assert.equal(parentToolUseId(parseRawEvent(writeEventRaw)), undefined);
  assert.equal(parentToolUseId(parseRawEvent(agentEventRaw)), undefined);
});

void test('C2: parentToolUseId returns the real id on a subagent-parented event', () => {
  assert.equal(
    parentToolUseId(parseRawEvent(subagentParentedRaw)),
    'toolu_01DL6Gqvj23os5rsNRhFSTmy'
  );
});

void test('eventUsage reads the message-level usage block off the real fixture', () => {
  const usage = eventUsage(parseRawEvent(writeEventRaw));
  assert.equal(usage?.input_tokens, 2);
  assert.equal(usage.cache_read_input_tokens, 29178);
});

void test('eventMessageId reads the turn identity off the real fixture', () => {
  assert.equal(eventMessageId(parseRawEvent(writeEventRaw)), 'msg_011CdeZ44G5n3VmRyeeUj6Hk');
});

void test('argsSummary previews a path-like key verbatim (metadata, not content)', () => {
  const [tool] = toolUseBlocks(parseRawEvent(writeEventRaw));
  const summary = argsSummary(tool?.input);
  assert.ok(summary.includes('file_path=/scrubbed/hello.txt'));
});

void test('argsSummary NEVER previews a content-bearing key value (never-content rule, minor (a))', () => {
  const [tool] = toolUseBlocks(parseRawEvent(writeEventRaw));
  const summary = argsSummary(tool?.input);
  // The real Write tool call's `content` value is "hi" — must never appear
  // verbatim, only a length hint.
  assert.ok(!summary.includes('content=hi'));
  assert.ok(summary.includes('content=<2 chars>'));
});

// ---------------------------------------------------------------------------
// Per-event-type summaries (2026-08-02 founder UI review: non-assistant rows
// rendered as bare chips — "system", "rate_limit_event" — with zero
// descriptive content). Every fixture below is copied verbatim from the same
// real trace files as above.
// ---------------------------------------------------------------------------

// server/internal/resourceaccess/agenticjob/testdata/streamjson/success_with_tools.jsonl, line 5
const INIT_EVENT_LINE =
  '{"type":"system","subtype":"init","cwd":"/scrubbed","session_id":"d0becfe3-d703-4311-8d8f-bf0ed7139dd7","tools":["Task","Bash","CronCreate","CronDelete","CronList","DesignSync","Edit","EnterWorktree","ExitWorktree","Monitor","NotebookEdit","PushNotification","Read","RemoteTrigger","ReportFindings","ScheduleWakeup","SendMessage","Skill","TaskCreate","TaskGet","TaskList","TaskOutput","TaskStop","TaskUpdate","ToolSearch","WebFetch","WebSearch","Workflow","Write"],"mcp_servers":[{"name":"plugin:playwright:playwright","status":"failed"}],"model":"claude-sonnet-5","permissionMode":"auto","slash_commands":["xcodebuildmcp","deep-research","superpowers:brainstorming","superpowers:dispatching-parallel-agents","superpowers:executing-plans","superpowers:finishing-a-development-branch","superpowers:receiving-code-review","superpowers:requesting-code-review","superpowers:subagent-driven-development","superpowers:systematic-debugging","superpowers:test-driven-development","superpowers:using-git-worktrees","superpowers:using-superpowers","superpowers:verification-before-completion","superpowers:writing-plans","superpowers:writing-skills","design-sync","dataviz","update-config","verify","debug","code-review","simplify","batch","fewer-permission-prompts","doctor","loop","schedule","claude-api","run","run-skill-generator","agents","clear","color","compact","config","context","effort","fast","heapdump","init","mcp","model","__remote-workflow","workflow-launch-exec","reload-skills","rename","review","ultrareview","security-review","usage-credits","extra-usage","usage","insights","recap","goal","design","design-consent","design-revoke","team-onboarding"],"apiKeySource":"none","claude_code_version":"2.1.220","output_style":"default","agents":["claude","Explore","general-purpose","Plan","statusline-setup"],"skills":["xcodebuildmcp","deep-research","superpowers:brainstorming","superpowers:dispatching-parallel-agents","superpowers:executing-plans","superpowers:finishing-a-development-branch","superpowers:receiving-code-review","superpowers:requesting-code-review","superpowers:subagent-driven-development","superpowers:systematic-debugging","superpowers:test-driven-development","superpowers:using-git-worktrees","superpowers:using-superpowers","superpowers:verification-before-completion","superpowers:writing-plans","superpowers:writing-skills","design-sync","dataviz","update-config","verify","debug","code-review","simplify","batch","fewer-permission-prompts","doctor","loop","schedule","claude-api","run","run-skill-generator"],"plugins":[{"name":"superpowers","path":"/scrubbed/.claude/plugins/cache/claude-plugins-official/superpowers/6.2.0","source":"superpowers@claude-plugins-official","version":"6.2.0"},{"name":"playwright","path":"/scrubbed/.claude/plugins/cache/claude-plugins-official/playwright/unknown","source":"playwright@claude-plugins-official"}],"capabilities":["interrupt_receipt_v1","interrupt_cancel_queued_v1","msg_lifecycle_v1"],"analytics_disabled":false,"product_feedback_disabled":false,"uuid":"7ad2c995-98d1-4df8-8b7f-72261d07ea49","memory_paths":{"auto":"/scrubbed/.claude/projects/-scrubbed/memory/"},"fast_mode_state":"off","fast_mode_disabled_reason":"sdk_opt_in_required"}';

// server/internal/resourceaccess/agenticjob/testdata/streamjson/success_with_tools.jsonl, line 9
const RATE_LIMIT_EVENT_LINE =
  '{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","resetsAt":1785704400,"rateLimitType":"five_hour","overageStatus":"rejected","overageDisabledReason":"org_level_disabled","isUsingOverage":false},"uuid":"64551f95-f627-490c-a881-cd7d816ab74d","session_id":"d0becfe3-d703-4311-8d8f-bf0ed7139dd7"}';

// server/internal/resourceaccess/agenticjob/testdata/streamjson/success_with_tools.jsonl, line 8
// — the Write tool_use's tool_result, content is a plain STRING (see also
// success_with_subagent.jsonl:15 for the array-of-text-blocks shape, covered
// structurally by toolResultContentLength — not re-fixtured here).
const TOOL_RESULT_USER_LINE =
  '{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_01Rjuz3fMBrwLKR72uHAcnmH","type":"tool_result","content":"File created successfully at: /scrubbed/hello.txt (file state is current in your context — no need to Read it back)"}]},"parent_tool_use_id":null,"session_id":"d0becfe3-d703-4311-8d8f-bf0ed7139dd7","uuid":"2b34e7b5-0b46-40c0-b1de-c4cba3826b19","timestamp":"2026-08-02T20:28:51.791Z","tool_use_result":{"type":"create","filePath":"/scrubbed/hello.txt","content":"hi","structuredPatch":[],"originalFile":null,"userModified":false}}';

// server/internal/resourceaccess/agenticjob/testdata/streamjson/success_with_tools.jsonl, line 6
// — an assistant turn with a text block and NO tool_use.
const ASSISTANT_TEXT_ONLY_LINE =
  '{"type":"assistant","message":{"model":"claude-sonnet-5","id":"msg_011CdeZ44G5n3VmRyeeUj6Hk","type":"message","role":"assistant","content":[{"type":"text","text":"This is a trivial file operation, not creative or debugging work, so no skill applies here."}],"stop_reason":null,"stop_sequence":null,"stop_details":null,"usage":{"input_tokens":2,"cache_creation_input_tokens":5244,"cache_read_input_tokens":29178,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":5244},"output_tokens":1,"service_tier":"standard","inference_geo":"not_available"},"diagnostics":null,"context_management":null},"parent_tool_use_id":null,"session_id":"d0becfe3-d703-4311-8d8f-bf0ed7139dd7","uuid":"15441028-b44c-47cc-ba58-99736cb34a0d","timestamp":"2026-08-02T20:28:51.269Z","request_id":"req_011CdeZ42b8Ls5w9eV9oNt4M"}';

// server/internal/resourceaccess/agenticjob/testdata/streamjson/success_with_tools.jsonl, line 12
// — the terminal result event.
const RESULT_EVENT_LINE =
  '{"is_error":false,"duration_api_ms":6424,"num_turns":2,"stop_reason":"end_turn","session_id":"d0becfe3-d703-4311-8d8f-bf0ed7139dd7","total_cost_usd":0.054201000000000006,"usage":{"input_tokens":4,"cache_creation_input_tokens":5424,"cache_read_input_tokens":63600,"output_tokens":171,"server_tool_use":{"web_search_requests":0,"web_fetch_requests":0},"service_tier":"standard","cache_creation":{"ephemeral_1h_input_tokens":5424,"ephemeral_5m_input_tokens":0},"inference_geo":"not_available","iterations":[{"input_tokens":2,"output_tokens":47,"cache_read_input_tokens":34422,"cache_creation_input_tokens":180,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":180},"type":"message"}],"speed":"standard"},"modelUsage":{"claude-sonnet-5":{"inputTokens":4,"outputTokens":171,"cacheReadInputTokens":63600,"cacheCreationInputTokens":5424,"webSearchRequests":0,"costUSD":0.054201000000000006,"contextWindow":1000000,"maxOutputTokens":64000,"canonicalModel":"claude-sonnet-5","provider":"firstParty"}},"permission_denials":[],"terminal_reason":"completed","fast_mode_state":"off","fast_mode_disabled_reason":"sdk_opt_in_required","subtype":"success","api_error_status":null,"result":"The tool result confirms the file is `hi` — read-back not needed since it\'s already current in context. Created `hello.txt` with contents \\"hi\\".","ttft_ms":2968,"ttft_stream_ms":2085,"time_to_request_ms":20,"type":"result","duration_ms":6488,"uuid":"c2b5ed6a-e19d-4ac6-be07-0027c7b869a4"}';

const initEventRaw: unknown = JSON.parse(INIT_EVENT_LINE);
const rateLimitEventRaw: unknown = JSON.parse(RATE_LIMIT_EVENT_LINE);
const toolResultUserRaw: unknown = JSON.parse(TOOL_RESULT_USER_LINE);
const assistantTextOnlyRaw: unknown = JSON.parse(ASSISTANT_TEXT_ONLY_LINE);
const resultEventRaw: unknown = JSON.parse(RESULT_EVENT_LINE);

void test('system/init: chip reads "system · init"', () => {
  assert.equal(eventChipLabel('system', parseRawEvent(initEventRaw)), 'system · init');
});

void test('system/init: summary surfaces model, real tool count, and cwd off the real fixture', () => {
  const fixture = initEventRaw as { model: string; tools: string[]; cwd: string };
  const summary = summarizeEvent('system', parseRawEvent(initEventRaw));
  assert.equal(
    summary,
    `model ${fixture.model} · ${String(fixture.tools.length)} tools · cwd ${fixture.cwd}`
  );
});

void test('system: an other-subtype event (no bespoke handling) falls through to the generic scalar-key summary', () => {
  // Reuses the earlier tool_use-line constants' sibling system events would
  // require a 4th fixture; the init line itself, read under a subtype the
  // module does NOT special-case, exercises the same fallback path.
  const genericSubtypeLine = INIT_EVENT_LINE.replace(
    '"subtype":"init"',
    '"subtype":"other_subtype"'
  );
  const summary = summarizeEvent(
    'system',
    parseRawEvent(JSON.parse(genericSubtypeLine) as unknown)
  );
  assert.notEqual(summary, undefined);
  // The generic fallback must never dump the huge slash_commands/skills
  // arrays (not scalar) or the full tools array (also not scalar) — only
  // compact scalar fields survive.
  assert.ok(!(summary ?? '').includes('xcodebuildmcp'));
});

void test('rate_limit_event: chip stays plain (no per-subtype label — only system events get one)', () => {
  assert.equal(
    eventChipLabel('rate_limit_event', parseRawEvent(rateLimitEventRaw)),
    'rate_limit_event'
  );
});

void test('rate_limit_event: summary surfaces status/rateLimitType/overageStatus off the real fixture', () => {
  const fixture = rateLimitEventRaw as {
    rate_limit_info: { status: string; rateLimitType: string; overageStatus: string };
  };
  const summary = summarizeEvent('rate_limit_event', parseRawEvent(rateLimitEventRaw));
  assert.equal(
    summary,
    `status ${fixture.rate_limit_info.status} · ${fixture.rate_limit_info.rateLimitType} · overage ${fixture.rate_limit_info.overageStatus}`
  );
});

void test('user/tool_result: summary is "tool_result · ok · <n> chars" — real content length, never the content itself', () => {
  const fixture = toolResultUserRaw as { message: { content: [{ content: string }] } };
  const realContent = fixture.message.content[0].content;
  const summary = summarizeEvent('user', parseRawEvent(toolResultUserRaw));
  assert.equal(summary, `tool_result · ok · ${String(realContent.length)} chars`);
  assert.ok(!summary.includes(realContent));
});

void test('assistant text-only (no tool_use): summary is a truncated excerpt of the real text', () => {
  const fixture = assistantTextOnlyRaw as { message: { content: [{ text: string }] } };
  const realText = fixture.message.content[0].text;
  assert.ok(
    realText.length > 80,
    'fixture text must exceed the 80-char excerpt cap to prove truncation'
  );
  const summary = summarizeEvent('assistant', parseRawEvent(assistantTextOnlyRaw));
  assert.equal(summary, `${realText.slice(0, 80)}…`);
});

void test('assistant tool_use event: summarizeEvent returns undefined (the tool_use row already covers it)', () => {
  const summary = summarizeEvent('assistant', parseRawEvent(writeEventRaw));
  assert.equal(summary, undefined);
});

void test('result: summary adds humanized duration + subtype off the real fixture (is_error false → no ERROR)', () => {
  const fixture = resultEventRaw as { subtype: string; duration_ms: number; is_error: boolean };
  assert.equal(fixture.is_error, false);
  const summary = summarizeEvent('result', parseRawEvent(resultEventRaw));
  assert.equal(summary, `${fixture.subtype} · 6.5s`);
});

void test('result: is_error true appends ERROR (synthetic — no captured fixture line has is_error on a tool_result block)', () => {
  const erroredLine = RESULT_EVENT_LINE.replace('"is_error":false', '"is_error":true');
  const summary = summarizeEvent('result', parseRawEvent(JSON.parse(erroredLine) as unknown));
  assert.ok((summary ?? '').includes('ERROR'));
});

void test('unknown/undecodable event type: summarizeEvent returns undefined (keeps current behavior)', () => {
  assert.equal(summarizeEvent('some_future_type', parseRawEvent(rateLimitEventRaw)), undefined);
  assert.equal(summarizeEvent('system', undefined), undefined);
  assert.equal(eventChipLabel('system', undefined), 'system');
});
