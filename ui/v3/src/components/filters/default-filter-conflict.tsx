import type React from "react";
import { AlertTriangle } from "lucide-react";
import { FormattedMessage } from "react-intl";
import { Button } from "src/components/ui/button";

export const DefaultFilterConflict: React.FC<{
  disabled: boolean;
  onUseLegacy: () => void;
  onKeepV3: () => void;
}> = ({ disabled, onUseLegacy, onKeepV3 }) => (
  <div
    role="status"
    className="flex w-full flex-wrap items-center gap-2 border-l-2 border-amber-500 bg-amber-500/10 px-3 py-2 text-sm"
  >
    <AlertTriangle className="size-4 shrink-0 text-amber-600 dark:text-amber-300" />
    <span className="mr-auto">
      <FormattedMessage
        id="default_filter.version_conflict"
        defaultMessage="This default was changed in v2.5. Choose which version to keep."
      />
    </span>
    <Button
      size="sm"
      variant="outline"
      disabled={disabled}
      onClick={onUseLegacy}
    >
      <FormattedMessage id="default_filter.use_v25" defaultMessage="Use v2.5" />
    </Button>
    <Button size="sm" variant="outline" disabled={disabled} onClick={onKeepV3}>
      <FormattedMessage id="default_filter.keep_v3" defaultMessage="Keep v3" />
    </Button>
  </div>
);
