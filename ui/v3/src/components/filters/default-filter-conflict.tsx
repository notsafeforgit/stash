import type React from "react";
import { AlertTriangle } from "lucide-react";
import { FormattedMessage } from "react-intl";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";

export const DefaultFilterConflict: React.FC<{
  disabled: boolean;
  canUseImported: boolean;
  onUseImported: () => void;
  onKeepCurrent: () => void;
}> = ({ disabled, canUseImported, onUseImported, onKeepCurrent }) => (
  <Alert role="status">
    <AlertTriangle />
    <AlertTitle>
      <FormattedMessage id="default_filter.import_conflict" />
    </AlertTitle>
    <AlertDescription>
      <div className="flex flex-wrap items-center gap-2">
        {!canUseImported && (
          <FormattedMessage id="default_filter.invalid_import" />
        )}
        <Button
          size="sm"
          variant="outline"
          disabled={disabled || !canUseImported}
          onClick={onUseImported}
        >
          <FormattedMessage id="default_filter.use_imported" />
        </Button>
        <Button
          size="sm"
          variant="outline"
          disabled={disabled}
          onClick={onKeepCurrent}
        >
          <FormattedMessage id="default_filter.keep_current" />
        </Button>
      </div>
    </AlertDescription>
  </Alert>
);
