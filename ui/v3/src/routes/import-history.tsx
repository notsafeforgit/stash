import { createFileRoute } from "@tanstack/react-router";
import { useMsg } from "@/hooks/message";
import { useDocumentTitle } from "@/hooks/title";
import { ImportHistory } from "@/components/archive/import-history";
import { importHistorySearchSchema } from "@/core/native-archive/import-history-api";

function ImportHistoryPage() {
  const msg = useMsg();
  useDocumentTitle(msg("import_history.title", "Import history"));
  const search = Route.useSearch();
  const navigate = Route.useNavigate();
  return (
    <ImportHistory
      key={search.kind}
      search={search}
      onChange={(search) => void navigate({ search })}
    />
  );
}

export const Route = createFileRoute("/import-history")({
  validateSearch: importHistorySearchSchema,
  component: ImportHistoryPage,
});
