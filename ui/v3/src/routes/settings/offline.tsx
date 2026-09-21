import { createFileRoute } from "@tanstack/react-router";
import { OfflineSettingsPage } from "@/components/offline/offline-settings-section";

export const Route = createFileRoute("/settings/offline")({
  component: OfflineSettingsPage,
});
