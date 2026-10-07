import { createFileRoute } from "@tanstack/react-router";
import { useDocumentTitle } from "@/hooks/title";
import { useMsg } from "@/hooks/message";
import { ArchiveActivity } from "@/components/archive/activity";
import { activitySearchSchema } from "@/core/native-archive/activity-schema";

function ArchiveActivityPage() {
  const msg = useMsg();
  useDocumentTitle(msg("archive_activity.title", "Archive activity"));
  const search = Route.useSearch();
  const { item: _, ...filter } = search;
  const navigate = Route.useNavigate();
  return (
    <ArchiveActivity
      key={JSON.stringify(filter)}
      search={search}
      onChange={(search) => void navigate({ search })}
    />
  );
}

export const Route = createFileRoute("/archive-activity")({
  validateSearch: activitySearchSchema,
  component: ArchiveActivityPage,
});
