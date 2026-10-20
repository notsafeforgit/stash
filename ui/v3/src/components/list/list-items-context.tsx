import { createContext, useContext, type PropsWithChildren } from "react";

/** A provider/consumer pair fixes the item type once. Consumers cannot choose
 * a different generic type from the provider. Selection remains a separate
 * context so selecting cards does not republish every list item. */
export function createListItemsContext<T>() {
  const context = createContext<readonly T[]>([]);
  const selectionContext = createContext<(() => readonly T[]) | undefined>(
    undefined,
  );
  function Provider({
    items,
    getSelectedItems,
    children,
  }: PropsWithChildren<{
    items: readonly T[];
    getSelectedItems?: () => readonly T[];
  }>) {
    return (
      <context.Provider value={items}>
        <selectionContext.Provider value={getSelectedItems}>
          {children}
        </selectionContext.Provider>
      </context.Provider>
    );
  }
  function useItems() {
    return useContext(context);
  }
  function useSelectionGetter() {
    return useContext(selectionContext);
  }
  return { Provider, useItems, useSelectionGetter };
}
