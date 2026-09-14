import {
  createContext,
  useContext,
  type ReactNode,
  type RefObject,
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
