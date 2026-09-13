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
 */
import { useSyncExternalStore } from 'react';
import { useQueryClient, type QueryClient } from '@tanstack/react-query';
import type { ConstructionSessionState } from '../contracts/types';
import { observeGate, occurrenceKey, type GateOccurrence } from './gateOccurrences';

export type GateOccurrences = ReadonlyMap<string, GateOccurrence>;

interface Store {
  snapshot: GateOccurrences;
  listeners: Set<() => void>;
}

const stores = new WeakMap<QueryClient, Store>();

function sessionOf(data: unknown): ConstructionSessionState | null | undefined {
  if (data === null) return null;
  if (typeof data === 'object' && 'stage' in data && typeof data.stage === 'string') {
    return data as ConstructionSessionState;
  }
  return undefined;
}

/** The two fields of a cached query this reads. */
interface CachedRead {
  queryKey: readonly unknown[];
  state: { data: unknown; dataUpdatedAt: number };
}

function storeFor(client: QueryClient): Store {
  const existing = stores.get(client);
  if (existing !== undefined) return existing;
  const store: Store = { snapshot: new Map(), listeners: new Set() };
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
      query.state.dataUpdatedAt
    );
    if (next === prev) return;
    const snapshot = new Map(store.snapshot);
    snapshot.set(key, next);
    store.snapshot = snapshot;
    for (const listener of store.listeners) listener();
  };
  const cache = client.getQueryCache();
  for (const query of cache.getAll()) fold(query);
  cache.subscribe((event) => {
    if (event.type === 'updated' || event.type === 'added') fold(event.query);
  });
  stores.set(client, store);
  return store;
}

export function useGateOccurrences(): GateOccurrences {
  const store = storeFor(useQueryClient());
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
