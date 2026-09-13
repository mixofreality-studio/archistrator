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
 * It also notes when each session fetch BEGAN (the cache's `fetch` action), so an
 * occurrence carries when its latest read was requested — the time the decision's
 * evidence rule counts (tasks round 2). And it keeps only the most recently observed
 * activities (gateOccurrences.OCCURRENCE_LIMIT).
 */
import { useSyncExternalStore } from 'react';
import { useQueryClient, type QueryClient } from '@tanstack/react-query';
import type { ConstructionSessionState } from '../contracts/types';
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
  // When each query's current fetch began. A read the store never saw start (it was
  // in the cache before the store subscribed) has no request time: 0, never newer.
  const fetchStartedAt = new Map<string, number>();
  const fold = (query: CachedRead): void => {
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
      fetchStartedAt.get(query.queryHash) ?? 0
    );
    if (next === prev) return;
    store.snapshot = withOccurrence(store.snapshot, key, next);
    for (const listener of store.listeners) listener();
  };
  const cache = client.getQueryCache();
  for (const query of cache.getAll()) fold(query);
  cache.subscribe((event) => {
    if (event.type === 'removed') {
      fetchStartedAt.delete(event.query.queryHash);
      return;
    }
    if (event.type === 'updated' && event.action.type === 'fetch') {
      fetchStartedAt.set(event.query.queryHash, Date.now());
    }
    if (event.type === 'updated' || event.type === 'added') fold(event.query);
  });
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
