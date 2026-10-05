import { useEffect, useState } from "react";
import { useIntl } from "react-intl";
import { ChevronDown } from "lucide-react";
import { useMsg } from "@/hooks/message";
import type {
  MediaRootAPI,
  MediaRootRevision,
} from "@/core/native-archive/media-root-api";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import {
  ReviewError,
  Expandable,
} from "@/components/detail/native-metadata/shared";
import { useCollectionLabels } from "../collections/shared";

export function MediaRootHistory({
  api,
  id,
}: {
  api: MediaRootAPI;
  id: string;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const labels = useCollectionLabels();
  const [open, setOpen] = useState(false);
  const [after, setAfter] = useState(0);
  const [previous, setPrevious] = useState<number[]>([]);
  const [rows, setRows] = useState<MediaRootRevision[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const [refresh, setRefresh] = useState(0);
  // biome-ignore lint/correctness/useExhaustiveDependencies: Explicit retry reloads only this history page.
  useEffect(() => {
    if (!open) return;
    const controller = new AbortController();
    setBusy(true);
    setError(undefined);
    api
      .history(id, after, api.pageLimit, controller.signal)
      .then((rows) => {
        if (!controller.signal.aborted) setRows(rows);
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted) setError(error);
      })
      .finally(() => {
        if (!controller.signal.aborted) setBusy(false);
      });
    return () => controller.abort();
  }, [api, id, open, after, refresh]);
  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger render={<Button type="button" variant="outline" />}>
        {msg("media_roots.history", "Media root history")}
        <ChevronDown data-icon="inline-end" />
      </CollapsibleTrigger>
      <CollapsibleContent className="flex flex-col gap-3 pt-3">
        {busy && <Spinner />}
        {error !== undefined && (
          <ReviewError
            error={error}
            retry={() => setRefresh((value) => value + 1)}
          />
        )}
        {!busy &&
          error === undefined &&
          rows.map((row) => (
            <div key={row.revision} className="flex flex-col gap-1">
              <p>
                {row.label} · {labels.states[row.state]}
              </p>
              <p className="text-sm text-muted-foreground">
                {intl.formatMessage(
                  {
                    id: "collections.revision",
                    defaultMessage: "Revision {revision}",
                  },
                  { revision: row.revision },
                )}
                {" · "}
                <time dateTime={row.recorded_at}>
                  {new Date(row.recorded_at).toLocaleString()}
                </time>
              </p>
              <p data-selectable-text className="wrap-anywhere">
                {row.binding?.path ??
                  msg("media_roots.unbound", "No local folder")}
              </p>
              {row.reason && <p>{row.reason}</p>}
              {row.binding && (
                <Expandable
                  title={msg(
                    "media_roots.directory_identity",
                    "Directory identity",
                  )}
                >
                  <p
                    data-selectable-text
                    className="wrap-anywhere font-mono text-xs"
                  >
                    {row.binding.directory_identity}
                  </p>
                </Expandable>
              )}
            </div>
          ))}
        <div className="flex gap-2">
          <Button
            type="button"
            variant="outline"
            disabled={busy || !previous.length}
            onClick={() => {
              setAfter(previous.at(-1) ?? 0);
              setPrevious((values) => values.slice(0, -1));
            }}
          >
            {msg("actions.previous", "Previous")}
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={
              busy || error !== undefined || rows.length < api.pageLimit
            }
            onClick={() => {
              setPrevious((values) => [...values, after]);
              setAfter(rows.at(-1)?.revision ?? after);
            }}
          >
            {msg("actions.next", "Next")}
          </Button>
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}
