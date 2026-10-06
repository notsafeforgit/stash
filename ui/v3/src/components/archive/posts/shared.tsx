import type { ReactNode } from "react";
import { useState } from "react";
import { ChevronDown } from "lucide-react";
import { useMsg } from "@/hooks/message";
import { NativeArchiveError } from "@/core/native-archive/client";
import { Button } from "@/components/ui/button";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import {
  Collapsible,
  CollapsibleTrigger,
  CollapsibleContent,
} from "@/components/ui/collapsible";
import {
  Empty,
  EmptyHeader,
  EmptyTitle,
  EmptyDescription,
} from "@/components/ui/empty";

export function PostReadError({
  error,
  retry,
}: {
  error: unknown;
  retry: () => void;
}) {
  const msg = useMsg();
  const missing = error instanceof NativeArchiveError && error.status === 404;
  const oversized =
    error instanceof NativeArchiveError && error.code === "source_review_limit";
  return (
    <Alert variant="destructive">
      <AlertTitle>
        {missing
          ? msg("source_posts.missing", "This post is unavailable")
          : msg("source_posts.failed", "Could not load post data")}
      </AlertTitle>
      <AlertDescription>
        <p>
          {oversized
            ? msg(
                "source_posts.limit",
                "This post has too many media associations to display together. You can inspect its sources from an individual scene or image.",
              )
            : msg(
                "source_posts.failed_help",
                "Check your connection and access to Stash, then retry.",
              )}
        </p>
        <Button variant="outline" size="sm" onClick={retry}>
          {msg("actions.retry", "Retry")}
        </Button>
      </AlertDescription>
    </Alert>
  );
}

export function PostSection({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger
        render={
          <Button
            type="button"
            variant="outline"
            className="w-full justify-between"
          />
        }
      >
        {title}
        <ChevronDown data-icon="inline-end" />
      </CollapsibleTrigger>
      <CollapsibleContent className="pt-4">
        {open && children}
      </CollapsibleContent>
    </Collapsible>
  );
}

export function PostEmpty({
  title,
  children,
}: {
  title: string;
  children?: ReactNode;
}) {
  return (
    <Empty>
      <EmptyHeader>
        <EmptyTitle>{title}</EmptyTitle>
        {children && <EmptyDescription>{children}</EmptyDescription>}
      </EmptyHeader>
    </Empty>
  );
}

export function PostURL({ value }: { value: string }) {
  let href: string | undefined;
  try {
    const parsed = new URL(value);
    if (
      /^https?:$/.test(parsed.protocol) &&
      !parsed.username &&
      !parsed.password
    )
      href = parsed.href;
  } catch {
    /* Retained non-web values are readable, never executable. */
  }
  return href ? (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      data-selectable-text
      className="wrap-anywhere text-sm underline underline-offset-4"
    >
      {value}
    </a>
  ) : (
    <span data-selectable-text className="wrap-anywhere text-sm">
      {value}
    </span>
  );
}
