import { createFileRoute } from "@tanstack/react-router";
import { useDocumentTitle } from "@/hooks/title";
import { useMsg } from "@/hooks/message";
import { Collections } from "@/components/archive/collections";
import { collectionSearchSchema } from "@/core/native-archive/collection-api";

function CollectionsPage() {
  const msg = useMsg();
  useDocumentTitle(msg("collections.title", "Source collections"));
  const { collection, create, ...filter } = Route.useSearch();
  const navigate = Route.useNavigate();
  return (
    <Collections
      key={JSON.stringify(filter)}
      filter={filter}
      selected={collection}
      create={create}
      onFilterChange={(filter) => void navigate({ search: filter })}
      onSelect={(collection, create) =>
        void navigate({ search: { ...filter, collection, create } })
      }
    />
  );
}
export const Route = createFileRoute("/collections")({
  validateSearch: collectionSearchSchema,
  component: CollectionsPage,
});
