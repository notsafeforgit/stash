import {
  createContext,
  type KeyboardEvent,
  type ReactNode,
  type RefObject,
  useContext,
  useEffect,
  useState,
  useSyncExternalStore,
} from "react";

const OverlayContainer = createContext<
  RefObject<HTMLElement | null> | undefined
>(undefined);
/** A presentation owner can keep all nested menus and dialogs inside its
 * fullscreen surface without changing ordinary application portals. */
export function OverlayContainerProvider({
  container,
  children,
}: {
  container: RefObject<HTMLElement | null>;
  children: ReactNode;
}) {
  return (
    <OverlayContainer.Provider value={container}>
      {children}
    </OverlayContainer.Provider>
  );
}
export function useOverlayContainer() {
  return useContext(OverlayContainer);
}

const subscribeToBody = () => () => {};
const clientBody = () => document.body;
const serverBody = () => null;

// Popups render into the body, as Base UI's portals did, so a table, link or card
// never contains their DOM, styles or native listeners. Server rendering keeps them
// in place. A presentation owner's ref attaches during commit, so it is read after
// mount; until then, in place.
export function useOverlayContainerElement() {
  const overlay = useContext(OverlayContainer);
  const body = useSyncExternalStore(subscribeToBody, clientBody, serverBody);
  const [element, setElement] = useState<HTMLElement | null>(null);
  useEffect(() => {
    if (overlay) setElement(overlay.current);
  }, [overlay]);
  return overlay ? element : body;
}

// Base UI kept Escape inside an open popup. ui-sdk lets it bubble through React
// to handlers around the popup, such as YARL's, which would close with it.
export function containEscape<Event extends KeyboardEvent>(
  handler?: (event: Event) => void,
) {
  return (event: Event) => {
    handler?.(event);
    if (event.key === "Escape") event.stopPropagation();
  };
}

type FocusTarget =
  | "auto"
  | "content"
  | "none"
  | "trigger"
  | (string & {})
  | HTMLElement
  | (() => HTMLElement | null | undefined);

/** Base UI took `false` or a ref for focus. ui-sdk takes keywords, elements,
 * selectors or functions, so refs become functions and `false` means "none". */
export type OverlayFocus =
  | FocusTarget
  | boolean
  | RefObject<HTMLElement | null>;

export function overlayFocus(
  value: OverlayFocus | undefined,
): FocusTarget | undefined {
  if (value === false) return "none";
  if (value === true) return undefined;
  if (value !== undefined && typeof value === "object" && "current" in value) {
    const ref = value;
    return () => ref.current;
  }
  return value;
}

export type OpenChangeArgs<Props> = Props extends {
  onOpenChange?: infer Handler;
}
  ? NonNullable<Handler> extends (open: boolean, ...args: infer Args) => void
    ? Args
    : never
  : never;
