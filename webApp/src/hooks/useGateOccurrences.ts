/**
 * Every activity's gate occurrence (gateOccurrences.ts), folded from the query
 * cache's per-activity session reads (tasks-lens review C2).
 *
 * The store belongs to the QueryClient, not to a component: it subscribes to the
 * query cache once, the first time any component asks, and keeps folding reads for
 * the client's lifetime. So a remount of the console — navigating home and back —
 * keeps every epoch a decision was recorded against, and a read the cache takes
 * while the console is away is still counted. Components read it through
 * useSyncExternalStore; nothing here sets React state from an effect.
 *
 * Each occurrence carries when its latest read was REQUESTED — the time the
 * decision's evidence rule counts (tasks round 2). That time comes from the ONE
 * request-time store (readRequestTimes.ts), which the Begin hold reads too: this
 * store folds each read through `subscribeToShownReads`, never timing fetches on
 * its own (tasks-lens merge round). And it keeps only the most recently observed
 * activities (gateOccurrences.OCCURRENCE_LIMIT).
 */
import { useSyncExternalStore } from 'react';
import { useQueryClient, type QueryClient } from '@tanstack/react-query';
import type { ConstructionSessionState } from '../contracts/types';
import { readRequestedAtByHash, subscribeToShownReads } from './readRequestTimes.ts';
import {
  observeGate,
  occurrenceKey,
  withOccurrence,
  type GateOccurrence,
} from './gateOccurrences.ts';

export type GateOccurrences = ReadonlyMap<string, GateOccurrence>;

/** One QueryClient's occurrence store (exported for useGateOccurrences.test.ts). */
export interface GateOccurrenceStore {
  snapshot: GateOccurrences;
  listeners: Set<() => void>;
}

type Store = GateOccurrenceStore;

const stores = new WeakMap<QueryClient, Store>();

function sessionOf(data: unknown): ConstructionSessionState | null | undefined {
  if (data === null) return null;
  if (typeof data === 'object' && 'stage' in data && typeof data.stage === 'string') {
    return data as ConstructionSessionState;
  }
  return undefined;
}

/** The fields of a cached query this reads. */
interface CachedRead {
  queryKey: readonly unknown[];
  queryHash: string;
  state: { data: unknown; dataUpdatedAt: number };
}

/** The client's store, created — and subscribed to its query cache — on first ask. */
export function gateOccurrenceStoreFor(client: QueryClient): Store {
  const existing = stores.get(client);
  if (existing !== undefined) return existing;
  const store: Store = { snapshot: new Map(), listeners: new Set() };
  const fold = (query: CachedRead, requestedAt: number): void => {
    const [root, projectId, activityId] = query.queryKey;
    if (root !== 'constructionSession' || typeof projectId !== 'string') return;
    if (typeof activityId !== 'string') return;
    const session = sessionOf(query.state.data);
    if (session === undefined) return;
    const key = occurrenceKey(projectId, activityId);
    const prev = store.snapshot.get(key);
    const next = observeGate(
      prev,
      session === null ? null : session.stage,
      query.state.dataUpdatedAt,
      requestedAt
    );
    if (next === prev) return;
    store.snapshot = withOccurrence(store.snapshot, key, next);
    for (const listener of store.listeners) listener();
  };
  // A read already cached takes whatever request time the one store has for it: 0
  // where it was cached before that store subscribed, which is never newer.
  for (const query of client.getQueryCache().getAll()) {
    fold(query, readRequestedAtByHash(client, query.queryHash));
  }
  subscribeToShownReads(client, fold);
  stores.set(client, store);
  return store;
}

export function useGateOccurrences(): GateOccurrences {
  const store = gateOccurrenceStoreFor(useQueryClient());
  return useSyncExternalStore(
    (onChange) => {
      store.listeners.add(onChange);
      return (): void => {
        store.listeners.delete(onChange);
      };
    },
    () => store.snapshot
  );
}
