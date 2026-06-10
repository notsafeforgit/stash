import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/stats")({
  component: () => <div className="p-4">Stats — coming soon</div>,
});
