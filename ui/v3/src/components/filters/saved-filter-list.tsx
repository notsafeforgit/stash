import { useId, useRef, useState } from "react";
import { useForm, useStore } from "@tanstack/react-form";
import { FormattedMessage, useIntl } from "react-intl";
import { z } from "zod";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  createListboxController,
  Listbox,
  ListboxItem,
  ListboxItemIndicator,
  matchRanges,
} from "@/components/ui/listbox";
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
import type {
  FilterMode,
  FindSavedFiltersQuery,
} from "@/core/generated-graphql";
import { useFindSavedFilters } from "@/core/saved-filters";
import { findSavedFilterByName } from "./saved-filter-name";

type SavedFilterItem = FindSavedFiltersQuery["findSavedFilters"][number];

function matchesName(name: string, query: string) {
  return query === "" || matchRanges(name, query, "contains").length > 0;
}

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
  const nameRef = useRef<HTMLInputElement>(null);
  const { data, previousData } = useFindSavedFilters(mode);
  const savedFilters =
    data?.findSavedFilters ?? previousData?.findSavedFilters ?? [];
  const [suggestions] = useState(() =>
    createListboxController<SavedFilterItem>(),
  );
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
    matchesName(filter.name, values.name.trim()),
  );
  function namedFilter(name: string) {
    return findSavedFilterByName(savedFilters, name, intl.locale);
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
        className="grid h-[min(32rem,calc(100dvh-2rem))] grid-rows-[auto_minmax(0,1fr)] overflow-hidden"
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
            <div className="flex min-w-0 flex-col gap-1.5">
              <Field data-disabled={isSaving} data-invalid={nameTaken}>
                <FieldLabel htmlFor={nameId}>
                  <FormattedMessage id="filter_name" />
                </FieldLabel>
                <Input
                  id={nameId}
                  value={values.name}
                  onChange={(event) => {
                    // Typing is the only path that edits the name freely; a
                    // suggestion choice below sets the saved filter instead.
                    form.setFieldValue("name", event.target.value);
                    form.setFieldValue("selectedId", null);
                  }}
                  placeholder={intl.formatMessage({
                    id: "search_filter.save_name_placeholder",
                  })}
                  aria-describedby={`${hintId} ${statusId}`}
                  aria-invalid={nameTaken}
                  disabled={isSaving}
                  autoFocus
                  ref={nameRef}
                  onKeyDown={(event) => {
                    // As in a combobox: ArrowDown moves into the suggestions.
                    const [first] = filteredItems;
                    if (event.key !== "ArrowDown" || !first) return;
                    event.preventDefault();
                    suggestions.highlight(first);
                    suggestions.focus();
                  }}
                />
              </Field>
              {/* Keep results and feedback inside a stable dialog, including
                  when the query has no matches or selects an overwrite. */}
              <div className="flex h-48 shrink-0 flex-col overflow-hidden rounded-md border">
                {filteredItems.length === 0 && (
                  <div className="flex shrink-0 px-2">
                    <FormattedMessage id="search_filter.save_no_matches" />
                  </div>
                )}
                <Listbox<SavedFilterItem>
                  controller={suggestions}
                  aria-label={intl.formatMessage({
                    id: "search_filter.saved_filters",
                  })}
                  // Arrows only move focus; Enter or a press chooses.
                  activation="manual"
                  className="h-full w-full min-h-0 max-w-none flex-1 overflow-y-auto rounded-none border-0 p-0"
                  disabled={isSaving}
                  value={selected}
                  itemToValue={(filter) => filter.id}
                  isItemEqualToValue={(filter, value) => filter.id === value.id}
                  onValueChange={(filter, details) => {
                    if (!filter) return;
                    form.setFieldValue("name", filter.name);
                    form.setFieldValue("selectedId", filter.id);
                    // Keep the name focused after a choice, as a combobox does.
                    if (details.reason === "item-press")
                      nameRef.current?.focus();
                  }}
                >
                  {filteredItems.map((filter, index) => (
                    <ListboxItem
                      key={filter.id}
                      value={filter}
                      className="min-h-11"
                      onKeyDown={(event) => {
                        if (event.key !== "ArrowUp" || index !== 0) return;
                        event.preventDefault();
                        nameRef.current?.focus();
                      }}
                    >
                      {filter.name}
                      <ListboxItemIndicator />
                    </ListboxItem>
                  ))}
                </Listbox>
              </div>
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
            </div>
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
