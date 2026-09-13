/**
 * When the read a query currently SHOWS was REQUESTED: when its fetch began, not
 * when it arrived (`dataUpdatedAt`).
 *
 * WHY
 * ---
 * The Begin hold counts only reads NEWER than a failed dispatch as pump evidence
 * (beginControl.pumpEvidencedSince). Counted by arrival, a read sent before the
 * failure but landing after it would count, although it describes the project from
 * before the dispatch was answered. Counted by request, it cannot. (Today the
 * failure's refresh invalidates the project and session queries, which cancels a
 * read in flight, so such a read is dropped anyway; the rule does not lean on
 * that.) The tasks lens counts a gate read the same way (useGateOccurrences).
 *
 * HOW
 * ---
 * One store per QueryClient, subscribed to its query cache on first ask. Each
 * query's fetch START is recorded; when a fetch SUCCEEDS, the read now shown takes
 * that start as its request time. A cancelled fetch never succeeds, so it never
 * pairs with a read. Data written by hand (setQueryData) has no request, and a read
 * that was already cached before the store subscribed was never seen starting: both
 * are 0, which is never newer than anything.
 */
import { useSyncExternalStore } from 'react';
import { hashKey, useQueryClient, type QueryClient } from '@tanstack/react-query';

interface Store {
  /** When each query's latest fetch began. */
  fetchStartedAt: Map<string, number>;
  /** When the read each query shows was requested. */
  shownRequestedAt: Map<string, number>;
  listeners: Set<() => void>;
}

const stores = new WeakMap<QueryClient, Store>();

/** The client's store, created (and subscribed to its query cache) on first ask. */
export function readRequestStoreFor(client: QueryClient): Store {
  const existing = stores.get(client);
  if (existing !== undefined) return existing;
  const store: Store = {
    fetchStartedAt: new Map(),
    shownRequestedAt: new Map(),
    listeners: new Set(),
  };
  client.getQueryCache().subscribe((event) => {
    const hash = event.query.queryHash;
    if (event.type === 'removed') {
      store.fetchStartedAt.delete(hash);
      store.shownRequestedAt.delete(hash);
      return;
    }
    if (event.type !== 'updated') return;
    if (event.action.type === 'fetch') {
      store.fetchStartedAt.set(hash, Date.now());
      return;
    }
    if (event.action.type !== 'success') return;
    const requestedAt = event.action.manual === true ? 0 : (store.fetchStartedAt.get(hash) ?? 0);
    store.shownRequestedAt.set(hash, requestedAt);
    for (const listener of store.listeners) listener();
  });
  stores.set(client, store);
  return store;
}

/** When the read `queryKey` shows was requested; 0 where unknown. */
export function readRequestedAt(client: QueryClient, queryKey: readonly unknown[]): number {
  return readRequestStoreFor(client).shownRequestedAt.get(hashKey(queryKey)) ?? 0;
}

/** The same, as a hook: re-renders when a new read of any query lands. */
export function useReadRequestedAt(queryKey: readonly unknown[]): number {
  const client = useQueryClient();
  const store = readRequestStoreFor(client);
  const hash = hashKey(queryKey);
  return useSyncExternalStore(
    (onChange) => {
      store.listeners.add(onChange);
      return (): void => {
        store.listeners.delete(onChange);
      };
    },
    () => store.shownRequestedAt.get(hash) ?? 0
  );
}
