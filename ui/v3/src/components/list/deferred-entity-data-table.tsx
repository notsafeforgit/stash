import { Suspense } from "react";
import { Spinner } from "@/components/ui/spinner";
import { lazyModule } from "@/utils/lazy-module";
import type { IHasID } from "@/utils/data";
import type { EntityDataTableProps } from "./entity-data-table";

const TableModule = lazyModule(() => import("./entity-data-table"));

export function EntityDataTable<TItem extends IHasID>(
  props: EntityDataTableProps<TItem>,
) {
  return (
    <Suspense
      fallback={
        <div className="flex justify-center p-8">
          <Spinner />
        </div>
      }
    >
      <TableModule>
        {({ EntityDataTable: Table }) => <Table {...props} />}
      </TableModule>
    </Suspense>
  );
}
