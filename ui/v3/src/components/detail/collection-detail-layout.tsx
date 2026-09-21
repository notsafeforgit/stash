import { useMemo, useState, type ReactNode } from "react";
import { EmbeddedListScrollContext } from "@/components/list/list-scroll-context";
import {
  MobileDetailChromeProvider,
  MobileDetailFooter,
} from "@/components/layout/mobile-detail-chrome";

export function CollectionDetailLayout({
  title,
  onBack,
  children,
}: {
  title: string;
  onBack: () => void;
  children: ReactNode;
}) {
  const [scrollElement, setScrollElement] = useState<HTMLDivElement | null>(
    null,
  );
  const scrollContext = useMemo(
    () => ({ element: scrollElement, restorationId: "collection-detail" }),
    [scrollElement],
  );
  return (
    <MobileDetailChromeProvider>
      <div className="md:hidden flex h-10 shrink-0 items-center border-b border-border bg-background px-3">
        <h1 className="truncate text-sm font-medium">{title}</h1>
      </div>
      <div
        ref={setScrollElement}
        data-scroll-restoration-id={scrollContext.restorationId}
        className="flex-1 min-h-0 overflow-y-auto md:overflow-hidden max-md:[container-type:size]"
      >
        <EmbeddedListScrollContext value={scrollContext}>
          {children}
        </EmbeddedListScrollContext>
      </div>
      <MobileDetailFooter onBack={onBack} />
    </MobileDetailChromeProvider>
  );
}
