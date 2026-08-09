import { useLayoutEffect, useRef } from "react";

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
 * Dynamic row measurements can change the size of content above the viewport.
 * TanStack Virtual can compensate by writing a corrected scroll offset, but a
 * programmatic scroll during native touch momentum stops that momentum. Only
 * correct an idle viewport, and leave deletion-refill preservation in sole
 * control while a page is temporarily short.
 */
export function shouldAdjustVirtualizedListScrollPosition(
  itemStart: number,
  scrollOffset: number,
  isScrolling: boolean,
  preserveDuringRefill: boolean,
): boolean {
  return !isScrolling && !preserveDuringRefill && itemStart < scrollOffset;
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
