import { createFileRoute, redirect } from "@tanstack/react-router";
import { mediaRootSearchSchema } from "@/core/native-archive/media-root-api";

// Preserve bookmarks while keeping one location for folder configuration.
export const Route = createFileRoute("/media-roots")({
  validateSearch: mediaRootSearchSchema,
  beforeLoad: ({ search }) => {
    throw redirect({ to: "/settings/library", search, replace: true });
  },
});
