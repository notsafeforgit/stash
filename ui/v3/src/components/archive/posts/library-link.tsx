import type { ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import type { PostLibraryItem } from "@/core/native-archive/source-post-api";
import { buttonVariants } from "@/components/ui/button";

export function LibraryLink({
  item,
  children,
  sources = false,
}: {
  item: PostLibraryItem;
  children: ReactNode;
  sources?: boolean;
}) {
  if (item.state !== "active" || item.local_id === null) return null;
  const className = buttonVariants({ variant: "outline" });
  if (item.kind === "scene")
    return (
      <Link
        className={className}
        to="/scenes/$sceneId"
        params={{ sceneId: String(item.local_id) }}
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
        params={{ imageId: String(item.local_id) }}
        search={sources ? { tab: "source-review" } : {}}
      >
        {children}
      </Link>
    );
  return (
    <Link
      className={className}
      to="/galleries/$galleryId"
      params={{ galleryId: String(item.local_id) }}
    >
      {children}
    </Link>
  );
}
