import { useCallback, useState } from "react";
import { useMsg } from "@/hooks/message";
import type {
  PostFilter,
  PostSummary,
  SourcePostAPI,
} from "@/core/native-archive/source-post-api";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardFooter,
} from "@/components/ui/card";
import { PostSearch } from "../search";
import { PostRows } from "../read";
import { useMergeLabels } from "./shared";

function Candidates({
  api,
  source,
  filter,
  onSelect,
}: {
  api: SourcePostAPI;
  source: string;
  filter: PostFilter;
  onSelect: (post: PostSummary) => void;
}) {
  const msg = useMsg();
  const labels = useMergeLabels();
  const load = useCallback(
    (after?: string, signal?: AbortSignal) => api.posts(filter, after, signal),
    [api, filter],
  );
  return (
    <PostRows
      load={load}
      pageLimit={api.pageLimit}
      rowKey={(p) => p.uuid}
      empty={msg("source_posts.empty", "No matching posts")}
      renderRow={(post) => (
        <Card>
          <CardHeader>
            <CardTitle className="wrap-anywhere" data-selectable-text>
              {labels.name(post)}
            </CardTitle>
            <CardDescription className="wrap-anywhere" data-selectable-text>
              {post.identifiers
                .map((i) => `${i.namespace} · ${i.value}`)
                .join("; ")}
            </CardDescription>
          </CardHeader>
          <CardFooter>
            <Button
              type="button"
              variant="outline"
              disabled={post.uuid === source || post.state !== "active"}
              onClick={() => onSelect(post)}
            >
              {post.uuid === source
                ? msg("post_merge.same_post", "This is already the same post")
                : post.state !== "active"
                  ? msg("source_posts.forgotten", "Forgotten source post")
                  : msg("post_merge.choose_target", "Keep this post")}
            </Button>
          </CardFooter>
        </Card>
      )}
    />
  );
}

export function MergePostPicker({
  api,
  source,
  onSelect,
}: {
  api: SourcePostAPI;
  source: string;
  onSelect: (post: PostSummary) => void;
}) {
  const [filter, setFilter] = useState<PostFilter>();
  return (
    <div className="flex flex-col gap-4">
      <PostSearch
        filter={{ mode: "url", value: "", namespace: "" }}
        onChange={setFilter}
      />
      {filter && (
        <Candidates
          key={JSON.stringify(filter)}
          api={api}
          source={source}
          filter={filter}
          onSelect={onSelect}
        />
      )}
    </div>
  );
}
