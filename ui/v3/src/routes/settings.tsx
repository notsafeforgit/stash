import { useEffect } from "react";
import { createFileRoute, Outlet } from "@tanstack/react-router";
import { zodValidator } from "@tanstack/zod-adapter";
import { z } from "zod";
import { useIntl } from "react-intl";
import { SettingsNav } from "src/components/settings/settings-nav";

const searchSchema = z.object({
  /** Resolved label text of a setting row to scroll to and flash —
   * set by the settings search box, inherited by all child pages. */
  hl: z.string().optional(),
});

function SettingsLayout() {
  const intl = useIntl();
  const { hl } = Route.useSearch();

  // Find the row whose label matches the `hl` search param, scroll it
  // into view, and flash it. Text-matching against the rendered label
  // keeps the Setting* row components free of per-row id plumbing; the
  // search box resolves the label with the same locale, so the strings
  // agree.
  useEffect(() => {
    if (!hl) return;
    const timeout = window.setTimeout(() => {
      const labels = document.querySelectorAll<HTMLElement>(
        '[data-slot="field-label"], [data-slot="field-legend"]',
      );
      const target = Array.from(labels).find(
        (el) => el.textContent?.trim() === hl,
      );
      if (!target) return;
      const row = target.closest<HTMLElement>('[data-slot="field"]') ?? target;
      row.scrollIntoView({ block: "center", behavior: "smooth" });
      row.classList.add("ring-2", "ring-primary", "rounded-md");
      window.setTimeout(() => {
        row.classList.remove("ring-2", "ring-primary", "rounded-md");
      }, 2000);
    }, 150);
    return () => window.clearTimeout(timeout);
  }, [hl]);

  return (
    <div className="flex h-full min-h-0 flex-1 flex-col md:flex-row">
      <div className="md:flex md:flex-col md:p-6 md:pr-0">
        <h1 className="hidden md:mb-4 md:block md:text-xl md:font-semibold">
          {intl.formatMessage({ id: "settings", defaultMessage: "Settings" })}
        </h1>
        <SettingsNav />
      </div>
      <div className="flex-1 overflow-y-auto">
        <Outlet />
      </div>
    </div>
  );
}

export const Route = createFileRoute("/settings")({
  validateSearch: zodValidator(searchSchema),
  component: SettingsLayout,
});
