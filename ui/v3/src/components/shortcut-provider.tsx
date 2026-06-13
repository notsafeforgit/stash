import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
} from "react";

const CHORD_TIMEOUT_MS = 1500;

export interface ListShortcutScope {
  currentPage?: number;
  pages?: number;
  onChangePage?: (page: number) => void;
  showEditFilter?: () => void;
  onSelectAll?: () => void;
  onSelectNone?: () => void;
  onInvertSelection?: () => void;
  selectModeActive?: boolean;
  disabled?: boolean;
}

export interface OverlayShortcutScope {
  open: boolean;
  blocksListShortcuts?: boolean;
}

export interface ShortcutOverlayRootProps {
  blocksListShortcuts?: boolean;
}

export function useOverlayOpenState<T extends unknown[]>({
  open,
  defaultOpen,
  onOpenChange,
  blocksListShortcuts,
}: {
  open?: boolean;
  defaultOpen?: boolean;
  onOpenChange?: (open: boolean, ...args: T) => void;
  blocksListShortcuts?: boolean;
}) {
  const [uncontrolledOpen, setUncontrolledOpen] = React.useState(
    () => !!defaultOpen,
  );
  const effectiveOpen = open ?? uncontrolledOpen;

  const scope = useMemo(
    () => ({
      open: effectiveOpen,
      blocksListShortcuts,
    }),
    [effectiveOpen, blocksListShortcuts],
  );
  useOverlayShortcutScope(scope);

  const handleOpenChange = useCallback(
    (nextOpen: boolean, ...args: T) => {
      setUncontrolledOpen(nextOpen);
      onOpenChange?.(nextOpen, ...args);
    },
    [onOpenChange],
  );

  return {
    open,
    defaultOpen,
    onOpenChange: handleOpenChange,
  };
}

interface ShortcutContextValue {
  registerListScope: (id: symbol, scope: ListShortcutScope) => () => void;
  updateListScope: (id: symbol, scope: ListShortcutScope) => void;
  registerOverlayScope: (id: symbol, scope: OverlayShortcutScope) => () => void;
  updateOverlayScope: (id: symbol, scope: OverlayShortcutScope) => void;
}

const ShortcutContext = createContext<ShortcutContextValue | null>(null);

function isTypingContext(el: Element | null): boolean {
  if (!el) return false;
  const tag = el.tagName.toLowerCase();
  if (tag === "input" || tag === "textarea" || tag === "select") return true;
  if ((el as HTMLElement).isContentEditable) return true;
  if (el.closest("[contenteditable='true']")) return true;
  return false;
}

function isPlainEscape(e: KeyboardEvent): boolean {
  return (
    e.key === "Escape" &&
    !e.ctrlKey &&
    !e.metaKey &&
    !e.altKey &&
    !e.shiftKey
  );
}

function isSelectAllShortcut(e: KeyboardEvent): boolean {
  return (
    e.key.toLowerCase() === "a" &&
    (e.metaKey || e.ctrlKey) &&
    !e.altKey &&
    !e.shiftKey
  );
}

function consumeShortcutEvent(e: KeyboardEvent) {
  e.preventDefault();
  e.stopPropagation();
  e.stopImmediatePropagation();
}

export function ShortcutProvider({
  children,
}: {
  children: React.ReactNode;
}) {
  const listScopesRef = useRef(new Map<symbol, ListShortcutScope>());
  const overlayScopesRef = useRef(new Map<symbol, OverlayShortcutScope>());

  const registerListScope = useCallback(
    (id: symbol, scope: ListShortcutScope) => {
      listScopesRef.current.set(id, scope);
      return () => {
        listScopesRef.current.delete(id);
      };
    },
    [],
  );

  const updateListScope = useCallback(
    (id: symbol, scope: ListShortcutScope) => {
      listScopesRef.current.set(id, scope);
    },
    [],
  );

  const registerOverlayScope = useCallback(
    (id: symbol, scope: OverlayShortcutScope) => {
      overlayScopesRef.current.set(id, scope);
      return () => {
        overlayScopesRef.current.delete(id);
      };
    },
    [],
  );

  const updateOverlayScope = useCallback(
    (id: symbol, scope: OverlayShortcutScope) => {
      overlayScopesRef.current.set(id, scope);
    },
    [],
  );

  const contextValue = useMemo<ShortcutContextValue>(
    () => ({
      registerListScope,
      updateListScope,
      registerOverlayScope,
      updateOverlayScope,
    }),
    [
      registerListScope,
      updateListScope,
      registerOverlayScope,
      updateOverlayScope,
    ],
  );

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

    function getActiveListScope() {
      const scopes = Array.from(listScopesRef.current.values());
      for (let i = scopes.length - 1; i >= 0; i -= 1) {
        const scope = scopes[i];
        if (!scope.disabled) return scope;
      }
      return undefined;
    }

    function hasBlockingOverlay() {
      return Array.from(overlayScopesRef.current.values()).some(
        (scope) => scope.open && scope.blocksListShortcuts !== false,
      );
    }

    function clampedPage(scope: ListShortcutScope, page: number) {
      return Math.max(1, Math.min(scope.pages ?? 0, page));
    }

    function onKeyDown(e: KeyboardEvent) {
      if (e.defaultPrevented) return;

      const activeScope = getActiveListScope();
      if (!activeScope) return;

      const activeElement = document.activeElement;
      const blockingOverlay = hasBlockingOverlay();

      if (isTypingContext(activeElement)) {
        if (isPlainEscape(e)) {
          consumeShortcutEvent(e);
          (activeElement as HTMLElement).blur();
          clearS();
        }
        return;
      }

      if (activeScope.selectModeActive && isPlainEscape(e)) {
        if (blockingOverlay) return;
        consumeShortcutEvent(e);
        activeScope.onSelectNone?.();
        return;
      }

      if (blockingOverlay) return;

      if (isSelectAllShortcut(e)) {
        consumeShortcutEvent(e);
        activeScope.onSelectAll?.();
        return;
      }

      if (pendingS) {
        clearS();
        if (e.key === "a") {
          consumeShortcutEvent(e);
          activeScope.onSelectAll?.();
        } else if (e.key === "n") {
          consumeShortcutEvent(e);
          activeScope.onSelectNone?.();
        } else if (e.key === "i") {
          consumeShortcutEvent(e);
          activeScope.onInvertSelection?.();
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
        consumeShortcutEvent(e);
        pendingS = true;
        sTimer = setTimeout(clearS, CHORD_TIMEOUT_MS);
        return;
      }

      if (
        e.key === "f" &&
        !e.ctrlKey &&
        !e.metaKey &&
        !e.altKey &&
        !e.shiftKey
      ) {
        consumeShortcutEvent(e);
        activeScope.showEditFilter?.();
        return;
      }

      if (
        !activeScope.currentPage ||
        !activeScope.onChangePage ||
        !activeScope.pages
      ) {
        return;
      }

      if (e.key === "ArrowRight" && !e.ctrlKey && !e.metaKey && !e.altKey) {
        if (e.shiftKey) {
          consumeShortcutEvent(e);
          activeScope.onChangePage(
            clampedPage(activeScope, activeScope.currentPage + 10),
          );
        } else {
          consumeShortcutEvent(e);
          activeScope.onChangePage(
            clampedPage(activeScope, activeScope.currentPage + 1),
          );
        }
        return;
      }

      if (e.key === "ArrowLeft" && !e.ctrlKey && !e.metaKey && !e.altKey) {
        if (e.shiftKey) {
          consumeShortcutEvent(e);
          activeScope.onChangePage(
            clampedPage(activeScope, activeScope.currentPage - 10),
          );
        } else {
          consumeShortcutEvent(e);
          activeScope.onChangePage(
            clampedPage(activeScope, activeScope.currentPage - 1),
          );
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
        consumeShortcutEvent(e);
        activeScope.onChangePage(activeScope.pages);
        return;
      }

      if (
        e.key === "Home" &&
        e.ctrlKey &&
        !e.metaKey &&
        !e.altKey &&
        !e.shiftKey
      ) {
        consumeShortcutEvent(e);
        activeScope.onChangePage(1);
        return;
      }
    }

    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
      clearS();
    };
  }, []);

  return (
    <ShortcutContext.Provider value={contextValue}>
      {children}
    </ShortcutContext.Provider>
  );
}

export function useListShortcutScope(scope: ListShortcutScope) {
  const context = useContext(ShortcutContext);
  const idRef = useRef<symbol | undefined>(undefined);
  const scopeRef = useRef(scope);
  scopeRef.current = scope;
  if (!idRef.current) idRef.current = Symbol("list-shortcut-scope");

  useEffect(() => {
    if (!context) return;
    return context.registerListScope(idRef.current!, scopeRef.current);
  }, [context]);

  useEffect(() => {
    context?.updateListScope(idRef.current!, scope);
  }, [context, scope]);
}

export function useOverlayShortcutScope(scope: OverlayShortcutScope) {
  const context = useContext(ShortcutContext);
  const idRef = useRef<symbol | undefined>(undefined);
  const scopeRef = useRef(scope);
  scopeRef.current = scope;
  if (!idRef.current) idRef.current = Symbol("overlay-shortcut-scope");

  useEffect(() => {
    if (!context) return;
    return context.registerOverlayScope(idRef.current!, scopeRef.current);
  }, [context]);

  useEffect(() => {
    context?.updateOverlayScope(idRef.current!, scope);
  }, [context, scope]);
}
