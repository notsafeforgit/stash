import { useId, type ComponentProps } from "react";
import { useForm, useStore } from "@tanstack/react-form";
import { FormattedMessage, useIntl } from "react-intl";
import { z } from "zod";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import type { SavedFilterDataFragment } from "@/core/generated-graphql";
import { checkSavedFilterRename } from "./saved-filter-name";

export function RenameSavedFilterDialog({
  filter,
  filters,
  onRename,
  onClose,
  finalFocus,
}: {
  filter: SavedFilterDataFragment;
  filters: readonly SavedFilterDataFragment[];
  /** Resolves true when the rename was saved and the dialog may close. */
  onRename: (name: string) => Promise<boolean>;
  onClose: () => void;
  finalFocus?: ComponentProps<typeof DialogContent>["finalFocus"];
}) {
  const intl = useIntl();
  const nameId = useId();
  const statusId = useId();
  const form = useForm({
    defaultValues: { name: filter.name },
    validators: {
      onChange: z.object({ name: z.string().trim().min(1) }),
    },
    onSubmit: async ({ value }) => {
      const check = checkSavedFilterRename(
        filters,
        filter,
        value.name,
        intl.locale,
      );
      if (check.status === "empty" || check.status === "duplicate") return;
      if (check.status === "unchanged") {
        onClose();
        return;
      }
      if (await onRename(check.name)) onClose();
    },
  });
  const name = useStore(form.store, (state) => state.values.name);
  const isSubmitting = useStore(form.store, (state) => state.isSubmitting);
  const check = checkSavedFilterRename(filters, filter, name, intl.locale);
  const duplicate = check.status === "duplicate";

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !isSubmitting) onClose();
      }}
    >
      <DialogContent showCloseButton={false} finalFocus={finalFocus}>
        <DialogHeader>
          <DialogTitle>
            <FormattedMessage id="search_filter.rename_saved_filter_title" />
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
            <form.Field name="name">
              {(field) => (
                <Field data-disabled={isSubmitting} data-invalid={duplicate}>
                  <FieldLabel htmlFor={nameId}>
                    <FormattedMessage id="filter_name" />
                  </FieldLabel>
                  <Input
                    id={nameId}
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(event) =>
                      field.handleChange(event.currentTarget.value)
                    }
                    disabled={isSubmitting}
                    aria-invalid={duplicate}
                    aria-describedby={statusId}
                    autoComplete="off"
                    autoFocus
                  />
                  <div id={statusId} aria-live="polite">
                    {duplicate && (
                      <FieldError>
                        <FormattedMessage id="search_filter.rename_name_exists" />
                      </FieldError>
                    )}
                  </div>
                </Field>
              )}
            </form.Field>
          </FieldGroup>
          <DialogFooter className="flex-row *:flex-1 sm:*:flex-none">
            <Button
              type="button"
              variant="outline"
              onClick={() => onClose()}
              disabled={isSubmitting}
            >
              <FormattedMessage id="actions.cancel" />
            </Button>
            <Button
              type="submit"
              disabled={isSubmitting || check.status === "empty" || duplicate}
            >
              {isSubmitting ? (
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
