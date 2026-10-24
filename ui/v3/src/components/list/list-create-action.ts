import { Plus } from "lucide-react";
import type { IntlShape } from "react-intl";
import type { EntityAction } from "@/components/detail/entity-actions-menu";

export function listCreateAction(
  intl: IntlShape,
  entityType: string,
  onSelect: () => void,
): EntityAction {
  return {
    key: "create",
    icon: Plus,
    label: intl.formatMessage(
      { id: "actions.create_entity", defaultMessage: "Create {entityType}" },
      { entityType: entityType.toLocaleLowerCase(intl.locale) },
    ),
    onSelect,
  };
}
