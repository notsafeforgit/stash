import type {
  ApolloCache,
  DocumentNode,
  OperationVariables,
} from "@apollo/client";
import { canonicalStringify } from "@apollo/client/utilities";
import { getOperationAST, valueFromASTUntyped } from "graphql";

// Shared across list mounts and filters, rather than a window that disappears
// on unmount. Active lists stay pinned; recent pages support immediate Back.
export const MAX_INACTIVE_LIST_QUERIES = 12;
type Entry = {
  fieldName: string;
  args: Record<string, unknown>;
  users: number;
};
const windows = new WeakMap<ApolloCache, Map<string, Entry>>();

export function retainListQuery(
  cache: ApolloCache,
  query: DocumentNode,
  variables: OperationVariables,
): () => void {
  let entries = windows.get(cache);
  if (!entries) {
    entries = new Map();
    windows.set(cache, entries);
  }
  const operation = getOperationAST(query);
  const values = { ...variables };
  for (const definition of operation?.variableDefinitions ?? []) {
    const name = definition.variable.name.value;
    if (values[name] === undefined && definition.defaultValue) {
      values[name] = valueFromASTUntyped(definition.defaultValue);
    }
  }
  const retained: Entry[] = [];
  for (const field of operation?.selectionSet.selections ?? []) {
    if (field.kind !== "Field") continue;
    const fieldName = field.name.value;
    const args = Object.fromEntries(
      (field.arguments ?? []).map((arg) => [
        arg.name.value,
        valueFromASTUntyped(arg.value, values),
      ]),
    );
    // Use schema field arguments, not operation variable names or aliases.
    const key = canonicalStringify({ fieldName, args });
    const entry = entries.get(key) ?? { fieldName, args, users: 0 };
    entry.users++;
    entries.delete(key);
    entries.set(key, entry);
    retained.push(entry);
  }
  pruneListQueries(cache);
  return () => {
    for (const entry of retained) entry.users--;
    pruneListQueries(cache);
  };
}

export function pruneListQueries(cache: ApolloCache): void {
  const entries = windows.get(cache);
  if (!entries) return;
  let inactive = [...entries.values()].filter((entry) => !entry.users).length;
  if (inactive <= MAX_INACTIVE_LIST_QUERIES) return;
  cache.batch({
    update(cache) {
      for (const [key, entry] of entries) {
        if (inactive <= MAX_INACTIVE_LIST_QUERIES) break;
        if (entry.users) continue;
        cache.evict({
          id: "ROOT_QUERY",
          fieldName: entry.fieldName,
          args: entry.args,
        });
        entries.delete(key);
        inactive--;
      }
    },
  });
  cache.gc();
}
