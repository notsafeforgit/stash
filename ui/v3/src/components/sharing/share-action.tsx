import { lazy, Suspense, useState } from "react";
import { Share2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { EntityActionItem } from "@/components/detail/entity-actions-menu";
import { useMsg } from "@/hooks/message";
import type {
  MatchingShareSelection,
  ShareSelection,
  ShareEntitySource,
  ShareScope,
} from "./share-source";
import { useCollectionShareContext } from "./collection-share-context";

const ShareDialog = lazy(() =>
  import("./share-dialog").then((module) => ({ default: module.ShareDialog })),
);

export function useShareAction(
  targets: ShareSelection[],
  matching?: MatchingShareSelection,
  entity?: ShareEntitySource,
) {
  const msg = useMsg();
  const collection = useCollectionShareContext();
  const [open, setOpen] = useState(false);
  const [source, setSource] = useState<{
    targets: ShareSelection[];
    matching?: MatchingShareSelection;
    entity?: ShareEntitySource;
    initialScope: ShareScope;
  }>();
  const openShare = () => {
    const sameCollection =
      !entity ||
      (entity.kind === collection?.entity.kind &&
        entity.id === collection.entity.id);
    const inherited = sameCollection ? collection : undefined;
    const list =
      !targets.length && !matching ? inherited?.readList() : undefined;
    const selected = list?.targets ?? targets;
    const filtered = matching ?? list?.matching;
    const owner = entity ?? inherited?.entity;
    setSource({
      targets: selected,
      matching: filtered && {
        ...filtered,
        filter: filtered.filter.clone(),
      },
      entity: owner,
      initialScope: selected.length
        ? "selection"
        : matching
          ? "filter"
          : owner
            ? "entity"
            : "filter",
    });
    setOpen(true);
  };
  const action: EntityActionItem = {
    key: "share",
    icon: Share2,
    label: msg("sharing.share", "Share…"),
    disabled: targets.length === 0 && !matching && !entity,
    onSelect: openShare,
  };
  const dialog = source ? (
    <Suspense>
      <ShareDialog open={open} onOpenChange={setOpen} {...source} />
    </Suspense>
  ) : null;
  return {
    action,
    dialog,
    open: openShare,
  };
}

export function ShareSelectionButton({
  onShare,
  compact = false,
}: {
  onShare: () => void;
  compact?: boolean;
}) {
  const msg = useMsg();
  return (
    <Button
      variant="ghost"
      size={compact ? "icon-lg" : "sm"}
      className={compact ? "size-11" : undefined}
      aria-label={msg("sharing.share", "Share…")}
      onClick={onShare}
    >
      <Share2 />
      {!compact && msg("sharing.share", "Share…")}
    </Button>
  );
}
