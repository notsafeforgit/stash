import type * as React from "react";
import {
  Pagination as PaginationRoot,
  PaginationEllipsis as PaginationEllipsisPart,
  PaginationFirst as PaginationFirstPart,
  PaginationItem as PaginationItemPart,
  PaginationLast as PaginationLastPart,
  PaginationLink as PaginationLinkPart,
  PaginationList,
  PaginationNext as PaginationNextPart,
  PaginationPrevious as PaginationPreviousPart,
} from "@ui-sdk/react/pagination";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { cn } from "@/lib/utils";

type PaginationProps = React.ComponentProps<typeof PaginationRoot>;

/** Pass `page`, `pageCount` and `onPageChange` to make its links and steps work. */
function Pagination({
  className,
  "aria-label": label = "pagination",
  ...props
}: PaginationProps) {
  return (
    <PaginationRoot
      aria-label={label}
      className={cn("mx-auto flex w-full justify-center", className)}
      {...props}
    />
  );
}

function PaginationContent({
  className,
  ...props
}: React.ComponentProps<typeof PaginationList>) {
  return (
    <PaginationList
      className={cn("flex flex-row items-center gap-1", className)}
      {...props}
    />
  );
}

function PaginationItem(
  props: React.ComponentProps<typeof PaginationItemPart>,
) {
  return <PaginationItemPart {...props} />;
}

function PaginationLink(
  props: React.ComponentProps<typeof PaginationLinkPart>,
) {
  return <PaginationLinkPart {...props} />;
}

type StepProps = Omit<
  React.ComponentProps<typeof PaginationPreviousPart>,
  "children"
> & {
  /** Visible on wide screens, next to the chevron, unless children replace it. */
  text?: string;
  children?: React.ReactNode;
};

function PaginationPrevious({
  text = "Previous",
  children,
  ...props
}: StepProps) {
  return (
    <PaginationPreviousPart aria-label="Go to previous page" {...props}>
      {children ?? (
        <>
          <ChevronLeft aria-hidden="true" />
          <span className="hidden sm:block">{text}</span>
        </>
      )}
    </PaginationPreviousPart>
  );
}

function PaginationNext({ text = "Next", children, ...props }: StepProps) {
  return (
    <PaginationNextPart aria-label="Go to next page" {...props}>
      {children ?? (
        <>
          <span className="hidden sm:block">{text}</span>
          <ChevronRight aria-hidden="true" />
        </>
      )}
    </PaginationNextPart>
  );
}

type EdgeProps = Omit<StepProps, "text">;

function PaginationFirst({ children, ...props }: EdgeProps) {
  return (
    <PaginationFirstPart aria-label="Go to first page" {...props}>
      {children}
    </PaginationFirstPart>
  );
}

function PaginationLast({ children, ...props }: EdgeProps) {
  return (
    <PaginationLastPart aria-label="Go to last page" {...props}>
      {children}
    </PaginationLastPart>
  );
}

function PaginationEllipsis({
  className,
  ...props
}: React.ComponentProps<typeof PaginationEllipsisPart>) {
  return <PaginationEllipsisPart className={className} {...props} />;
}

export {
  Pagination,
  PaginationContent,
  PaginationEllipsis,
  PaginationFirst,
  PaginationItem,
  PaginationLast,
  PaginationLink,
  PaginationNext,
  PaginationPrevious,
};
