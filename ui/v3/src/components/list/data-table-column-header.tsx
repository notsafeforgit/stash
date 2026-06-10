import type React from "react";
import type { Column } from "@tanstack/react-table";
import { ArrowUpDown } from "lucide-react";
import { Button } from "src/components/ui/button";
import { cn } from "src/lib/utils";
import { SortDirectionEnum } from "src/core/generated-graphql";
import { getSortDirectionIcon } from "./sort-icon";

interface DataTableColumnHeaderProps<TData, TValue>
  extends React.HTMLAttributes<HTMLDivElement> {
  column: Column<TData, TValue>;
  title: string;
}

export function DataTableColumnHeader<TData, TValue>({
  column,
  title,
  className,
}: DataTableColumnHeaderProps<TData, TValue>) {
  if (!column.getCanSort()) {
    return <div className={cn(className)}>{title}</div>;
  }

  const sorted = column.getIsSorted();
  const SortDirIcon = sorted
    ? getSortDirectionIcon(
        column.id,
        sorted === "asc" ? SortDirectionEnum.Asc : SortDirectionEnum.Desc,
      )
    : null;

  return (
    <div className={cn("flex items-center gap-1", className)}>
      <Button
        variant="ghost"
        size="sm"
        className="-ml-3 h-8 data-[state=open]:bg-accent"
        onClick={column.getToggleSortingHandler()}
      >
        <span>{title}</span>
        {SortDirIcon ? (
          <SortDirIcon size={13} />
        ) : (
          <ArrowUpDown size={13} className="text-muted-foreground" />
        )}
      </Button>
    </div>
  );
}
