import { useId } from "react";
import { useForm } from "@tanstack/react-form";
import { useIntl } from "react-intl";
import { z } from "zod";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";

export interface MobileListPaginationProps {
  currentPage: number;
  itemsPerPage: number;
  totalItems: number;
  onChangePage: (page: number) => void;
}

/** In the list's scroll flow, so pagination costs no permanent screen space. */
export function MobileListPagination({
  currentPage,
  itemsPerPage,
  totalItems,
  onChangePage,
}: MobileListPaginationProps) {
  const intl = useIntl();
  const pages = Math.ceil(totalItems / itemsPerPage);
  if (pages <= 1) return null;
  return (
    <nav
      aria-label={intl.formatMessage({
        id: "pagination.pages",
        defaultMessage: "Pages",
      })}
      className="flex items-center justify-between gap-2 px-3 py-2"
    >
      <Button
        variant="ghost"
        className="h-11"
        disabled={currentPage <= 1}
        onClick={() => onChangePage(currentPage - 1)}
      >
        <ChevronLeft data-icon="inline-start" />
        {intl.formatMessage({
          id: "pagination.previous_short",
          defaultMessage: "Prev",
        })}
      </Button>
      <span className="text-xs text-muted-foreground tabular-nums">
        {intl.formatNumber(currentPage)} / {intl.formatNumber(pages)}
      </span>
      <Button
        variant="ghost"
        className="h-11"
        disabled={currentPage >= pages}
        onClick={() => onChangePage(currentPage + 1)}
      >
        {intl.formatMessage({ id: "pagination.next", defaultMessage: "Next" })}
        <ChevronRight data-icon="inline-end" />
      </Button>
    </nav>
  );
}

/** Page jump is on demand; opening the section picker never opens the keyboard. */
export function MobileListPagePicker(props: MobileListPaginationProps) {
  const { currentPage, itemsPerPage, totalItems, onChangePage } = props;
  const intl = useIntl();
  const pages = Math.ceil(totalItems / itemsPerPage);
  const first =
    totalItems > 0
      ? Math.min((currentPage - 1) * itemsPerPage + 1, totalItems)
      : 0;
  const last = Math.min(currentPage * itemsPerPage, totalItems);
  return (
    <div className="flex flex-col gap-2">
      <span className="text-center text-xs text-muted-foreground tabular-nums">
        {intl.formatNumber(first)}–{intl.formatNumber(last)} /{" "}
        {intl.formatNumber(totalItems)}
      </span>
      {pages > 1 && (
        <>
          <MobileListPagination {...props} />
          <MobileListPageJump
            key={`${currentPage}:${pages}`}
            currentPage={currentPage}
            pages={pages}
            onChangePage={onChangePage}
          />
        </>
      )}
    </div>
  );
}

/** A new page or page count starts a fresh draft, including in kept-mounted popovers. */
function MobileListPageJump({
  currentPage,
  pages,
  onChangePage,
}: Pick<MobileListPaginationProps, "currentPage" | "onChangePage"> & {
  pages: number;
}) {
  const intl = useIntl();
  const inputId = useId();
  const errorId = `${inputId}-error`;
  const form = useForm({
    defaultValues: { page: String(currentPage) },
    validators: {
      onChange: z.object({
        page: z.string().refine(
          (raw) => {
            const page = Number(raw);
            return Number.isInteger(page) && page >= 1 && page <= pages;
          },
          intl.formatMessage(
            {
              id: "pagination.invalid_page",
              defaultMessage:
                "Enter a whole page number between 1 and {pages}.",
            },
            { pages },
          ),
        ),
      }),
    },
    onSubmit: ({ value }) => onChangePage(Number(value.page)),
  });
  return (
    <form
      noValidate
      onSubmit={(event) => {
        event.preventDefault();
        event.stopPropagation();
        void form.handleSubmit();
      }}
    >
      <FieldGroup>
        <form.Field name="page">
          {(field) => (
            <Field data-invalid={field.state.meta.errors.length > 0}>
              <FieldLabel htmlFor={inputId}>
                {intl.formatMessage({
                  id: "pagination.go_to_page",
                  defaultMessage: "Go to page",
                })}
              </FieldLabel>
              <div className="flex gap-2">
                <Input
                  id={inputId}
                  name={field.name}
                  type="number"
                  inputMode="numeric"
                  min={1}
                  max={pages}
                  step={1}
                  required
                  value={field.state.value}
                  onChange={(event) => field.handleChange(event.target.value)}
                  onBlur={field.handleBlur}
                  aria-invalid={field.state.meta.errors.length > 0}
                  aria-describedby={
                    field.state.meta.errors.length > 0 ? errorId : undefined
                  }
                  className="h-11 min-w-0 flex-1 text-base"
                />
                <Button type="submit" className="h-11">
                  {intl.formatMessage({
                    id: "actions.go",
                    defaultMessage: "Go",
                  })}
                </Button>
              </div>
              <FieldError id={errorId} errors={field.state.meta.errors} />
            </Field>
          )}
        </form.Field>
      </FieldGroup>
    </form>
  );
}
