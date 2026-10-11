import { useCallback, useId, useRef, useState } from "react";
import { useMsg } from "@/hooks/message";
import {
  createSubscriptionAPI,
  type SourceSubscription,
  type SubscriptionScope,
} from "@/core/native-archive/subscription-api";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldLabel,
  FieldGroup,
} from "@/components/ui/field";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import { Empty, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Spinner } from "@/components/ui/spinner";
import { PageSummary } from "@/components/list/page-summary";
import {
  Pagination,
  PaginationContent,
  PaginationItem,
} from "@/components/ui/pagination";
import { useActivityRead } from "./activity/shared";

function SubscriptionRow({
  item,
  disabled,
  save,
}: {
  item: SourceSubscription;
  disabled: boolean;
  save: (item: SourceSubscription, enabled: boolean) => void;
}) {
  const msg = useMsg();
  const id = useId();
  const unavailable = !item.enabled && item.collection.state !== "active";
  return (
    <Field orientation="horizontal" data-disabled={disabled || unavailable}>
      <FieldContent>
        <FieldLabel htmlFor={id}>{item.collection.label}</FieldLabel>
        <FieldDescription className="wrap-anywhere">
          {item.collection.target_url}
        </FieldDescription>
        {unavailable && (
          <FieldDescription>
            {msg(
              "source_monitoring.inactive",
              "Activate this source before enabling monitoring.",
            )}
          </FieldDescription>
        )}
      </FieldContent>
      <Switch
        id={id}
        checked={item.enabled}
        disabled={disabled || unavailable}
        onCheckedChange={(enabled) => save(item, enabled)}
      />
    </Field>
  );
}

function Monitoring({ scope, id }: { scope: SubscriptionScope; id: string }) {
  const msg = useMsg();
  const [api] = useState(() => createSubscriptionAPI());
  const [page, setPage] = useState(1);
  const [refresh, setRefresh] = useState(0);
  const [saving, setSaving] = useState(false);
  const pending = useRef(false);
  const [error, setError] = useState<unknown>();
  const load = useCallback(
    (signal: AbortSignal) => api.list(scope, id, page, signal),
    [api, scope, id, page],
  );
  const read = useActivityRead(`${scope}:${id}:${page}`, load, refresh);
  const data = read.current;
  const busy = read.busy || saving;
  async function change(operation: () => Promise<unknown>) {
    if (pending.current) return;
    pending.current = true;
    setSaving(true);
    setError(undefined);
    try {
      await operation();
    } catch (error) {
      setError(error);
    } finally {
      pending.current = false;
      setSaving(false);
      setRefresh((value) => value + 1);
    }
  }
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          {msg("source_monitoring.title", "Automatic monitoring")}
        </CardTitle>
        <CardDescription>
          {msg(
            "source_monitoring.help",
            "Your scheduled worker checks enabled sources daily and during periodic recovery. Turning a source off cancels queued monitoring; a scrape already running can finish. Existing media and manual imports are preserved.",
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {busy && (
          <Spinner
            aria-label={msg("source_monitoring.loading", "Updating monitoring")}
          />
        )}
        {(error !== undefined || read.error !== undefined) && (
          <Alert variant="destructive">
            <AlertTitle>
              {msg(
                "source_monitoring.failed",
                "Could not confirm monitoring settings",
              )}
            </AlertTitle>
            <AlertDescription>
              {msg(
                "source_monitoring.failed_help",
                "Refresh to check the current setting before trying again.",
              )}
            </AlertDescription>
          </Alert>
        )}
        {data?.total === 0 && (
          <Empty>
            <EmptyHeader>
              <EmptyTitle>
                {msg("source_monitoring.empty", "No configured scrape sources")}
              </EmptyTitle>
            </EmptyHeader>
          </Empty>
        )}
        <FieldGroup>
          {data?.items.map((item) => (
            <SubscriptionRow
              key={item.collection.uuid}
              item={item}
              disabled={busy || read.error !== undefined}
              save={(row, enabled) => void change(() => api.save(row, enabled))}
            />
          ))}
        </FieldGroup>
        {data && data.total > 25 && (
          <div className="flex flex-wrap items-center justify-between gap-2">
            <PageSummary total={data.total} page={page} pageSize={25} />
            <Pagination className="mx-0 w-auto">
              <PaginationContent>
                <PaginationItem>
                  <Button
                    variant="outline"
                    disabled={busy || page <= 1}
                    onClick={() => setPage((value) => value - 1)}
                  >
                    {msg("actions.previous", "Previous")}
                  </Button>
                </PaginationItem>
                <PaginationItem>
                  <Button
                    variant="outline"
                    disabled={busy || page * 25 >= data.total}
                    onClick={() => setPage((value) => value + 1)}
                  >
                    {msg("actions.next", "Next")}
                  </Button>
                </PaginationItem>
              </PaginationContent>
            </Pagination>
          </div>
        )}
      </CardContent>
      <CardFooter className="flex-wrap gap-2">
        <Button
          variant="outline"
          disabled={busy}
          onClick={() => {
            setError(undefined);
            setRefresh((value) => value + 1);
          }}
        >
          {msg("source_monitoring.refresh", "Refresh monitoring")}
        </Button>
        {scope === "performer" && !!data?.total && (
          <Button
            variant="outline"
            disabled={busy || read.error !== undefined}
            onClick={() => void change(() => api.stopPerformer(id))}
          >
            {msg(
              "source_monitoring.stop_performer",
              "Stop monitoring all linked accounts",
            )}
          </Button>
        )}
      </CardFooter>
    </Card>
  );
}

export function SourceMonitoring(props: {
  scope: SubscriptionScope;
  id: string;
}) {
  return <Monitoring key={`${props.scope}:${props.id}`} {...props} />;
}
