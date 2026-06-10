import { useEffect } from "react";

// How long to wait for the second key after 's'
const CHORD_TIMEOUT_MS = 1500;

function isTypingContext(el: Element | null): boolean {
  if (!el) return false;
  const tag = el.tagName.toLowerCase();
  if (tag === "input" || tag === "textarea" || tag === "select") return true;
  if ((el as HTMLElement).isContentEditable) return true;
  return false;
}

export interface ListKeyboardShortcutsProps {
  currentPage?: number;
  pages?: number;
  onChangePage?: (page: number) => void;
  showEditFilter?: () => void;
  onSelectAll?: () => void;
  onSelectNone?: () => void;
  onInvertSelection?: () => void;
  /** When true, all shortcuts are suppressed (e.g. while a lightbox is open). */
  disabled?: boolean;
}

export function useListKeyboardShortcuts(props: ListKeyboardShortcutsProps) {
  const {
    currentPage,
    pages = 0,
    onChangePage,
    showEditFilter,
    onSelectAll,
    onSelectNone,
    onInvertSelection,
    disabled,
  } = props;

  useEffect(() => {
    let pendingS = false;
    let sTimer: ReturnType<typeof setTimeout> | null = null;

    function clearS() {
      pendingS = false;
      if (sTimer !== null) {
        clearTimeout(sTimer);
        sTimer = null;
      }
    }

    function clampedPage(page: number) {
      return Math.max(1, Math.min(pages, page));
    }

    function onKeyDown(e: KeyboardEvent) {
      if (disabled) return;
      if (isTypingContext(document.activeElement)) return;

      // ── 's' chord (s a / s n / s i) ──────────────────────────────────
      if (pendingS) {
        clearS();
        if (e.key === "a") {
          e.preventDefault();
          onSelectAll?.();
        } else if (e.key === "n") {
          e.preventDefault();
          onSelectNone?.();
        } else if (e.key === "i") {
          e.preventDefault();
          onInvertSelection?.();
        }
        return;
      }

      if (
        e.key === "s" &&
        !e.ctrlKey &&
        !e.metaKey &&
        !e.altKey &&
        !e.shiftKey
      ) {
        e.preventDefault();
        pendingS = true;
        sTimer = setTimeout(clearS, CHORD_TIMEOUT_MS);
        return;
      }

      // ── filter open ───────────────────────────────────────────────────
      if (
        e.key === "f" &&
        !e.ctrlKey &&
        !e.metaKey &&
        !e.altKey &&
        !e.shiftKey
      ) {
        e.preventDefault();
        showEditFilter?.();
        return;
      }

      // ── pagination ────────────────────────────────────────────────────
      if (!currentPage || !onChangePage || !pages) return;

      if (e.key === "ArrowRight" && !e.ctrlKey && !e.metaKey && !e.altKey) {
        if (e.shiftKey) {
          e.preventDefault();
          onChangePage(clampedPage(currentPage + 10));
        } else {
          e.preventDefault();
          onChangePage(clampedPage(currentPage + 1));
        }
        return;
      }

      if (e.key === "ArrowLeft" && !e.ctrlKey && !e.metaKey && !e.altKey) {
        if (e.shiftKey) {
          e.preventDefault();
          onChangePage(clampedPage(currentPage - 10));
        } else {
          e.preventDefault();
          onChangePage(clampedPage(currentPage - 1));
        }
        return;
      }

      if (
        e.key === "End" &&
        e.ctrlKey &&
        !e.metaKey &&
        !e.altKey &&
        !e.shiftKey
      ) {
        e.preventDefault();
        onChangePage(pages);
        return;
      }

      if (
        e.key === "Home" &&
        e.ctrlKey &&
        !e.metaKey &&
        !e.altKey &&
        !e.shiftKey
      ) {
        e.preventDefault();
        onChangePage(1);
        return;
      }
    }

    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
      clearS();
    };
  }, [
    currentPage,
    pages,
    onChangePage,
    showEditFilter,
    onSelectAll,
    onSelectNone,
    onInvertSelection,
    disabled,
  ]);
}
