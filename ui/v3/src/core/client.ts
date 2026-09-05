import { affectedActiveQueries, rootFields } from "./mutation-invalidation";
import { invalidateAfterEntityJob } from "./entity-job-invalidation";
import type {
  ApolloCache,
  DocumentNode,
  OperationVariables,
} from "@apollo/client";
import {
  type Modifiers,
  isReference,
  type Reference,
} from "@apollo/client/cache";
import { useApolloClient, useMutation } from "@apollo/client/react";
import type { TypedDocumentNode } from "@graphql-typed-document-node/core";
import type { FieldNode, OperationDefinitionNode } from "graphql";
import { createClient } from "./create-client";

let clients: ReturnType<typeof createClient> | undefined;
const getClients = () => (clients ??= createClient());
export const getClient = () => getClients().client;
export const getWSClient = () => getClients().wsClient;

function isField(node: { kind: string }): node is FieldNode {
  return node.kind === "Field";
}

function getQueryDefinition(doc: DocumentNode): OperationDefinitionNode {
  const def = doc.definitions.find(
    (d): d is OperationDefinitionNode =>
      d.kind === "OperationDefinition" && d.operation === "query",
  );
  if (!def) throw new Error("No query operation found in document");
  return def;
}

export function evictQueries(cache: ApolloCache, queries: DocumentNode[]) {
  const fields: Modifiers = {};
  for (const query of queries) {
    const { selections } = getQueryDefinition(query).selectionSet;
    for (const field of selections) {
      if (!isField(field)) continue;
      fields[field.name.value] = (_value, { DELETE }) => DELETE;
    }
  }
  cache.modify({ fields });
  cache.gc();
}

/**
 * Apply a delete to the Apollo cache so subscribed list views update
 * immediately, without waiting for a refetch.
 *
 * Steps:
 *   1. `cache.modify` the root list query (e.g. `findScenes`) to drop
 *      the deleted ids from its `items` array and decrement `count`.
 *      Without this, the cached list keeps a dangling reference to
 *      the evicted entity — Apollo filters dangling refs from arrays
 *      automatically when reading, but `count` stays stale and the
 *      gap doesn't always trigger a re-broadcast.
 *   2. `cache.evict` each entity entry so any other consumer reading
 *      `Entity:id` sees a miss.
 *   3. `cache.gc` to clean up orphans.
 *
 * Use this from every entity destroy mutation's `update` (single +
 * bulk). Single delete is a one-id call; bulk passes the array.
 *
 * Example:
 *   removeEntitiesFromCache({
 *     cache,
 *     typename: "Scene",
 *     listFieldName: "findScenes",
 *     itemsField: "scenes",
 *     ids: [scene.id],
 *   });
 */
export function removeEntitiesFromCache({
  cache,
  typename,
  listFieldName,
  itemsField,
  ids,
}: {
  cache: ApolloCache;
  typename: string;
  listFieldName: string;
  itemsField: string;
  ids: string[];
}): void {
  if (ids.length === 0) return;
  const idSet = new Set(ids);

  // The list query's cache shape is `{ count: number, [itemsField]: Reference[], ... }`.
  // We type as `unknown` and narrow inside because the field-name keys
  // are dynamic — `cache.modify`'s generic can't usefully constrain
  // them, and the tree-shape varies per-entity (some queries select
  // additional fields on the page result).
  cache.modify({
    fields: {
      [listFieldName](existing: unknown, { readField }) {
        if (existing == null || isReference(existing)) return existing;
        const page = existing as { count?: number } & Record<string, unknown>;
        const items = page[itemsField];
        if (!Array.isArray(items)) return existing;
        const refs = items as Reference[];
        const filtered = refs.filter((ref) => {
          const id = readField<string>("id", ref);
          return id === undefined || !idSet.has(id);
        });
        const removed = refs.length - filtered.length;
        if (removed === 0) return existing;
        return {
          ...page,
          [itemsField]: filtered,
          count: Math.max(0, (page.count ?? 0) - removed),
        };
      },
    },
  });

  for (const id of ids) {
    cache.evict({ id: cache.identify({ __typename: typename, id }) });
  }
  cache.gc();
}

/**
 * `useMutation` for mutations that change relationships, counts, or aggregate
 * fields — creates, destroys, bulk updates, edit-form submits, merges. Defaults
 * refetching to active queries in the affected library domains, including
 * parent counts and relationship lists. Configuration and plugin queries
 * are not part of library invalidation. Pair with `removeEntitiesFromCache`
 * inside `update` for the destroying entity's own list, so the visible card
 * still drops with no flash while the rest of the page reconciles.
 *
 * Don't use for pure scalar toggles (favorite, rating, organized, o-counter,
 * `image_path`, etc.) — Apollo's normalized cache propagates updates to the
 * same entity by id without a refetch, so paying for one is wasted.
 */
export function useEntityMutation<TData, TVariables extends OperationVariables>(
  document: TypedDocumentNode<TData, TVariables>,
  options?: useMutation.Options<TData, TVariables>,
) {
  const client = useApolloClient();
  return useMutation<TData, TVariables>(document, {
    refetchQueries: (result) => {
      if (rootFields(document).some((field) => /^bulk.*Job$/.test(field))) {
        for (const id of Object.values(result.data ?? {})) {
          if (typeof id === "string")
            invalidateAfterEntityJob(client, document, id);
        }
        return [];
      }
      return affectedActiveQueries(client, document);
    },
    ...options,
  } as useMutation.Options<TData, TVariables, ApolloCache, TVariables>);
}
