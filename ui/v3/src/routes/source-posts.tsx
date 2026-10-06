import { createFileRoute } from "@tanstack/react-router";
import { useDocumentTitle } from "@/hooks/title";
import { useMsg } from "@/hooks/message";
import { SourcePosts } from "@/components/archive/source-posts";
import { postSearchSchema } from "@/core/native-archive/source-post-api";

function SourcePostsPage() {
  const msg = useMsg();
  useDocumentTitle(msg("source_posts.title", "Source posts"));
  const { post, ...filter } = Route.useSearch();
  const navigate = Route.useNavigate();
  return (
    <SourcePosts
      key={JSON.stringify(filter)}
      filter={filter}
      selected={post}
      onFilterChange={(filter) => void navigate({ search: filter })}
      onSelect={(post) => void navigate({ search: { ...filter, post } })}
    />
  );
}
export const Route = createFileRoute("/source-posts")({
  validateSearch: postSearchSchema,
  component: SourcePostsPage,
});
