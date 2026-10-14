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
        className="max-h-[calc(100dvh-2rem)] overflow-y-auto"
      >
        <DialogHeader>
          <DialogTitle>
            <FormattedMessage id="actions.save_filter" />
          </DialogTitle>
        </DialogHeader>
        <form
          className="flex flex-col gap-4"
          onSubmit={(event) => {
            event.preventDefault();
            void form.handleSubmit();
          }}
        >
          <FieldGroup>
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
                <div className="rounded-md border">
                  <ComboboxEmpty>
                    <FormattedMessage id="search_filter.save_no_matches" />
                  </ComboboxEmpty>
                  <ComboboxList className="max-h-48">
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
          <DialogFooter>
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
