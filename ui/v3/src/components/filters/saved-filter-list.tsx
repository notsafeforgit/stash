import { Label } from "@/components/ui/label";
import type React from "react";
import { useId, useMemo, useState } from "react";
import { Dialog } from "@base-ui/react";
import { FormattedMessage, useIntl } from "react-intl";
import { Button } from "src/components/ui/button";
import { Input } from "src/components/ui/input";
import type { FilterMode } from "src/core/generated-graphql";
import { useFindSavedFilters } from "src/core/saved-filters";

export const SaveFilterDialog: React.FC<{
  mode: FilterMode;
  onClose: (name?: string, id?: string) => void;
  isSaving?: boolean;
}> = ({ mode, onClose, isSaving = false }) => {
  const intl = useIntl();
  const nameId = useId();
  const [filterName, setFilterName] = useState("");

  const { data } = useFindSavedFilters(mode);

  const overwritingFilter = useMemo(() => {
    const savedFilters = data?.findSavedFilters ?? [];
    return savedFilters.find(
      (f) => f.name.toLowerCase() === filterName.toLowerCase(),
    );
  }, [data?.findSavedFilters, filterName]);

  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-50 bg-black/50" />
        <Dialog.Popup className="fixed left-1/2 top-1/2 z-50 w-full max-w-md -translate-x-1/2 -translate-y-1/2 rounded-lg bg-background p-6 shadow-lg">
          <Dialog.Title className="mb-4 text-lg font-semibold">
            <FormattedMessage id="actions.save_filter" />
          </Dialog.Title>

          <div className="space-y-4">
            <div>
              <Label
                htmlFor={nameId}
                className="mb-1 block text-sm font-medium"
              >
                <FormattedMessage id="filter_name" />
              </Label>
              <Input
                id={nameId}
                type="text"
                placeholder={`${intl.formatMessage({ id: "filter_name" })}…`}
                value={filterName}
                onChange={(e) => setFilterName(e.target.value)}
                disabled={isSaving}
                autoFocus
              />
            </div>

            {(data?.findSavedFilters ?? []).length > 0 && (
              <ul className="max-h-40 overflow-y-auto rounded border border-input text-sm">
                {(data?.findSavedFilters ?? []).map((f) => (
                  <li key={f.id}>
                    <Button
                      variant="ghost"
                      className="w-full justify-start h-auto rounded-none px-3 py-1.5 font-normal hover:bg-accent"
                      onClick={() => setFilterName(f.name)}
                    >
                      {f.name}
                    </Button>
                  </li>
                ))}
              </ul>
            )}

            {!!overwritingFilter && (
              <p className="text-sm text-yellow-500">
                <FormattedMessage
                  id="dialogs.overwrite_filter_warning"
                  values={{ entityName: overwritingFilter.name }}
                />
              </p>
            )}
          </div>

          <div className="mt-6 flex justify-end gap-2">
            <Button
              variant="outline"
              onClick={() => onClose()}
              disabled={isSaving}
            >
              <FormattedMessage id="actions.cancel" />
            </Button>
            <Button
              onClick={() => onClose(filterName, overwritingFilter?.id)}
              disabled={isSaving || !filterName.trim()}
            >
              {isSaving ? (
                <FormattedMessage
                  id="actions.saving"
                  defaultMessage="Saving…"
                />
              ) : (
                <FormattedMessage id="actions.save" />
              )}
            </Button>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
};
