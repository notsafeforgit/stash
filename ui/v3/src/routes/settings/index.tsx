import { createFileRoute, redirect } from "@tanstack/react-router";

// Mirror v2.5, where the default settings tab is Tasks.
export const Route = createFileRoute("/settings/")({
  beforeLoad: () => {
    throw redirect({ to: "/settings/tasks" });
  },
});
