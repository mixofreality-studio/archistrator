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
  eventMessageId,
  eventUsage,
  parentToolUseId,
  parseRawEvent,
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
