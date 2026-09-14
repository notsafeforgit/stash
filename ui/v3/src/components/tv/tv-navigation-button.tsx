import { Menu } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useMsg } from "@/hooks/message";

export function TvNavigationButton({ onClick }: { onClick: () => void }) {
  const msg = useMsg();
  return (
    <Button
      variant="secondary"
      size="icon-lg"
      className="size-11"
      aria-label={msg("navigation", "Navigation")}
      onClick={onClick}
    >
      <Menu />
    </Button>
  );
}
