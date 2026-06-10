import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";

// EntityDataTable owns its `useReactTable` instance, but the column-manager
// button needs to live in the chrome bar (or mobile view-options drawer)
// rather than in its own row above the table. To bridge the gap without
// lifting all of the table's state, the toolbar/drawer renders a
// `<TableToolbarSlot />` element here; EntityDataTable reads that element
// from context and `createPortal`s its column-manager Sheet into it.
//
// `providerCount` is reference-counted so EntityDataTable can tell the
// difference between "nobody owns a slot, fall back to an inline button"
// and "a slot is owned, but the host element isn't mounted right now"
// (e.g. the mobile view-options drawer is closed). In the second case the
// inline fallback would double-render the button on top of the drawer's
// hidden one — so we suppress it.

interface SlotState {
  slotEl: HTMLElement | null;
  setSlotEl: (el: HTMLElement | null) => void;
  providerCount: number;
  registerProvider: () => () => void;
}

const TableToolbarSlotContext = createContext<SlotState | null>(null);

export function TableToolbarSlotProvider({
  children,
}: {
  children: ReactNode;
}) {
  const [slotEl, setSlotEl] = useState<HTMLElement | null>(null);
  const [providerCount, setProviderCount] = useState(0);
  const value = useMemo<SlotState>(
    () => ({
      slotEl,
      setSlotEl,
      providerCount,
      registerProvider: () => {
        setProviderCount((n) => n + 1);
        return () => setProviderCount((n) => n - 1);
      },
    }),
    [slotEl, providerCount],
  );
  return (
    <TableToolbarSlotContext.Provider value={value}>
      {children}
    </TableToolbarSlotContext.Provider>
  );
}

/**
 * Reserves a DOM element to portal the column-manager button into. Render
 * this only when the table view is active — the surrounding context
 * registers the latest slot element and EntityDataTable portals into it.
 */
export function TableToolbarSlot({ className }: { className?: string }) {
  const ctx = useContext(TableToolbarSlotContext);
  return (
    <div
      ref={(el) => ctx?.setSlotEl(el)}
      className={className}
      data-slot="table-toolbar-slot"
    />
  );
}

/**
 * Declares "a slot will be provided" without actually mounting the slot
 * element. Use from the toolbar/drawer that *owns* the slot when the host
 * element is conditionally mounted (e.g. mobile drawer that mounts its body
 * only when open). EntityDataTable suppresses its inline fallback while a
 * provider is registered, so the button won't double-render above the
 * table while the drawer is closed.
 */
export function useDeclareTableToolbarProvider(active: boolean) {
  const ctx = useContext(TableToolbarSlotContext);
  useEffect(() => {
    if (!ctx || !active) return;
    return ctx.registerProvider();
  }, [ctx, active]);
}

export function useTableToolbarSlotEl(): HTMLElement | null {
  return useContext(TableToolbarSlotContext)?.slotEl ?? null;
}

export function useTableToolbarHasProvider(): boolean {
  return (useContext(TableToolbarSlotContext)?.providerCount ?? 0) > 0;
}
