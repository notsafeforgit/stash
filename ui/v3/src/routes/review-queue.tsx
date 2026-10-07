import { createFileRoute } from "@tanstack/react-router";
import { useMsg } from "@/hooks/message";
import { useDocumentTitle } from "@/hooks/title";
import { ReviewQueue } from "@/components/archive/review-queue";
import { reviewQueueSearchSchema } from "@/core/native-archive/review-queue-api";

function ReviewQueuePage() {
  const msg = useMsg();
  useDocumentTitle(msg("review_queue.title", "Review queue"));
  const search = Route.useSearch();
  const navigate = Route.useNavigate();
  return (
    <ReviewQueue
      key={search.kind}
      search={search}
      onChange={(search) => void navigate({ search })}
    />
  );
}

export const Route = createFileRoute("/review-queue")({
  validateSearch: reviewQueueSearchSchema,
  component: ReviewQueuePage,
});
