import { X } from "lucide-react";
import { useIntl } from "react-intl";
import { Button } from "@/components/ui/button";

export function PlayerCloseButton({ onClose }: { onClose: () => void }) {
  const intl = useIntl();
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      data-player-close=""
      aria-label={intl.formatMessage({ id: "actions.close" })}
      className="size-11 min-w-11 shrink-0 bg-transparent text-white/80 hover:bg-transparent hover:text-white"
      onClick={(event) => {
        event.stopPropagation();
        onClose();
      }}
    >
      <X />
    </Button>
  );
}
