import {
  createContext,
  useCallback,
  useContext,
  useLayoutEffect,
  useMemo,
  useRef,
  type ReactNode,
} from "react";
import { useListActivity } from "@/components/list/list-activity-context";
import { useCommittedRef } from "@/hooks/use-committed-ref";
import type { ShareEntitySource, ShareListSource } from "./share-source";

type ReadList = () => ShareListSource | undefined;

interface CollectionShareContextValue {
  entity: ShareEntitySource;
  readList: ReadList;
  registerList: (read: ReadList) => () => void;
}

const CollectionShareContext = createContext<
  CollectionShareContextValue | undefined
>(undefined);

export function CollectionShareProvider({
  entity,
  children,
}: {
  entity?: ShareEntitySource;
  children: ReactNode;
}) {
  const activeList = useRef<ReadList | undefined>(undefined);
  const readList = useCallback(() => activeList.current?.(), []);
  const registerList = useCallback((read: ReadList) => {
    activeList.current = read;
    return () => {
      if (activeList.current === read) activeList.current = undefined;
    };
  }, []);
  const value = useMemo(
    () => (entity ? { entity, readList, registerList } : undefined),
    [entity, readList, registerList],
  );
  return (
    <CollectionShareContext value={value}>{children}</CollectionShareContext>
  );
}

export function useCollectionShareContext() {
  return useContext(CollectionShareContext);
}

// The list remains the owner of its filters and cross-page selection. A share
// action reads the latest committed values only when opened; selection changes
// do not rerender the collection toolbar or copy list state into a second store.
export function useActiveCollectionShareList(source?: ShareListSource) {
  const registerList = useCollectionShareContext()?.registerList;
  const active = useListActivity();
  const current = useCommittedRef(source);
  const enabled = active && !!source;
  useLayoutEffect(() => {
    if (!enabled || !registerList) return;
    return registerList(() => current.current);
  }, [enabled, registerList, current]);
}
