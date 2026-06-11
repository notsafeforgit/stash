/**
 * Internal link to a list page with a pre-built filter query string
 * (a `NavUtils.make*Url` href). Renders a real anchor so middle-click
 * and open-in-new-tab work, but plain clicks push the raw href through
 * router history: filter URLs carry pre-encoded `c=` criteria that
 * TanStack Router's path machinery (`Link to`) would percent-decode
 * and its search codec (`navigate({ href })`) would re-stringify
 * lossily — same reason use-filter-state pushes raw strings.
 */
import type React from "react";
import { useRouter } from "@tanstack/react-router";
import { cn } from "src/lib/utils";

export function FilterUrlLink({
  href,
  title,
  className,
  children,
}: {
  href: string;
  title?: string;
  className?: string;
  children: React.ReactNode;
}) {
  const router = useRouter();
  return (
    <a
      href={href}
      title={title}
      className={cn("text-primary hover:underline", className)}
      onClick={(e) => {
        // Modified clicks (new tab / window) keep browser behaviour.
        if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
        e.preventDefault();
        router.history.push(href);
      }}
    >
      {children}
    </a>
  );
}
