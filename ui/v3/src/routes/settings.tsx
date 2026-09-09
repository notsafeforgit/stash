import { useEffect } from "react";
import {
  createFileRoute,
  Outlet,
  useRouterState,
} from "@tanstack/react-router";
import { z } from "zod";
import { useIntl } from "react-intl";
import { SettingsLayout } from "@/components/settings/settings-layout";
import { getSettingsSection } from "@/components/settings/settings-navigation";
import { useDocumentTitle } from "src/hooks/title";

const searchSchema = z.object({
  /** Resolved label text of a setting row to scroll to and flash —
   * set by the settings search box, inherited by all child pages. */
  hl: z.string().optional(),
});

function SettingsPage() {
  const intl = useIntl();
  const { hl } = Route.useSearch();
  const pathname = useRouterState({ select: (s) => s.location.pathname });

  // "<Section> | Settings | Stash". Match the longest nav route that the
  // current path falls under (e.g. /settings/system → System).
  const section = getSettingsSection(pathname);
  useDocumentTitle(
    section
      ? intl.formatMessage({
          id: section.labelId,
          defaultMessage: section.defaultLabel,
        })
      : undefined,
    intl.formatMessage({ id: "settings", defaultMessage: "Settings" }),
  );

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
    <SettingsLayout>
      <Outlet />
    </SettingsLayout>
  );
}

export const Route = createFileRoute("/settings")({
  validateSearch: searchSchema,
  component: SettingsPage,
});
