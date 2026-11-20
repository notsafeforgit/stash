import type { ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import type { PostLibraryItem } from "@/core/native-archive/source-post-api";
import { Button } from "@/components/ui/button";

export function LibraryLink({
  item,
  children,
  sources = false,
}: {
  item: Pick<PostLibraryItem, "kind" | "state" | "uuid"> &
    Partial<PostLibraryItem>;
  children: ReactNode;
  sources?: boolean;
}) {
  if (item.state !== "active") return null;
  if (item.kind === "scene")
    return (
      <Button
        variant="outline"
        render={
          <Link
            to="/scenes/$sceneId"
            params={{ sceneId: item.uuid }}
            search={sources ? { tab: "source-review" } : {}}
          />
        }
      >
        {children}
      </Button>
    );
  if (item.kind === "image")
    return (
      <Button
        variant="outline"
        render={
          <Link
            to="/images/$imageId"
            params={{ imageId: item.uuid }}
            search={sources ? { tab: "source-review" } : {}}
          />
        }
      >
        {children}
      </Button>
    );
  return (
    <Button
      variant="outline"
      render={
        <Link to="/galleries/$galleryId" params={{ galleryId: item.uuid }} />
      }
    >
      {children}
    </Button>
  );
}
