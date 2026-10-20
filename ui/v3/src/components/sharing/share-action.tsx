import { lazy, Suspense, useState } from "react";
import { Share2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { EntityActionItem } from "@/components/detail/entity-actions-menu";
import { useMsg } from "@/hooks/message";
import type { MatchingShareSelection } from "./share-matching-button";
import type { ShareSelection } from "./share-target-picker";

const ShareDialog = lazy(() =>
  import("./share-dialog").then((module) => ({ default: module.ShareDialog })),
);

export function useShareAction(
  targets: ShareSelection[],
  matching?: MatchingShareSelection,
) {
  const msg = useMsg();
  const [open, setOpen] = useState(false);
  const [mounted, setMounted] = useState(false);
  const action: EntityActionItem = {
    key: "share",
    icon: Share2,
    label: msg("sharing.share", "Share…"),
    disabled: targets.length === 0,
    onSelect: () => {
      setMounted(true);
      setOpen(true);
    },
  };
  const dialog = mounted ? (
    <Suspense>
      <ShareDialog
        open={open}
        onOpenChange={setOpen}
        targets={targets}
        matching={matching}
      />
    </Suspense>
  ) : null;
  return {
    action,
    dialog,
    open: () => {
      setMounted(true);
      setOpen(true);
    },
  };
}

export function ShareSelectionButton({
  targets,
  matching,
  compact = false,
}: {
  targets: ShareSelection[];
  matching?: MatchingShareSelection;
  compact?: boolean;
}) {
  const msg = useMsg();
  const share = useShareAction(targets, matching);
  return (
    <>
      <Button
        variant="ghost"
        size={compact ? "icon-lg" : "sm"}
        className={compact ? "size-11" : undefined}
        aria-label={msg("sharing.share", "Share…")}
        onClick={share.open}
        disabled={!targets.length}
      >
        <Share2 />
        {!compact && msg("sharing.share", "Share…")}
      </Button>
      {share.dialog}
    </>
  );
}
