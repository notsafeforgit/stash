import { createFileRoute } from "@tanstack/react-router";
import { useDocumentTitle } from "@/hooks/title";
import { useMsg } from "@/hooks/message";
import { MediaRoots } from "@/components/archive/media-roots";
import { mediaRootSearchSchema } from "@/core/native-archive/media-root-api";

function MediaRootsPage() {
  const msg = useMsg();
  useDocumentTitle(msg("media_roots.title", "Media roots"));
  const { root, create, ...filter } = Route.useSearch();
  const navigate = Route.useNavigate();
  return (
    <MediaRoots
      key={JSON.stringify(filter)}
      filter={filter}
      selected={root}
      create={create}
      onFilterChange={(filter) => void navigate({ search: filter })}
      onSelect={(root, create) =>
        void navigate({ search: { ...filter, root, create } })
      }
    />
  );
}
export const Route = createFileRoute("/media-roots")({
  validateSearch: mediaRootSearchSchema,
  component: MediaRootsPage,
});
