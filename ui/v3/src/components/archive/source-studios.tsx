import { useCallback, useMemo } from "react";
import { Link } from "@tanstack/react-router";
import { useMsg } from "@/hooks/message";
import {
  createSourceStudioAPI,
  type SourceStudio,
} from "@/core/native-archive/source-studio-api";
import { usePostRead } from "./posts/read";
import { PostReadError } from "./posts/shared";
import { Spinner } from "@/components/ui/spinner";

export function SourceStudios({
  studios,
}: {
  studios: readonly SourceStudio[];
}) {
  const msg = useMsg();
  if (!studios.length) return null;
  return (
    <div className="flex flex-col gap-2">
      <span className="text-sm text-muted-foreground">
        {msg("source_studios.title", "Source studios")}
      </span>
      <div className="flex flex-wrap gap-x-4 gap-y-2">
        {studios.map((studio) => (
          <Link
            key={studio.uuid}
            to="/studios/$studioId"
            params={{ studioId: studio.uuid }}
            className="text-primary hover:underline"
          >
            {studio.name}
          </Link>
        ))}
      </div>
    </div>
  );
}

export function PostStudios({
  post,
  endpoint,
}: {
  post: string;
  endpoint: string;
}) {
  const api = useMemo(() => createSourceStudioAPI(endpoint), [endpoint]);
  const load = useCallback(
    (signal: AbortSignal) => api.post(post, signal),
    [api, post],
  );
  const read = usePostRead(load);
  return (
    <>
      {read.busy && <Spinner />}
      {read.error !== undefined && (
        <PostReadError error={read.error} retry={read.retry} />
      )}
      {read.value && <SourceStudios studios={read.value} />}
    </>
  );
}
