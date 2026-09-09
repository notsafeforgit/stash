import type { ReactNode } from "react";
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
  return (
    <MobileDetailChromeProvider>
      <div className="md:hidden flex h-10 shrink-0 items-center border-b border-border bg-background px-3">
        <h1 className="truncate text-sm font-medium">{title}</h1>
      </div>
      <div className="flex-1 min-h-0 overflow-y-auto md:overflow-hidden max-md:[container-type:size]">
        {children}
      </div>
      <MobileDetailFooter onBack={onBack} />
    </MobileDetailChromeProvider>
  );
}
