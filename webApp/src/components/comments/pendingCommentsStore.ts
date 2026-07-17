/**
 * localStorage persistence for pending (client-side, unsent) send-back comments,
 * extracted from CommentContext.tsx so the invalidation rules are unit-testable
 * under node:test (which cannot import .tsx files — same posture as
 * disabledCommentContext.ts).
 *
 * ── Incarnation binding (F-QA2-5) ────────────────────────────────────────────
 * A project can be DELETED and RECREATED under the SAME ProjectID (git repo
 * reset + re-onboard), so `projectId:kind` alone is NOT a stable identity — a
 * fresh incarnation would resurrect the previous incarnation's unsent drafts.
 * The GetProject payload exposes no creation stamp today, but it does expose
 * the head-state `Version`: the optimistic-concurrency counter persisted in
 * project.json, bumped by +1 on EVERY applied mutation and reset when the
 * project is recreated (CreateProject starts a fresh aggregate at Version 0).
 * Within one incarnation Version therefore NEVER decreases.
 *
 * Each persisted slot carries the Version observed when it was last written.
 * On load, `stored.projectVersion > currentVersion` is impossible within the
 * same incarnation — it proves the entries were written against a PREVIOUS
 * incarnation, so they are dropped (and the slot removed). This is the best
 * incarnation signal available client-side; a recreated project that has
 * already advanced PAST the old incarnation's version count cannot be detected
 * — a true fix needs the server to expose a creation identity (e.g. a
 * CreatedAt set once at CreateProject) on the GetProject payload.
 */
import type { PostedComment } from './commentContextTypes';

/** localStorage namespace for pending (client-side, unsent) send-back comments. */
const PENDING_STORAGE_PREFIX = 'aiarch.pendingComments';

/**
 * Minimal KV surface of Web Storage. Injected (rather than read from the
 * global) so node:test can exercise the invalidation rules with an in-memory
 * map, and so a surface where `window.localStorage` itself throws (sandboxed
 * MCP iframe / storage disabled) can pass `null` to degrade to in-memory-only.
 */
export interface PendingCommentStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

/** The persisted slot shape: entries + the incarnation stamp they were written under. */
interface PendingEnvelope {
  /** Project head-state Version observed when the entries were last persisted. */
  projectVersion: number;
  comments: PostedComment[];
}

export function storageKeyFor(activeKey: string): string {
  return `${PENDING_STORAGE_PREFIX}.${activeKey}`;
}

function isEnvelope(v: unknown): v is PendingEnvelope {
  return (
    typeof v === 'object' &&
    v !== null &&
    !Array.isArray(v) &&
    typeof (v as { projectVersion?: unknown }).projectVersion === 'number' &&
    Array.isArray((v as { comments?: unknown }).comments)
  );
}

/**
 * Best-effort load of a slot's persisted pending entries (storage may be
 * unavailable). `currentProjectVersion` is the project Version the surface just
 * read from GetProject; entries stamped with a HIGHER version were written by a
 * previous incarnation of this ProjectID and are dropped. Entries in the legacy
 * pre-envelope format (a bare array) carry no incarnation stamp, so they cannot
 * be proven to belong to THIS incarnation — they are dropped too (unsent drafts
 * are ephemeral; resurrecting a stale incarnation's drafts is the worse failure).
 */
export function loadPending(
  storage: PendingCommentStorage | null,
  activeKey: string,
  currentProjectVersion: number
): PostedComment[] {
  if (storage === null) return [];
  try {
    const key = storageKeyFor(activeKey);
    const raw = storage.getItem(key);
    if (raw === null) return [];
    const parsed = JSON.parse(raw) as unknown;
    if (!isEnvelope(parsed) || parsed.projectVersion > currentProjectVersion) {
      storage.removeItem(key);
      return [];
    }
    return parsed.comments;
  } catch {
    return [];
  }
}

/**
 * Best-effort persist, stamping the entries with the observed project Version
 * (empty list removes the slot so a cleared step leaves no orphan).
 */
export function savePending(
  storage: PendingCommentStorage | null,
  activeKey: string,
  list: PostedComment[],
  projectVersion: number
): void {
  if (storage === null) return;
  try {
    const key = storageKeyFor(activeKey);
    if (list.length === 0) {
      storage.removeItem(key);
    } else {
      const envelope: PendingEnvelope = { projectVersion, comments: list };
      storage.setItem(key, JSON.stringify(envelope));
    }
  } catch {
    /* storage unavailable (private mode / quota) — pending stays in-memory only. */
  }
}

/** The page's localStorage, or null where accessing it throws (sandboxed iframe). */
export function browserPendingCommentStorage(): PendingCommentStorage | null {
  try {
    return globalThis.localStorage;
  } catch {
    return null;
  }
}
