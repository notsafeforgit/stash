import type { ReactNode } from "react";
import { useIntl } from "react-intl";
import { SettingsNav } from "./settings-nav";

/** The navigation sits beside the scroller on desktop and below it on mobile. */
export function SettingsLayout({ children }: { children: ReactNode }) {
  const intl = useIntl();
  const title = intl.formatMessage({
    id: "settings",
    defaultMessage: "Settings",
  });
  return (
    <div className="flex h-full min-h-0 flex-1 flex-col md:flex-row">
      <header className="flex h-11 shrink-0 items-center border-b px-4 md:hidden">
        <h1 className="truncate font-semibold">{title}</h1>
      </header>
      <div className="order-last shrink-0 md:order-none md:flex md:flex-col md:p-6 md:pr-0">
        <h1 className="hidden md:mb-4 md:block md:text-xl md:font-semibold">
          {title}
        </h1>
        <SettingsNav />
      </div>
      <div data-settings-scroll className="min-h-0 flex-1 overflow-y-auto">
        {children}
      </div>
    </div>
  );
}
