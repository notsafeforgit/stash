import type { ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import type { PostLibraryItem } from "@/core/native-archive/source-post-api";
import { buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";

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
  const className = cn(buttonVariants({ variant: "outline" }));
  if (item.kind === "scene")
    return (
      <Link
        className={className}
        to="/scenes/$sceneId"
        params={{ sceneId: item.uuid }}
        search={sources ? { tab: "source-review" } : {}}
      >
        {children}
      </Link>
    );
  if (item.kind === "image")
    return (
      <Link
        className={className}
        to="/images/$imageId"
        params={{ imageId: item.uuid }}
        search={sources ? { tab: "source-review" } : {}}
      >
        {children}
      </Link>
    );
  return (
    <Link
      className={className}
      to="/galleries/$galleryId"
      params={{ galleryId: item.uuid }}
    >
      {children}
    </Link>
  );
}
