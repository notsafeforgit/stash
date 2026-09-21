import { createFileRoute } from "@tanstack/react-router";
import { SharesSettings } from "@/components/sharing/shares-settings";

export const Route = createFileRoute("/settings/shares")({
  component: SharesSettings,
});
