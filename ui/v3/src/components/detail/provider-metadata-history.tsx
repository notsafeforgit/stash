import { useEffect, useState } from "react";
import { useIntl } from "react-intl";
import { ChevronDown } from "lucide-react";
import { useMsg } from "@/hooks/message";
import {
  createProviderMetadataAPI,
  type ProviderEntityKind,
  type ProviderMetadataImport,
} from "@/core/native-archive/provider-metadata-api";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import { Empty, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import {
  Collapsible,
  CollapsibleTrigger,
  CollapsibleContent,
} from "@/components/ui/collapsible";
import { Expandable, fieldMessages } from "./native-metadata/shared";

type HistoryTarget = { kind: ProviderEntityKind } & (
  | { localId: string; entityUUID?: never }
  | { entityUUID: string; localId?: never }
);

/** The keyed child prevents navigation from retaining another entity's page. */
export function ProviderMetadataHistory(props: HistoryTarget) {
  return (
    <History
      key={`${props.kind}:${props.entityUUID ?? props.localId}`}
      {...props}
    />
  );
}

function History({ kind, localId, entityUUID }: HistoryTarget) {
  const msg = useMsg();
  const intl = useIntl();
  const [api] = useState(() => createProviderMetadataAPI());
  const [open, setOpen] = useState(false);
  const [after, setAfter] = useState(0);
  const [previous, setPrevious] = useState<number[]>([]);
  const [rows, setRows] = useState<ProviderMetadataImport[]>([]);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const [refresh, setRefresh] = useState(0);
  // biome-ignore lint/correctness/useExhaustiveDependencies: Explicit retry/refresh loads this page again.
  useEffect(() => {
    if (!open) return;
    const controller = new AbortController();
    const signal = controller.signal;
    setBusy(true);
    setFailed(false);
    async function load() {
      try {
        const id =
          entityUUID ?? (await api.identity(kind, localId ?? "", signal)).uuid;
        const page = await api.history(id, kind, after, signal);
        if (!signal.aborted) setRows(page);
      } catch {
        if (!signal.aborted) setFailed(true);
      } finally {
        if (!signal.aborted) setBusy(false);
      }
    }
    void load();
    return () => controller.abort();
  }, [api, open, kind, localId, entityUUID, after, refresh]);

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger render={<Button type="button" variant="outline" />}>
        {msg("provider_metadata.history", "Provider import history")}
        <ChevronDown data-icon="inline-end" />
      </CollapsibleTrigger>
      <CollapsibleContent className="flex flex-col gap-3 pt-3">
        <p className="text-sm text-muted-foreground">
          {msg(
            "provider_metadata.history_help",
            "These are the values saved when an import was accepted, including merged local values. Later edits may differ. This history does not identify the source of every current field.",
          )}
        </p>
        {kind === "performer" && localId && (
          <p className="text-sm text-muted-foreground">
            {msg(
              "provider_metadata.merged_help",
              "Imports recorded before a performer merge remain under the earlier identity. Open identity history in Source accounts to view them.",
            )}
          </p>
        )}
        {busy && (
          <Spinner
            aria-label={msg(
              "provider_metadata.loading",
              "Loading provider imports",
            )}
          />
        )}
        {failed && (
          <Alert variant="destructive">
            <AlertTitle>
              {msg("provider_metadata.failed", "Could not load import history")}
            </AlertTitle>
            <AlertDescription>
              <Button
                type="button"
                variant="outline"
                onClick={() => setRefresh((value) => value + 1)}
              >
                {msg("actions.retry", "Retry")}
              </Button>
            </AlertDescription>
          </Alert>
        )}
        {!busy && !failed && rows.length === 0 && (
          <Empty>
            <EmptyHeader>
              <EmptyTitle>
                {msg(
                  "provider_metadata.empty",
                  "No recorded imports on this page",
                )}
              </EmptyTitle>
            </EmptyHeader>
          </Empty>
        )}
        {!busy &&
          !failed &&
          rows.map((row) => (
            <Expandable
              key={row.uuid}
              title={intl.formatMessage(
                {
                  id: "provider_metadata.entry",
                  defaultMessage: "{provider} · {date}",
                },
                {
                  provider: new URL(row.endpoint).host,
                  date: intl.formatDate(row.created_at, {
                    dateStyle: "medium",
                    timeStyle: "short",
                  }),
                },
              )}
            >
              <p data-selectable-text className="wrap-anywhere text-sm">
                {row.endpoint}
              </p>
              <p data-selectable-text className="wrap-anywhere text-sm">
                {intl.formatMessage(
                  {
                    id: "provider_metadata.remote_id",
                    defaultMessage: "Provider ID: {id}",
                  },
                  { id: row.remote_id },
                )}
              </p>
              <dl className="flex flex-col gap-2">
                {Object.entries(row.values).map(([field, value]) => (
                  <div key={field}>
                    <dt className="text-sm font-medium">
                      {intl.formatMessage({
                        id:
                          fieldMessages[field] ??
                          (
                            {
                              parent: "parent_studio",
                              parents: "parent_tags",
                            } as Record<string, string>
                          )[field] ??
                          field,
                        defaultMessage: field,
                      })}
                    </dt>
                    <dd>
                      <pre
                        data-selectable-text
                        className="whitespace-pre-wrap wrap-anywhere text-xs"
                      >
                        {typeof value === "string"
                          ? value
                          : JSON.stringify(value, null, 2)}
                      </pre>
                    </dd>
                  </div>
                ))}
              </dl>
            </Expandable>
          ))}
        <div className="flex flex-wrap gap-2">
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
            disabled={busy || failed || rows.length < api.pageLimit}
            onClick={() => {
              setPrevious((values) => [...values, after]);
              setAfter(rows.at(-1)?.sequence ?? after);
            }}
          >
            {msg("actions.next", "Next")}
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={busy}
            onClick={() => setRefresh((value) => value + 1)}
          >
            {msg("provider_metadata.refresh", "Refresh imports")}
          </Button>
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}
