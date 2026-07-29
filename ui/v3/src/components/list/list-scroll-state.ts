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
 * Embedded mobile lists do not own the scroll viewport: their surrounding
 * detail page does. When deletion shortens the list, restore that outer
 * viewport to the same numeric position, clamped to the new bottom.
 */
export function getEmbeddedListDeletionScrollTop(
  previousTotalCount: number,
  totalCount: number,
  desiredScrollTop: number,
  scrollHeight: number,
  clientHeight: number,
): number | null {
  if (totalCount >= previousTotalCount) return null;
  return clampListScrollTop(desiredScrollTop, scrollHeight, clientHeight);
}

function findScrollableAncestor(element: HTMLElement): HTMLElement | null {
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
 * Preserve the actual page viewport used by embedded mobile lists.
 *
 * EntityList's own content element is the scroll viewport on standalone pages
 * and on desktop. Collection detail pages are different on mobile: the
 * performer/studio/tag header and the embedded list share an outer scroller.
 * Remember that scroller independently, then restore it when the result count
 * drops. A second animation-frame restoration wins over browser scroll
 * anchoring and virtualizer measurement work scheduled by the same commit.
 */
export function usePreservedEmbeddedListScrollPosition(
  listElement: HTMLElement | null,
  enabled: boolean,
  totalCount: number,
): void {
  const previousTotalCountRef = useRef(totalCount);
  const lastKnownScrollTopRef = useRef(0);
  const scrollElementRef = useRef<HTMLElement | null>(null);

  useLayoutEffect(() => {
    if (!enabled || !listElement) {
      scrollElementRef.current = null;
      return;
    }

    const scrollElement = findScrollableAncestor(listElement);
    scrollElementRef.current = scrollElement;
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
      if (scrollElementRef.current === scrollElement) {
        scrollElementRef.current = null;
      }
    };
  }, [enabled, listElement]);

  useLayoutEffect(() => {
    const previousTotalCount = previousTotalCountRef.current;
    previousTotalCountRef.current = totalCount;
    if (!enabled || !listElement) return;

    const scrollElement =
      scrollElementRef.current ?? findScrollableAncestor(listElement);
    if (!scrollElement) return;

    const desiredScrollTop = lastKnownScrollTopRef.current;
    if (totalCount >= previousTotalCount) return;

    const restore = () => {
      const target = getEmbeddedListDeletionScrollTop(
        previousTotalCount,
        totalCount,
        desiredScrollTop,
        scrollElement.scrollHeight,
        scrollElement.clientHeight,
      );
      if (target === null) return;
      scrollElement.scrollTop = target;
      lastKnownScrollTopRef.current = target;
    };

    restore();
    let secondFrame: number | undefined;
    const firstFrame = requestAnimationFrame(() => {
      restore();
      secondFrame = requestAnimationFrame(restore);
    });
    return () => {
      cancelAnimationFrame(firstFrame);
      if (secondFrame !== undefined) cancelAnimationFrame(secondFrame);
    };
  }, [enabled, listElement, totalCount]);
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
