import { createFileRoute } from "@tanstack/react-router";
import { OfflineSceneListPage } from "src/components/offline/offline-scene-list-page";

export const Route = createFileRoute("/offline/")({
  component: OfflineSceneListPage,
});
