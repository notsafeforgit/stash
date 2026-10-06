import { useMsg } from "@/hooks/message";
import type { ManualFileStatus } from "@/core/native-archive/manual-intake-api";
import {
  manualBatchSettled,
  type SavedManualBatch,
} from "@/core/native-archive/manual-intake-outbox";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from "@/components/ui/card";
import { Expandable } from "@/components/detail/native-metadata/shared";
export function ManualBatch({
  batch,
  disabled,
  canSubmit,
  onRefresh,
  onDeliver,
  onCancel,
  onRetry,
  onDismiss,
}: {
  batch: SavedManualBatch;
  disabled: boolean;
  canSubmit: boolean;
  onRefresh: () => void;
  onDeliver: () => void;
  onCancel: (status: ManualFileStatus) => void;
  onRetry: (status: ManualFileStatus) => void;
  onDismiss: () => void;
}) {
  const msg = useMsg();
  const states = {
    queued: msg("manual_intake.queued", "Queued"),
    running: msg("manual_intake.running", "Processing"),
    succeeded: msg("manual_intake.succeeded", "Imported"),
    failed: msg("manual_intake.failed", "Stopped after an error"),
    cancelled: msg("manual_intake.cancelled", "Cancelled"),
  };
  return (
    <Card>
      <CardHeader>
        <CardTitle>{msg("manual_intake.batch", "Import batch")}</CardTitle>
        <CardDescription>
          {msg(
            "manual_intake.batch_help",
            "This review is saved in this browser. Refresh checks the original requests; continuing sends only imports that have not been accepted.",
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {batch.items.map((item) => (
          <div
            key={item.preview.relative_path}
            className="flex min-w-0 flex-col gap-2"
          >
            <p data-selectable-text className="wrap-anywhere">
              {item.preview.relative_path}
            </p>
            <div className="flex flex-wrap gap-2">
              <Badge
                variant={
                  item.rejection || item.status?.state === "failed"
                    ? "destructive"
                    : "secondary"
                }
              >
                {item.rejection
                  ? msg("manual_intake.rejected", "Needs a new review")
                  : item.cancel
                    ? msg(
                        "manual_intake.cancel_pending",
                        "Cancellation unconfirmed",
                      )
                    : item.status
                      ? states[item.status.state]
                      : item.retry
                        ? msg(
                            "manual_intake.retry_unconfirmed",
                            "Retry unconfirmed",
                          )
                        : msg(
                            "manual_intake.unconfirmed",
                            "Import unconfirmed",
                          )}
              </Badge>
              {(item.status ?? item.retry?.prior)?.registration_committed &&
                !item.status?.media_ingested && (
                  <Badge variant="outline">
                    {msg(
                      "manual_intake.registered",
                      "Media saved; follow-up work unfinished",
                    )}
                  </Badge>
                )}
            </div>
            {item.rejection && (
              <p className="text-sm">
                {msg(
                  "manual_intake.rejected_help",
                  "The file or its rules changed before import. Start another review after this batch is resolved.",
                )}
              </p>
            )}
            {(item.status ?? item.retry?.prior)?.registration_committed &&
              item.status?.state !== "succeeded" && (
                <p className="text-sm">
                  {msg(
                    "manual_intake.registered_help",
                    "The library item is retained. Cancelling does not remove it or undo its metadata.",
                  )}
                </p>
              )}
            {item.status &&
              ["queued", "running"].includes(item.status.state) &&
              !item.cancel && (
                <Button
                  variant="outline"
                  size="sm"
                  className="w-fit"
                  disabled={disabled}
                  onClick={() => item.status && onCancel(item.status)}
                >
                  {msg("manual_intake.cancel", "Cancel remaining work")}
                </Button>
              )}
            {item.status &&
              ["failed", "cancelled"].includes(item.status.state) &&
              !item.cancel && (
                <Button
                  variant="outline"
                  size="sm"
                  className="w-fit"
                  disabled={disabled || !canSubmit}
                  onClick={() => item.status && onRetry(item.status)}
                >
                  {item.status.registration_committed
                    ? msg("manual_intake.retry_effects", "Retry remaining work")
                    : msg("manual_intake.retry", "Retry import")}
                </Button>
              )}
            <Expandable
              title={msg("manual_intake.references", "Import references")}
            >
              <p
                data-selectable-text
                className="wrap-anywhere font-mono text-xs"
              >
                {JSON.parse(item.body).request_uuid}
              </p>
              {item.status && (
                <p
                  data-selectable-text
                  className="wrap-anywhere font-mono text-xs"
                >
                  {item.status.job_uuid}
                </p>
              )}
              {item.status?.error_code && (
                <p
                  data-selectable-text
                  className="wrap-anywhere font-mono text-xs"
                >
                  {item.status.error_code}
                </p>
              )}
            </Expandable>
          </div>
        ))}
      </CardContent>
      <CardFooter className="flex flex-wrap gap-2">
        <Button variant="outline" disabled={disabled} onClick={onRefresh}>
          {msg("manual_intake.refresh", "Refresh import status")}
        </Button>
        {batch.items.some(
          (item) => item.cancel || (!item.status && !item.rejection),
        ) && (
          <Button disabled={disabled || !canSubmit} onClick={onDeliver}>
            {msg("manual_intake.continue", "Continue saved imports")}
          </Button>
        )}
        {manualBatchSettled(batch) && (
          <Button variant="outline" disabled={disabled} onClick={onDismiss}>
            {msg("manual_intake.new_batch", "Start another batch")}
          </Button>
        )}
      </CardFooter>
    </Card>
  );
}
