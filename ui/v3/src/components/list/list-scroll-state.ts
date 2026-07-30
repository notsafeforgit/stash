import { useCallback, useEffect, useLayoutEffect, useRef } from "react";
import type {
  BeginListDeletionScrollPreservation,
  FinishListDeletionScrollPreservation,
} from "./list-scroll-context";

export type ListPageChangeScrollTarget = "start" | "end" | null;

/**
 * Keep the existing viewport while a deleted item leaves a non-final page
 * temporarily short. The next query result can refill that gap with items
 * pulled forward from later pages.
 */
export function shouldPreserveListScrollDuringRefill(
  currentPage: number,
  itemsPerPage: number,
  totalCount: number,
  itemCount: number,
): boolean {
  const totalPages = Math.max(1, Math.ceil(totalCount / itemsPerPage));
  if (currentPage < 1 || currentPage > totalPages) return false;

  const isLastPage = currentPage === totalPages;
  const expectedItemCount = isLastPage
    ? Math.max(0, totalCount - (currentPage - 1) * itemsPerPage)
    : itemsPerPage;

  return itemCount < expectedItemCount;
}

/**
 * A normal pagination action starts at the top. If a deletion removed the
 * page the user was on, the replacement last page should land at its end.
 */
export function getListPageChangeScrollTarget(
  previousPage: number,
  currentPage: number,
  totalPages: number,
): ListPageChangeScrollTarget {
  if (previousPage === currentPage) return null;
  if (previousPage > totalPages && currentPage === totalPages) return "end";
  return "start";
}

export function clampListScrollTop(
  desiredScrollTop: number,
  scrollHeight: number,
  clientHeight: number,
): number {
  const maximumScrollTop = Math.max(0, scrollHeight - clientHeight);
  return Math.max(0, Math.min(desiredScrollTop, maximumScrollTop));
}

/**
 * Embedded mobile lists do not own the scroll viewport: their surrounding
 * detail page does. When deletion shortens the list, restore that outer
 * viewport to the same numeric position, clamped to the new bottom.
 */
export function getListDeletionScrollTop(
  previousTotalCount: number,
  totalCount: number,
  previousItemCount: number,
  itemCount: number,
  desiredScrollTop: number,
  scrollHeight: number,
  clientHeight: number,
): number | null {
  if (totalCount >= previousTotalCount && itemCount >= previousItemCount) {
    return null;
  }
  return clampListScrollTop(desiredScrollTop, scrollHeight, clientHeight);
}

export function findScrollableAncestor(
  element: HTMLElement,
): HTMLElement | null {
  let ancestor = element.parentElement;
  while (ancestor) {
    const overflowY = window.getComputedStyle(ancestor).overflowY;
    if (
      overflowY === "auto" ||
      overflowY === "scroll" ||
      overflowY === "overlay"
    ) {
      return ancestor;
    }
    ancestor = ancestor.parentElement;
  }
  return null;
}

/**
 * Capture list scroll at the deletion boundary, before Apollo evicts cards.
 *
 * Waiting until a count changes is too late: removing dangling entity
 * references can shorten the rendered list before its cached count/refetch
 * result changes, and the browser may already have clamped the surrounding
 * detail-page scroller to the top. The DeleteDialog starts this preservation
 * synchronously when the user confirms. Item/count changes restore from that
 * pre-mutation snapshot; mutation completion performs a final two-frame
 * restore after focus cleanup and virtualizer measurements have settled.
 */
export function useListDeletionScrollPreserver(
  listElement: HTMLElement | null,
  innerScrollElement: HTMLElement | null,
  useOuterScrollElement: boolean,
  totalCount: number,
  itemCount: number,
): BeginListDeletionScrollPreservation {
  interface DeletionSnapshot {
    element: HTMLElement;
    desiredScrollTop: number;
    totalCount: number;
    itemCount: number;
    completed: boolean;
    contentShrank: boolean;
  }

  const latestRef = useRef({
    listElement,
    innerScrollElement,
    useOuterScrollElement,
    totalCount,
    itemCount,
  });
  latestRef.current = {
    listElement,
    innerScrollElement,
    useOuterScrollElement,
    totalCount,
    itemCount,
  };

  const snapshotRef = useRef<DeletionSnapshot | null>(null);
  const firstFrameRef = useRef<number | null>(null);
  const secondFrameRef = useRef<number | null>(null);

  const cancelFrames = useCallback(() => {
    if (firstFrameRef.current !== null) {
      cancelAnimationFrame(firstFrameRef.current);
      firstFrameRef.current = null;
    }
    if (secondFrameRef.current !== null) {
      cancelAnimationFrame(secondFrameRef.current);
      secondFrameRef.current = null;
    }
  }, []);

  const restore = useCallback((snapshot: DeletionSnapshot) => {
    const { element } = snapshot;
    if (!element.isConnected) return;
    const target = clampListScrollTop(
      snapshot.desiredScrollTop,
      element.scrollHeight,
      element.clientHeight,
    );
    element.scrollTop = target;
  }, []);

  const restoreAcrossFrames = useCallback(
    (snapshot: DeletionSnapshot) => {
      cancelFrames();
      restore(snapshot);
      firstFrameRef.current = requestAnimationFrame(() => {
        firstFrameRef.current = null;
        restore(snapshot);
        secondFrameRef.current = requestAnimationFrame(() => {
          secondFrameRef.current = null;
          restore(snapshot);
          if (
            snapshotRef.current === snapshot &&
            snapshot.completed &&
            snapshot.contentShrank
          ) {
            snapshotRef.current = null;
          }
        });
      });
    },
    [cancelFrames, restore],
  );

  useLayoutEffect(() => {
    const snapshot = snapshotRef.current;
    if (!snapshot) return;
    const target = getListDeletionScrollTop(
      snapshot.totalCount,
      totalCount,
      snapshot.itemCount,
      itemCount,
      snapshot.desiredScrollTop,
      snapshot.element.scrollHeight,
      snapshot.element.clientHeight,
    );
    if (target === null) return;

    snapshot.contentShrank = true;
    snapshot.element.scrollTop = target;
    if (snapshot.completed) restoreAcrossFrames(snapshot);
  }, [itemCount, restoreAcrossFrames, totalCount]);

  useEffect(
    () => () => {
      cancelFrames();
      snapshotRef.current = null;
    },
    [cancelFrames],
  );

  return useCallback(() => {
    cancelFrames();
    const latest = latestRef.current;
    const scrollElement =
      latest.useOuterScrollElement && latest.listElement
        ? findScrollableAncestor(latest.listElement)
        : latest.innerScrollElement;
    if (!scrollElement) return () => {};

    const snapshot: DeletionSnapshot = {
      element: scrollElement,
      desiredScrollTop: scrollElement.scrollTop,
      totalCount: latest.totalCount,
      itemCount: latest.itemCount,
      completed: false,
      contentShrank: false,
    };
    snapshotRef.current = snapshot;

    const finish: FinishListDeletionScrollPreservation = (succeeded) => {
      if (snapshotRef.current !== snapshot) return;
      if (!succeeded) {
        snapshotRef.current = null;
        return;
      }
      snapshot.completed = true;
      restoreAcrossFrames(snapshot);

      // If the list has already shrunk, the second frame clears the snapshot.
      // Otherwise retain it for the delayed Apollo cache/refetch commit; the
      // layout effect above will restore and clear when item/count data lands.
      const current = latestRef.current;
      if (
        getListDeletionScrollTop(
          snapshot.totalCount,
          current.totalCount,
          snapshot.itemCount,
          current.itemCount,
          snapshot.desiredScrollTop,
          snapshot.element.scrollHeight,
          snapshot.element.clientHeight,
        ) !== null
      ) {
        snapshot.contentShrank = true;
      }
    };
    return finish;
  }, [cancelFrames, restoreAcrossFrames]);
}

export function usePreservedListScrollPosition(
  scrollElement: HTMLElement | null,
  preserveDuringRefill: boolean,
): void {
  const lastKnownScrollTopRef = useRef(0);
  const preservedScrollTopRef = useRef<number | null>(null);

  useLayoutEffect(() => {
    if (!scrollElement) return;
    const rememberScrollTop = () => {
      lastKnownScrollTopRef.current = scrollElement.scrollTop;
    };
    rememberScrollTop();
    scrollElement.addEventListener("scroll", rememberScrollTop, {
      passive: true,
    });
    return () => {
      rememberScrollTop();
      scrollElement.removeEventListener("scroll", rememberScrollTop);
    };
  }, [scrollElement]);

  useLayoutEffect(() => {
    if (!scrollElement) return;

    if (preserveDuringRefill) {
      preservedScrollTopRef.current ??= lastKnownScrollTopRef.current;
    }

    const preservedScrollTop = preservedScrollTopRef.current;
    if (preservedScrollTop != null) {
      scrollElement.scrollTop = clampListScrollTop(
        preservedScrollTop,
        scrollElement.scrollHeight,
        scrollElement.clientHeight,
      );
      if (!preserveDuringRefill) {
        preservedScrollTopRef.current = null;
      }
    }

    lastKnownScrollTopRef.current = scrollElement.scrollTop;
    return () => {
      lastKnownScrollTopRef.current = scrollElement.scrollTop;
    };
  }, [scrollElement, preserveDuringRefill]);
}

export function useListPageChangeScrollPosition(
  scrollElement: HTMLElement | null,
  currentPage: number,
  itemsPerPage: number,
  totalCount: number,
): void {
  const lastScrolledPageRef = useRef(currentPage);

  useLayoutEffect(() => {
    if (!scrollElement) return;
    const totalPages = Math.max(1, Math.ceil(totalCount / itemsPerPage));
    const target = getListPageChangeScrollTarget(
      lastScrolledPageRef.current,
      currentPage,
      totalPages,
    );
    lastScrolledPageRef.current = currentPage;
    if (target === null) return;
    scrollElement.scrollTop =
      target === "end"
        ? clampListScrollTop(
            scrollElement.scrollHeight,
            scrollElement.scrollHeight,
            scrollElement.clientHeight,
          )
        : 0;
  }, [currentPage, itemsPerPage, scrollElement, totalCount]);
}
