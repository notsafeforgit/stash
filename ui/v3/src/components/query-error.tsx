import { FormattedMessage, IntlProvider } from "react-intl";
import { Button } from "@/components/ui/button";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from "@/components/ui/empty";
import { Spinner } from "@/components/ui/spinner";

export interface QueryErrorProps {
  error: Error;
  retry: () => Promise<unknown>;
  retrying?: boolean;
  stale?: boolean;
}

export function QueryError({ error, retry, retrying, stale }: QueryErrorProps) {
  return (
    <Empty
      role="alert"
      className={
        stale ? "flex-none border border-destructive/30 py-3" : undefined
      }
    >
      <EmptyHeader>
        <EmptyTitle>
          {stale ? (
            <FormattedMessage
              id="errors.refresh_failed"
              defaultMessage="Could not refresh. Showing previously loaded data."
            />
          ) : (
            <FormattedMessage
              id="errors.load_failed"
              defaultMessage="Could not load data."
            />
          )}
        </EmptyTitle>
        <EmptyDescription className="break-words">
          {error.message}
        </EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Button
          variant="outline"
          disabled={retrying}
          onClick={() => {
            void retry().catch(() => {});
          }}
        >
          {retrying && <Spinner data-icon="inline-start" />}
          <FormattedMessage id="actions.retry" defaultMessage="Retry" />
        </Button>
      </EmptyContent>
    </Empty>
  );
}

/** Available even before configuration and locale assets can be loaded. */
export function StartupError(props: QueryErrorProps) {
  return (
    <IntlProvider locale="en-GB" defaultLocale="en-GB">
      <div className="flex min-h-screen items-center justify-center bg-background text-foreground">
        <QueryError {...props} />
      </div>
    </IntlProvider>
  );
}
