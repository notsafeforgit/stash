import { Plus } from "lucide-react";
import { useIntl } from "react-intl";
import { Button } from "@/components/ui/button";

/** One create action for the desktop toolbar and compact mobile footer. */
export function ListCreateButton({
  entityType,
  onClick,
}: {
  entityType: string;
  onClick: () => void;
}) {
  const intl = useIntl();
  const label = intl.formatMessage(
    { id: "actions.create_entity", defaultMessage: "Create {entityType}" },
    { entityType: entityType.toLocaleLowerCase(intl.locale) },
  );
  return (
    <Button
      size="icon-lg"
      className="size-11 md:h-7 md:w-auto md:gap-1 md:px-2.5"
      aria-label={label}
      title={label}
      onClick={onClick}
    >
      <Plus data-icon="inline-start" />
      <span className="hidden md:inline">
        {intl.formatMessage({ id: "actions.new", defaultMessage: "New" })}
      </span>
    </Button>
  );
}
