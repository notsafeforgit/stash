import { createFileRoute } from "@tanstack/react-router";
import { useDocumentTitle } from "@/hooks/title";
import { useMsg } from "@/hooks/message";
import { SavedActions } from "@/components/archive/saved-actions";
import { savedActionSearchSchema } from "@/core/native-archive/saved-actions";

function SavedActionsPage() {
  const msg = useMsg();
  useDocumentTitle(msg("saved_actions.title", "Saved actions"));
  const { family } = Route.useSearch();
  const navigate = Route.useNavigate();
  return (
    <SavedActions
      key={family}
      family={family}
      onFamilyChange={(family) => void navigate({ search: { family } })}
    />
  );
}
export const Route = createFileRoute("/saved-actions")({
  validateSearch: savedActionSearchSchema,
  component: SavedActionsPage,
});
