import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useRouter } from "@tanstack/react-router";
import { holdRouteMotion } from "@/core/route-transitions";
import { MobileNavSheet } from "./mobile-nav-sheet";

const MobileNavigationContext = createContext<(() => void) | null>(null);

/** One navigation drawer survives list/detail/Home changes. Page toolbars only
 * open it, so rapid navigation cannot leave several closing backdrops alive. */
export function MobileNavigationProvider({
  children,
}: {
  children: ReactNode;
}) {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const releaseMotion = useRef<(() => void) | undefined>(undefined);
  const openNavigation = useCallback(() => {
    releaseMotion.current ??= holdRouteMotion(router);
    setOpen(true);
  }, [router]);
  const onOpenChangeComplete = useCallback((isOpen: boolean) => {
    if (isOpen) return;
    releaseMotion.current?.();
    releaseMotion.current = undefined;
  }, []);
  useEffect(() => () => releaseMotion.current?.(), []);

  return (
    <MobileNavigationContext value={openNavigation}>
      {children}
      <MobileNavSheet
        open={open}
        onOpenChange={setOpen}
        onOpenChangeComplete={onOpenChangeComplete}
      />
    </MobileNavigationContext>
  );
}

export function useMobileNavigation() {
  const openNavigation = useContext(MobileNavigationContext);
  if (!openNavigation)
    throw new Error("Mobile navigation requires the app shell provider");
  return openNavigation;
}
