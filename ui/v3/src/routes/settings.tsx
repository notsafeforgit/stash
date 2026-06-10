import { createFileRoute, Outlet } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { SettingsNav } from "src/components/settings/settings-nav";

function SettingsLayout() {
  const intl = useIntl();
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
  component: SettingsLayout,
});
