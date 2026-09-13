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
 * that.)
 *
 * ONE STORE, TWO READERS (tasks-lens merge round)
 * -----------------------------------------------
 * The tasks lens's gate decisions count a session read the same way: a read
 * already in flight when a decision failed describes the gate from before it
 * (decisionFlow.ts). Its occurrence store (useGateOccurrences) used to note fetch
 * starts itself; it now folds each read through `subscribeToShownReads`, with the
 * request time this store paired to it. So the Begin hold and the gate decisions
 * cannot disagree about when the same read was asked for.
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
import { orderedNow } from '../utilities/orderedNow.ts';

/** The fields of a cached query a shown-read listener may read. */
export interface ShownRead {
  queryKey: readonly unknown[];
  queryHash: string;
  state: { data: unknown; dataUpdatedAt: number };
}

/** Told of every read a query now shows, with when it was requested. */
export type ShownReadListener = (query: ShownRead, requestedAt: number) => void;

interface Store {
  /** When each query's latest fetch began. */
  fetchStartedAt: Map<string, number>;
  /** When the read each query shows was requested. */
  shownRequestedAt: Map<string, number>;
  listeners: Set<() => void>;
  /** Readers that fold each shown read themselves (useGateOccurrences). */
  readListeners: Set<ShownReadListener>;
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
    readListeners: new Set(),
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
      // The ordered clock, shared with the Begin records: a fetch started just after
      // a record is strictly later than it, even within one millisecond (fix I).
      store.fetchStartedAt.set(hash, orderedNow());
      return;
    }
    if (event.action.type !== 'success') return;
    const requestedAt = event.action.manual === true ? 0 : (store.fetchStartedAt.get(hash) ?? 0);
    store.shownRequestedAt.set(hash, requestedAt);
    for (const listener of store.readListeners) listener(event.query, requestedAt);
    for (const listener of store.listeners) listener();
  });
  stores.set(client, store);
  return store;
}

/**
 * Fold every read a query of this client shows from now on, with when it was
 * requested: called after the store has paired the read with its request, so the
 * listener and readRequestedAt always agree. Returns the unsubscribe.
 */
export function subscribeToShownReads(
  client: QueryClient,
  listener: ShownReadListener
): () => void {
  const store = readRequestStoreFor(client);
  store.readListeners.add(listener);
  return (): void => {
    store.readListeners.delete(listener);
  };
}

/** When the read the query with this hash shows was requested; 0 where unknown. */
export function readRequestedAtByHash(client: QueryClient, queryHash: string): number {
  return readRequestStoreFor(client).shownRequestedAt.get(queryHash) ?? 0;
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
