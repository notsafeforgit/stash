import { useState, type RefObject } from "react";
import { useIntl } from "react-intl";
import { Pencil, Search, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuGroup,
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";

/** Tap to type; the primitive's long press previews the active query. */
export function MobileSearchButton({
  query,
  buttonRef,
  inputRef,
  onOpen,
  onClear,
}: {
  query: string | undefined;
  buttonRef: RefObject<HTMLButtonElement | null>;
  inputRef: RefObject<HTMLInputElement | null>;
  onOpen: () => void;
  onClear: () => void;
}) {
  const intl = useIntl();
  const [previewOpen, setPreviewOpen] = useState(false);
  const summary = query
    ? intl.formatMessage({ id: "list.active_search" }, { query })
    : undefined;

  return (
    <ContextMenu
      open={previewOpen}
      onOpenChange={setPreviewOpen}
      disabled={!query}
    >
      <ContextMenuTrigger
        render={
          <Button
            ref={buttonRef}
            variant={query ? "secondary" : "ghost"}
            size="icon-lg"
            className="size-11 shrink-0"
            aria-label={intl.formatMessage({ id: "search" })}
            aria-description={summary}
            onClick={() => {
              // Releasing a long press must not also open the keyboard.
              if (!previewOpen) onOpen();
            }}
          />
        }
      >
        <Search />
      </ContextMenuTrigger>
      <ContextMenuContent
        anchor={buttonRef}
        side="top"
        align="center"
        alignOffset={0}
        sideOffset={8}
        className="w-72 max-w-[calc(100vw-1.5rem)]"
        // Editing replaces the trigger with an input. Preserve its focus when
        // the menu unmounts instead of restoring focus to the removed button.
        finalFocus={() => inputRef.current ?? buttonRef.current}
      >
        <ContextMenuGroup>
          <ContextMenuLabel className="whitespace-pre-wrap wrap-anywhere">
            {summary}
          </ContextMenuLabel>
          <ContextMenuItem className="min-h-11" onClick={onOpen}>
            <Pencil />
            {intl.formatMessage({ id: "actions.edit_search" })}
          </ContextMenuItem>
          <ContextMenuItem className="min-h-11" onClick={onClear}>
            <X />
            {intl.formatMessage({ id: "actions.clear_search" })}
          </ContextMenuItem>
        </ContextMenuGroup>
      </ContextMenuContent>
    </ContextMenu>
  );
}
