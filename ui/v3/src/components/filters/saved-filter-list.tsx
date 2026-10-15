import { useId } from "react";
import { useForm, useStore } from "@tanstack/react-form";
import { FormattedMessage, useIntl } from "react-intl";
import { z } from "zod";
import { Button } from "@/components/ui/button";
import {
  Combobox,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
  useComboboxFilter,
} from "@/components/ui/combobox";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import type { FilterMode } from "@/core/generated-graphql";
import { useFindSavedFilters } from "@/core/saved-filters";

export function SaveFilterDialog({
  mode,
  onClose,
  isSaving = false,
}: {
  mode: FilterMode;
  onClose: (name?: string, id?: string) => void;
  isSaving?: boolean;
}) {
  const intl = useIntl();
  const nameId = useId();
  const hintId = useId();
  const statusId = useId();
  const { contains } = useComboboxFilter({ locale: intl.locale });
  const { data, previousData } = useFindSavedFilters(mode);
  const savedFilters =
    data?.findSavedFilters ?? previousData?.findSavedFilters ?? [];
  const form = useForm({
    defaultValues: { name: "", selectedId: null as string | null },
    validators: {
      onChange: z.object({
        name: z.string().trim().min(1),
        selectedId: z.string().nullable(),
      }),
    },
    onSubmit: ({ value }) => {
      if (isSaving) return;
      const selected = savedFilters.find(
        (filter) =>
          filter.id === value.selectedId && filter.name === value.name,
      );
      if (!selected && namedFilter(value.name)) return;
      onClose(value.name.trim(), selected?.id);
    },
  });
  const values = useStore(form.store, (state) => state.values);
  const selected =
    savedFilters.find((filter) => filter.id === values.selectedId) ?? null;
  const filteredItems = savedFilters.filter((filter) =>
    contains(filter.name, values.name.trim()),
  );
  function namedFilter(name: string) {
    return savedFilters.find(
      (filter) =>
        filter.name.toLocaleLowerCase(intl.locale) ===
        name.trim().toLocaleLowerCase(intl.locale),
    );
  }
  const nameTaken = !selected && !!namedFilter(values.name);

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !isSaving) onClose();
      }}
    >
      <DialogContent
        showCloseButton={false}
        className="h-[min(32rem,calc(100dvh-2rem))] grid-rows-[auto_minmax(0,1fr)] overflow-hidden"
      >
        <DialogHeader>
          <DialogTitle>
            <FormattedMessage id="actions.save_filter" />
          </DialogTitle>
        </DialogHeader>
        <form
          className="flex min-h-0 flex-col gap-4"
          onSubmit={(event) => {
            event.preventDefault();
            void form.handleSubmit();
          }}
        >
          <FieldGroup className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
            <Field data-disabled={isSaving} data-invalid={nameTaken}>
              <FieldLabel htmlFor={nameId}>
                <FormattedMessage id="filter_name" />
              </FieldLabel>
              <Combobox
                inline
                open
                items={savedFilters}
                filteredItems={filteredItems}
                value={selected}
                inputValue={values.name}
                itemToStringLabel={(filter) => filter.name}
                itemToStringValue={(filter) => filter.id}
                isItemEqualToValue={(filter, value) => filter.id === value.id}
                onValueChange={(filter) => {
                  if (!filter) return;
                  form.setFieldValue("name", filter.name);
                  form.setFieldValue("selectedId", filter.id);
                }}
                onInputValueChange={(name, details) => {
                  // Popup/selection resets must not erase a freely typed name.
                  // Only an explicit item selection authorizes an overwrite.
                  if (
                    details.reason === "input-change" ||
                    details.reason === "input-clear"
                  ) {
                    form.setFieldValue("name", name);
                    form.setFieldValue("selectedId", null);
                  }
                }}
                disabled={isSaving}
              >
                <ComboboxInput
                  id={nameId}
                  placeholder={intl.formatMessage({
                    id: "search_filter.save_name_placeholder",
                  })}
                  aria-describedby={`${hintId} ${statusId}`}
                  aria-invalid={nameTaken}
                  disabled={isSaving}
                  showTrigger={false}
                  autoFocus
                />
                {/* Keep results and feedback inside a stable dialog, including
                    when the query has no matches or selects an overwrite. */}
                <div className="flex h-48 shrink-0 flex-col overflow-hidden rounded-md border">
                  <ComboboxEmpty className="flex shrink-0 px-2 empty:p-0">
                    <FormattedMessage id="search_filter.save_no_matches" />
                  </ComboboxEmpty>
                  <ComboboxList className="min-h-0 max-h-none flex-1">
                    {(filter: (typeof savedFilters)[number]) => (
                      <ComboboxItem
                        key={filter.id}
                        value={filter}
                        className="min-h-11"
                      >
                        {filter.name}
                      </ComboboxItem>
                    )}
                  </ComboboxList>
                </div>
              </Combobox>
              <FieldDescription id={hintId}>
                <FormattedMessage id="search_filter.save_name_help" />
              </FieldDescription>
              <div id={statusId} aria-live="polite">
                {nameTaken && (
                  <FieldError>
                    <FormattedMessage id="search_filter.save_name_exists" />
                  </FieldError>
                )}
                {selected && (
                  <FieldDescription>
                    <FormattedMessage
                      id="dialogs.overwrite_filter_warning"
                      values={{ entityName: selected.name }}
                    />
                  </FieldDescription>
                )}
              </div>
            </Field>
          </FieldGroup>
          <DialogFooter className="shrink-0 flex-row *:flex-1 sm:*:flex-none">
            <Button
              type="button"
              variant="outline"
              onClick={() => onClose()}
              disabled={isSaving}
            >
              <FormattedMessage id="actions.cancel" />
            </Button>
            <Button
              type="submit"
              disabled={isSaving || !values.name.trim() || nameTaken}
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
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
