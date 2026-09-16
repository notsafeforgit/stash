import { useState } from "react";
import { MockedProvider } from "@apollo/client/testing/react";
import { Button } from "@/components/ui/button";
import { BulkEditSheet } from "@/components/detail/bulk-edit-sheet";
import { DestructiveConfirmDialog } from "@/components/shared/destructive-confirm-dialog";
import { PerformerSearchDialog } from "@/components/scrape/performer-search-dialog";

export function DialogDismissalFixture() {
  const [open, setOpen] = useState<"confirm" | "bulk" | "search" | null>(null);
  const [completed, setCompleted] = useState(0);
  const [applyToAll, setApplyToAll] = useState(false);
  function complete() {
    setCompleted((count) => count + 1);
    setOpen(null);
  }
  return (
    <MockedProvider mocks={[]}>
      <>
        <div className="flex flex-col items-start gap-3 p-4">
          <Button onClick={() => setOpen("confirm")}>Open confirmation</Button>
          <Button onClick={() => setOpen("bulk")}>Open bulk editor</Button>
          <Button onClick={() => setOpen("search")}>Open search</Button>
          <output data-testid="completed">{completed}</output>
        </div>
        <DestructiveConfirmDialog
          open={open === "confirm"}
          onOpenChange={(next) => setOpen(next ? "confirm" : null)}
          title="Confirm action"
          onConfirm={complete}
        >
          <p>Confirm this synthetic action.</p>
        </DestructiveConfirmDialog>
        <BulkEditSheet
          open={open === "bulk"}
          onOpenChange={(next) => setOpen(next ? "bulk" : null)}
          title="Bulk editor"
          saving={false}
          onSubmit={complete}
          itemCount={2}
          applyToAll={applyToAll}
          onApplyToAllChange={setApplyToAll}
        >
          <p className="min-h-200">Scrollable editor fields</p>
        </BulkEditSheet>
        <PerformerSearchDialog
          open={open === "search"}
          onOpenChange={(next) => setOpen(next ? "search" : null)}
          source={null}
          initialQuery=""
          onSelect={complete}
        />
      </>
    </MockedProvider>
  );
}
