import { X } from "lucide-react";
import { useIntl } from "react-intl";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

export function PlayerCloseButton({
  onClose,
  subtle = false,
}: {
  onClose: () => void;
  subtle?: boolean;
}) {
  const intl = useIntl();
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      data-player-close=""
      aria-label={intl.formatMessage({ id: "actions.close" })}
      className={cn(
        "pointer-events-auto size-11 min-w-11 shrink-0 bg-transparent text-white/80 drop-shadow-[0_1px_2px_black] transition-colors duration-300 hover:bg-transparent hover:text-white motion-reduce:transition-none",
        subtle && "text-white/45",
      )}
      onClick={(event) => {
        event.stopPropagation();
        onClose();
      }}
    >
      <X />
    </Button>
  );
}
