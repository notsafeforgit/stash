import { SourceTrackingSettings } from "@/components/settings/source-tracking-settings";
import { createFileRoute } from "@tanstack/react-router";

function SettingsServicesPage() {
  return (
    <div className="max-w-3xl space-y-8 p-6">
      <SourceTrackingSettings />
    </div>
  );
}

export const Route = createFileRoute("/settings/services")({
  component: SettingsServicesPage,
});
