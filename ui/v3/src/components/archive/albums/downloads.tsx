import { useCallback, type ReactNode } from "react";
import { useIntl } from "react-intl";
import { useMsg } from "@/hooks/message";
import type {
  DownloadAPI,
  DownloadTransfer,
} from "@/core/native-archive/download-api";
import type { AlbumSlot } from "@/core/native-archive/source-album-api";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Spinner } from "@/components/ui/spinner";
import { PostEmpty, PostSection } from "../posts/shared";
import { useAlbumPages } from "./read";
import { useDownloadStatus, useVisibleDownloadGroup } from "./download-read";

function useDownloadLabels() {
  const msg = useMsg();
  return {
    state: {
      downloading: msg("downloads.downloading", "Download started"),
      interrupted: msg("downloads.interrupted", "No completion report"),
      downloaded: msg("downloads.downloaded", "Download reported complete"),
      failed: msg("downloads.failed", "Download failed"),
      excluded: msg("downloads.excluded", "Excluded from download"),
      skipped: msg("downloads.skipped", "Skipped without a file"),
    },
    verification: {
      queued: msg("downloads.verification_queued", "File check queued"),
      running: msg("downloads.verification_running", "Checking file"),
      succeeded: msg("downloads.verification_succeeded", "File check passed"),
      failed: msg("downloads.verification_failed", "File check failed"),
      cancelled: msg(
        "downloads.verification_cancelled",
        "File check cancelled",
      ),
    },
    reason: {
      download_failed: msg(
        "downloads.reason_download",
        "The download did not finish successfully.",
      ),
      postprocess_failed: msg(
        "downloads.reason_postprocess",
        "Processing the downloaded file failed.",
      ),
      source_failure: msg(
        "downloads.reason_source",
        "The source attempt failed before this file completed.",
      ),
      unsupported_media: msg(
        "downloads.reason_unsupported",
        "This media type is not supported.",
      ),
      filter: msg(
        "downloads.reason_filter",
        "The download configuration excluded this item.",
      ),
      archive_entry_without_file: msg(
        "downloads.reason_archive",
        "The downloader's archive marks this item as handled, but it could not confirm a local file.",
      ),
      existing_without_file: msg(
        "downloads.reason_existing",
        "The downloader skipped an existing item without confirming a completed local file.",
      ),
    },
  };
}

function DownloadBadges({ transfer }: { transfer: DownloadTransfer }) {
  const labels = useDownloadLabels();
  return (
    <div className="flex flex-wrap gap-2">
      <Badge variant={transfer.state === "failed" ? "destructive" : "outline"}>
        {labels.state[transfer.state]}
      </Badge>
      {transfer.verification_state && (
        <Badge
          variant={
            transfer.verification_state === "failed"
              ? "destructive"
              : "secondary"
          }
        >
          {labels.verification[transfer.verification_state]}
        </Badge>
      )}
    </div>
  );
}

export function DownloadSummary({
  transfer,
}: {
  transfer: DownloadTransfer | null | undefined;
}) {
  const msg = useMsg();
  if (transfer === undefined) return null;
  if (transfer === null)
    return (
      <p className="text-muted-foreground">
        {msg("downloads.no_reports", "No download reports recorded")}
      </p>
    );
  return (
    <div className="flex flex-col gap-1" data-download-summary>
      <p className="text-muted-foreground">
        {msg("downloads.latest", "Latest recorded transfer")}
      </p>
      <DownloadBadges transfer={transfer} />
    </div>
  );
}

function DownloadReadError({ retry }: { retry: () => void }) {
  const msg = useMsg();
  return (
    <Alert variant="destructive">
      <AlertTitle>
        {msg("downloads.read_failed", "Could not refresh download reports")}
      </AlertTitle>
      <AlertDescription>
        <p>
          {msg(
            "downloads.read_failed_help",
            "Any reports still shown are from the last successful check. Check your connection and access to Stash, then retry.",
          )}
        </p>
        <Button type="button" variant="outline" size="sm" onClick={retry}>
          {msg("actions.retry", "Retry")}
        </Button>
      </AlertDescription>
    </Alert>
  );
}

type DownloadSlotsProps = {
  slots: AlbumSlot[];
  api: DownloadAPI;
  children: (
    slot: AlbumSlot,
    transfer: DownloadTransfer | null | undefined,
  ) => ReactNode;
};

function DownloadGroup({ slots, api, children }: DownloadSlotsProps) {
  const msg = useMsg(),
    intl = useIntl();
  const ids = [
    ...new Set(
      slots.flatMap((slot) => (slot.attachment ? [slot.attachment.uuid] : [])),
    ),
  ].join(",");
  const { setNode, active } = useVisibleDownloadGroup();
  const result = useDownloadStatus(api, ids, active);
  const latest = new Map(
    result.data?.attachments.map((row) => [row.attachment_uuid, row.latest]),
  );
  return (
    <div ref={setNode} className="flex flex-col gap-4" data-download-group>
      {result.error !== undefined && (
        <DownloadReadError retry={result.reload} />
      )}
      {slots.map((slot) =>
        children(
          slot,
          slot.attachment ? latest.get(slot.attachment.uuid) : undefined,
        ),
      )}
      {ids && (
        <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
          {result.data && (
            <p>
              {intl.formatMessage(
                {
                  id: "downloads.checked",
                  defaultMessage: "Download reports checked {time}",
                },
                {
                  time: intl.formatDate(result.data.checked_at, {
                    dateStyle: "medium",
                    timeStyle: "short",
                  }),
                },
              )}
            </p>
          )}
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={result.busy}
            onClick={result.reload}
          >
            {result.busy && <Spinner data-icon="inline-start" />}
            {msg("downloads.refresh", "Refresh download reports")}
          </Button>
        </div>
      )}
    </div>
  );
}

export function DownloadSlots({ slots, ...props }: DownloadSlotsProps) {
  const groups: AlbumSlot[][] = [];
  for (let offset = 0; offset < slots.length; offset += 25)
    groups.push(slots.slice(offset, offset + 25));
  return groups.map((group) => (
    <DownloadGroup key={group[0]!.position} slots={group} {...props} />
  ));
}

function TransferCard({ transfer }: { transfer: DownloadTransfer }) {
  const msg = useMsg(),
    intl = useIntl(),
    labels = useDownloadLabels();
  const date = (value: string) =>
    intl.formatDate(value, { dateStyle: "medium", timeStyle: "medium" });
  return (
    <Card
      size="sm"
      className="shrink-0"
      data-download-transfer={transfer.sequence}
    >
      <CardHeader>
        <CardTitle className="wrap-anywhere" data-selectable-text>
          {transfer.collection_label ||
            msg("downloads.source", "Source collection")}
        </CardTitle>
        <CardDescription className="wrap-anywhere" data-selectable-text>
          {transfer.root_label}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3 text-sm">
        <DownloadBadges transfer={transfer} />
        {transfer.reason_code && <p>{labels.reason[transfer.reason_code]}</p>}
        {transfer.state === "interrupted" && (
          <p>
            {msg(
              "downloads.interrupted_help",
              "The source attempt is no longer active, and no outcome has arrived. A delayed report may still arrive.",
            )}
          </p>
        )}
        <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-2">
          <dt>{msg("downloads.started", "Started")}</dt>
          <dd>
            {transfer.started_at
              ? date(transfer.started_at)
              : msg("downloads.not_reported", "Not reported")}
          </dd>
          <dt>{msg("downloads.finished", "Finished")}</dt>
          <dd>
            {transfer.finished_at
              ? date(transfer.finished_at)
              : msg("downloads.not_reported", "Not reported")}
          </dd>
          <dt>{msg("downloads.received", "First received")}</dt>
          <dd>{date(transfer.first_recorded_at)}</dd>
          <dt>{msg("downloads.last_received", "Last received")}</dt>
          <dd>{date(transfer.last_recorded_at)}</dd>
        </dl>
        <PostSection title={msg("downloads.references", "Report references")}>
          <dl className="flex flex-col gap-2">
            <dt>{msg("downloads.run", "Source run")}</dt>
            <dd className="wrap-anywhere" data-selectable-text>
              {transfer.run_uuid}
            </dd>
            <dt>{msg("downloads.capture", "Source capture")}</dt>
            <dd className="wrap-anywhere" data-selectable-text>
              {transfer.capture_event_uuid}
            </dd>
            {transfer.start_event_uuid && (
              <>
                <dt>{msg("downloads.start_report", "Start report")}</dt>
                <dd className="wrap-anywhere" data-selectable-text>
                  {transfer.start_event_uuid}
                </dd>
              </>
            )}
            {transfer.terminal_event_uuid && (
              <>
                <dt>{msg("downloads.outcome_report", "Outcome report")}</dt>
                <dd className="wrap-anywhere" data-selectable-text>
                  {transfer.terminal_event_uuid}
                </dd>
              </>
            )}
            {transfer.verification_job_uuid && (
              <>
                <dt>{msg("downloads.file_check", "File check")}</dt>
                <dd className="wrap-anywhere" data-selectable-text>
                  {transfer.verification_job_uuid}
                </dd>
              </>
            )}
          </dl>
        </PostSection>
      </CardContent>
    </Card>
  );
}

export function DownloadHistory({
  api,
  attachment,
  onClose,
}: {
  api: DownloadAPI;
  attachment: string;
  onClose: () => void;
}) {
  const msg = useMsg();
  const load = useCallback(
    async (before: number | undefined, signal: AbortSignal) => {
      const page = await api.history(attachment, before, signal);
      return {
        signature: attachment,
        items: page.transfers,
        next: page.next_before,
        header: page,
      };
    },
    [api, attachment],
  );
  const result = useAlbumPages(load);
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="flex max-h-[85dvh] flex-col overflow-hidden sm:max-w-2xl">
        <DialogHeader className="shrink-0 pr-8">
          <DialogTitle>
            {msg("downloads.history", "Download history")}
          </DialogTitle>
          <DialogDescription>
            {msg(
              "downloads.history_help",
              "One entry per transfer, ordered by its first received report. Reports can arrive late. Completion and file checks do not guarantee the file is still available.",
            )}
          </DialogDescription>
        </DialogHeader>
        <Button
          type="button"
          variant="outline"
          disabled={result.busy}
          onClick={result.reload}
        >
          {result.busy && <Spinner data-icon="inline-start" />}
          {msg("downloads.refresh", "Refresh download reports")}
        </Button>
        <div className="flex min-h-0 flex-col gap-4 overflow-y-auto">
          {result.error !== undefined && (
            <DownloadReadError retry={result.reload} />
          )}
          {result.data?.items.length === 0 && (
            <PostEmpty
              title={msg(
                "downloads.no_reports",
                "No download reports recorded",
              )}
            >
              {msg(
                "downloads.no_reports_help",
                "Files imported or downloaded before native reporting may still exist in the library.",
              )}
            </PostEmpty>
          )}
          {result.data?.items.map((transfer) => (
            <TransferCard key={transfer.sequence} transfer={transfer} />
          ))}
          {result.data?.next != null && (
            <Button
              type="button"
              variant="outline"
              disabled={result.busy || result.error !== undefined}
              onClick={() => void result.more()}
            >
              {msg("downloads.older", "Load older transfers")}
            </Button>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
