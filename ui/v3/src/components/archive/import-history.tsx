import { useCallback, useState } from "react";
import { Link } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { useMsg } from "@/hooks/message";
import { cn } from "@/lib/utils";
import {
  createImportHistoryAPI,
  importHistorySearchSchema,
  type ImportHistoryAPI,
  type ImportHistorySearch,
  type ImportKind,
  type ImportSnapshot,
} from "@/core/native-archive/import-history-api";
import { useListScrollRestoration } from "@/components/list/use-list-scroll-restoration";
import { Button, buttonVariants } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from "@/components/ui/card";
import { Spinner } from "@/components/ui/spinner";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ReviewError } from "@/components/detail/native-metadata/shared";
import { ActivityTime, useActivityRead } from "./activity/shared";
import { PostEmpty, PostSection } from "./posts/shared";

function useImportLabels() {
  const msg = useMsg();
  return {
    evidence: msg("import_history.evidence", "Post and profile evidence"),
    relations: msg("import_history.relations", "Source relationships"),
    publishers: msg("import_history.publishers", "Source publishers"),
    attachments: msg("import_history.attachments", "Post attachments"),
    media: msg("import_history.media", "Media and file matches"),
    memberships: msg("import_history.memberships", "Collection memberships"),
    documents: msg("import_history.documents", "Retained documents"),
    translations: msg("import_history.translations", "Translation history"),
    enrichment: msg("import_history.enrichment", "Metadata lookup history"),
    file_history: msg(
      "import_history.file_history",
      "File edits and deduplication",
    ),
    cleanup: msg("import_history.cleanup", "Historical cleanup requests"),
    discovery: msg("import_history.discovery", "Source discovery history"),
    checkpoints: msg("import_history.checkpoints", "Saved work checkpoints"),
  };
}

function SnapshotTitle({ row }: { row: ImportSnapshot }) {
  const msg = useMsg();
  return (
    row.collection_label ??
    msg("import_history.automation_snapshot", "Automation snapshot")
  );
}

function SnapshotProgress({ row }: { row: ImportSnapshot }) {
  const msg = useMsg();
  const intl = useIntl();
  return (
    <div className="flex flex-col gap-2">
      <div>
        <Badge variant="secondary">
          {row.transfer_state === "received"
            ? msg("import_history.received", "Transfer received")
            : msg("import_history.receiving", "Receiving transfer")}
        </Badge>
      </div>
      <p className="text-sm">
        {intl.formatMessage(
          {
            id: "import_history.records_received",
            defaultMessage: "{received} of {total} records received",
          },
          {
            received: intl.formatNumber(row.received_records),
            total: intl.formatNumber(row.records),
          },
        )}
      </p>
      <p className="text-sm text-muted-foreground">
        {msg("import_history.captured", "Captured")}{" "}
        <ActivityTime value={row.captured_at} />
      </p>
    </div>
  );
}

function ImportDetail({
  api,
  kind,
  id,
  onBack,
}: {
  api: ImportHistoryAPI;
  kind: ImportKind;
  id: string;
  onBack: () => void;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const labels = useImportLabels();
  const states = {
    running: msg("import_history.running", "In progress"),
    mapped: msg("import_history.mapped", "Mapped"),
    review: msg("import_history.recorded_warnings", "Recorded warnings"),
    retained: msg("import_history.retained", "Retained"),
  };
  const [refresh, setRefresh] = useState(0);
  const load = useCallback(
    (signal: AbortSignal) => api.snapshot(kind, id, signal),
    [api, kind, id],
  );
  const read = useActivityRead(JSON.stringify([kind, id]), load, refresh);
  const row = read.current?.snapshot;
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap gap-2">
        <Button variant="outline" onClick={onBack}>
          {msg("import_history.back", "Back to imports")}
        </Button>
        <Button
          variant="outline"
          disabled={read.busy}
          onClick={() => setRefresh((n) => n + 1)}
        >
          {msg("actions.refresh", "Refresh")}
        </Button>
      </div>
      {read.busy && (
        <Spinner
          aria-label={msg("import_history.loading", "Loading import history")}
        />
      )}
      {read.error !== undefined && (
        <ReviewError
          error={read.error}
          retry={() => setRefresh((n) => n + 1)}
        />
      )}
      {row && (
        <Card>
          <CardHeader>
            <CardTitle>
              <SnapshotTitle row={row} />
            </CardTitle>
            <CardDescription>
              {msg(
                "import_history.transfer_help",
                "Receiving a snapshot preserves its input. Each import step reports its own result below.",
              )}
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <SnapshotProgress row={row} />
            {row.collection_uuid && (
              <div>
                <Link
                  to="/collections"
                  search={{
                    q: "",
                    state: "",
                    kind: "",
                    collection: row.collection_uuid,
                  }}
                  className={buttonVariants({ variant: "outline" })}
                >
                  {msg(
                    "import_history.open_collection",
                    "Open current collection",
                  )}
                </Link>
              </div>
            )}
            <PostSection title={msg("import_history.steps", "Import steps")}>
              <div className="flex flex-col gap-4">
                <p className="text-sm text-muted-foreground">
                  {msg(
                    "import_history.history_help",
                    "These results were recorded during import. Later association reviews do not change historical warning counts. Retained work still needs separate activation before it can run.",
                  )}
                </p>
                {read.current?.families.map((family) => (
                  <Card key={family.name}>
                    <CardHeader>
                      <CardTitle className="text-base">
                        {labels[family.name]}
                      </CardTitle>
                    </CardHeader>
                    <CardContent className="flex flex-col gap-2">
                      <div>
                        <Badge variant="secondary">
                          {family.progress
                            ? states[family.progress.state]
                            : msg("import_history.not_started", "Not started")}
                        </Badge>
                      </div>
                      {family.progress && (
                        <>
                          <p className="text-sm">
                            {intl.formatMessage(
                              {
                                id: "import_history.records_processed",
                                defaultMessage:
                                  "{processed} of {total} records processed",
                              },
                              {
                                processed: intl.formatNumber(
                                  family.progress.processed_records,
                                ),
                                total: intl.formatNumber(
                                  family.progress.source_records,
                                ),
                              },
                            )}
                          </p>
                          {family.progress.historical_review_records > 0 && (
                            <p className="text-sm">
                              {intl.formatMessage(
                                {
                                  id: "import_history.warning_count",
                                  defaultMessage:
                                    "{count, plural, one {# record needed review at import time} other {# records needed review at import time}}",
                                },
                                {
                                  count:
                                    family.progress.historical_review_records,
                                },
                              )}
                            </p>
                          )}
                        </>
                      )}
                    </CardContent>
                  </Card>
                ))}
              </div>
            </PostSection>
            <PostSection
              title={msg("import_history.details", "Snapshot details")}
            >
              <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm">
                <dt>{msg("import_history.snapshot_id", "Snapshot ID")}</dt>
                <dd data-selectable-text className="wrap-anywhere">
                  {row.uuid}
                </dd>
                <dt>{msg("import_history.source_id", "Source ID")}</dt>
                <dd data-selectable-text className="wrap-anywhere">
                  {row.source_uuid}
                </dd>
                <dt>
                  {msg("import_history.registry_id", "Registry import ID")}
                </dt>
                <dd data-selectable-text className="wrap-anywhere">
                  {row.registry_import_uuid}
                </dd>
                <dt>{msg("import_history.updated", "Transfer updated")}</dt>
                <dd>
                  <ActivityTime value={row.updated_at} />
                </dd>
                <dt>{msg("import_history.bytes", "Bytes received")}</dt>
                <dd>{intl.formatNumber(row.received_bytes)}</dd>
                <dt>{msg("import_history.chunks", "Chunks received")}</dt>
                <dd>{intl.formatNumber(row.received_chunks)}</dd>
              </dl>
            </PostSection>
          </CardContent>
        </Card>
      )}
    </div>
  );
}

export function ImportHistory({
  search,
  onChange,
}: {
  search: ImportHistorySearch;
  onChange: (value: ImportHistorySearch) => void;
}) {
  const msg = useMsg();
  const [api] = useState(() => createImportHistoryAPI());
  const [after, setAfter] = useState("");
  const [previous, setPrevious] = useState<string[]>([]);
  const [refresh, setRefresh] = useState(0);
  const [scroller, setScroller] = useState<HTMLDivElement | null>(null);
  const { kind, snapshot } = search;
  const key = JSON.stringify([kind, after]);
  const load = useCallback(
    (signal: AbortSignal) => api.snapshots(kind, after, signal),
    [api, kind, after],
  );
  const read = useActivityRead(key, load, refresh);
  useListScrollRestoration(
    "import-history",
    scroller,
    !snapshot && !!read.current && !read.busy,
    key,
  );
  return (
    <div
      ref={setScroller}
      className="min-h-0 flex-1 overflow-y-auto"
      data-scroll-restoration-id="import-history"
    >
      <div className="mx-auto flex w-full max-w-4xl flex-col gap-6 p-4 md:p-6">
        <header className="flex flex-col gap-2">
          <h1 className="text-2xl font-semibold">
            {msg("import_history.title", "Import history")}
          </h1>
          <p className="text-muted-foreground">
            {msg(
              "import_history.description",
              "Inspect retained catalog and automation snapshots and the results of their import steps.",
            )}
          </p>
        </header>
        <ToggleGroup
          aria-label={msg("import_history.kind", "Snapshot type")}
          variant="outline"
          value={[kind]}
          onValueChange={(values) => {
            if (values[0])
              onChange(importHistorySearchSchema.parse({ kind: values[0] }));
          }}
        >
          <ToggleGroupItem value="catalog">
            {msg("import_history.catalogs", "Catalogs")}
          </ToggleGroupItem>
          <ToggleGroupItem value="automation">
            {msg("import_history.automation", "Automation")}
          </ToggleGroupItem>
        </ToggleGroup>
        {snapshot && (
          <ImportDetail
            key={`${kind}:${snapshot}`}
            api={api}
            kind={kind}
            id={snapshot}
            onBack={() => onChange({ ...search, snapshot: undefined })}
          />
        )}
        <div
          hidden={!!snapshot}
          className={cn("flex flex-col gap-4", snapshot && "hidden")}
        >
          <div>
            <Button
              variant="outline"
              disabled={read.busy}
              onClick={() => setRefresh((n) => n + 1)}
            >
              {msg("actions.refresh", "Refresh")}
            </Button>
          </div>
          {read.busy && (
            <Spinner
              aria-label={msg(
                "import_history.loading",
                "Loading import history",
              )}
            />
          )}
          {read.error !== undefined && (
            <ReviewError
              error={read.error}
              retry={() => setRefresh((n) => n + 1)}
            />
          )}
          {read.current?.map((row) => (
            <Card key={row.uuid}>
              <CardHeader>
                <CardTitle>
                  <SnapshotTitle row={row} />
                </CardTitle>
              </CardHeader>
              <CardContent>
                <SnapshotProgress row={row} />
              </CardContent>
              <CardFooter>
                <Button
                  variant="outline"
                  onClick={() => onChange({ ...search, snapshot: row.uuid })}
                >
                  {msg("import_history.inspect", "Inspect import")}
                </Button>
              </CardFooter>
            </Card>
          ))}
          {read.current?.length === 0 && (
            <PostEmpty
              title={msg("import_history.empty", "No snapshots on this page")}
            />
          )}
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              disabled={read.busy || previous.length === 0}
              onClick={() => {
                setAfter(previous.at(-1) ?? "");
                setPrevious((pages) => pages.slice(0, -1));
              }}
            >
              {msg("import_history.previous", "Previous page")}
            </Button>
            <Button
              variant="outline"
              disabled={read.busy || read.current?.length !== api.pageLimit}
              onClick={() => {
                const last = read.current?.at(-1);
                if (last) {
                  setPrevious((pages) => [...pages, after]);
                  setAfter(last.uuid);
                }
              }}
            >
              {msg("import_history.next", "Next page")}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
