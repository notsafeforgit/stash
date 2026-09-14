import { createFileRoute } from "@tanstack/react-router";
import { SettingsTvPage } from "@/components/tv/tv-settings";

export const Route = createFileRoute("/settings/tv")({
  component: SettingsTvPage,
});
