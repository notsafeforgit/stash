import { createContext, useContext, type PropsWithChildren } from "react";

/** A provider/consumer pair fixes the item type once. Consumers cannot choose
 * a different generic type from the provider. Selection remains a separate
 * context so selecting cards does not republish every list item. */
export function createListItemsContext<T>() {
  const context = createContext<readonly T[]>([]);
  function Provider({
    items,
    children,
  }: PropsWithChildren<{ items: readonly T[] }>) {
    return <context.Provider value={items}>{children}</context.Provider>;
  }
  function useItems() {
    return useContext(context);
  }
  return { Provider, useItems };
}
