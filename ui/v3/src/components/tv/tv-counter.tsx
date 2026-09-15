import { FormattedNumber } from "react-intl";
import { Minus, Plus, RotateCcw } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverHeader,
  PopoverTitle,
} from "@/components/ui/popover";
import { useMsg } from "@/hooks/message";
import { cn } from "@/lib/utils";
import type { TvScene, useTvMutations } from "./use-tv-mutations";

export interface TvCounterAnchor {
  element: HTMLElement;
  entryId: string;
  side: "top" | "left" | "right";
}

export function TvCounterBadge({
  count,
  overlay = false,
}: {
  count: number;
  overlay?: boolean;
}) {
  return (
    <Badge
      variant="secondary"
      aria-hidden
      data-tv-counter-badge
      className={cn(
        "h-4 min-w-4 px-1 text-[10px] leading-none tabular-nums",
        overlay && "pointer-events-none absolute bottom-0.5 right-0.5",
      )}
    >
      {count > 9999 ? (
        <>
          <FormattedNumber value={9999} useGrouping={false} />+
        </>
      ) : (
        <FormattedNumber value={count} useGrouping={false} />
      )}
    </Badge>
  );
}

export function TvCounter({
  scene,
  anchor,
  mutations,
  close,
}: {
  scene: TvScene;
  anchor: TvCounterAnchor;
  mutations: ReturnType<typeof useTvMutations>;
  close: () => void;
}) {
  const msg = useMsg();
  const count = scene.o_counter ?? 0;
  const update = (operation: "add" | "subtract" | "reset") => {
    void mutations.run(scene.id, () => mutations.counter(scene.id, operation));
  };
  return (
    <Popover
      open
      modal={false}
      onOpenChange={(open) => {
        if (!open) close();
      }}
    >
      <PopoverContent
        anchor={anchor.element}
        side={anchor.side}
        align="end"
        finalFocus={() => anchor.element}
        // Portals still bubble React events into the player, whose pointer-up
        // handler would otherwise take focus away from these controls.
        onPointerUp={(event) => event.stopPropagation()}
        data-tv-counter
        data-tv-interactive
        data-base-ui-swipe-ignore=""
        aria-busy={mutations.busy}
        className="w-56 max-w-[calc(100vw-1.5rem)] data-open:animate-none"
      >
        <PopoverHeader>
          <PopoverTitle className="flex items-center justify-between gap-4">
            {msg("tv.action.counter", "O-counter")}
            <span aria-live="polite" className="tabular-nums">
              <FormattedNumber value={count} />
            </span>
          </PopoverTitle>
        </PopoverHeader>
        {/* Keep focus and appearance steady while writes are pending. Only
            actions that need a nonzero count should look unavailable. */}
        <div className="flex items-center gap-1">
          <Button
            variant="ghost"
            className={cn(
              "size-11 active:translate-y-0",
              count === 0 && "opacity-50",
            )}
            size="icon-lg"
            aria-label={msg("actions.decrement_o", "Decrement O")}
            disabled={mutations.busy || count === 0}
            focusableWhenDisabled
            onClick={() => update("subtract")}
          >
            <Minus />
          </Button>
          <Button
            variant="ghost"
            className="size-11 active:translate-y-0"
            size="icon-lg"
            aria-label={msg("actions.increment_o", "Add O")}
            disabled={mutations.busy}
            focusableWhenDisabled
            onClick={() => update("add")}
          >
            <Plus />
          </Button>
          <Button
            variant="ghost"
            className={cn(
              "min-h-11 active:translate-y-0",
              count === 0 && "opacity-50",
            )}
            disabled={mutations.busy || count === 0}
            focusableWhenDisabled
            onClick={() => update("reset")}
          >
            <RotateCcw data-icon="inline-start" />
            {msg("actions.reset", "Reset")}
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}
