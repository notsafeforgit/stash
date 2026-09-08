import { useEffect, useId, useState, useSyncExternalStore } from "react";
import { useIntl } from "react-intl";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "@/components/ui/field";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { QueryError } from "@/components/query-error";
import { useToast } from "@/hooks/toast";
import { entryDisplayTitle, listEntries } from "./offline-db";
import { getOfflineScope } from "./offline-scope";
import { legacyEntryDeployment } from "./offline-migration-policy";
import {
  getMigrationProgress,
  cancelAutomaticMigration,
  listRecoverySources,
  restoreDownloads,
  subscribeToMigration,
  type RecoverySource,
} from "./offline-migration";
import { canCoordinateDownloads } from "./use-download-queue";
import { OfflineSourceAddressForm } from "./offline-source-address-form";

const NO_SELECTION: ReadonlySet<string> = new Set();

/** Recovery is a local selection operation. Nothing is imported until the
 * selected rows' ownership is confirmed; the worker rechecks them under locks. */
export function OfflineRecoveryControl() {
  const intl = useIntl();
  const toast = useToast();
  const id = useId();
  const progress = useSyncExternalStore(
    subscribeToMigration,
    getMigrationProgress,
  );
  const [open, setOpen] = useState(false);
  const [revision, setRevision] = useState(0);
  const [data, setData] = useState<{
    sources: RecoverySource[];
    existing: ReadonlySet<string>;
    destination: string;
  }>();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<Error>();
  const [sourceKey, setSourceKey] = useState("");
  const [selection, setSelection] = useState<{
    source: string;
    ids: ReadonlySet<string>;
  }>({ source: "", ids: NO_SELECTION });
  const [operation, setOperation] = useState<AbortController>();
  const source =
    data?.sources.find((candidate) => candidate.databaseName === sourceKey) ??
    data?.sources[0];
  const selected =
    selection.source === source?.databaseName ? selection.ids : NO_SELECTION;
  const eligible =
    source?.entries.filter((entry) => !data?.existing.has(entry.scene_id)) ??
    [];
  const selectedIds = new Set(
    eligible
      .filter((entry) => selected.has(entry.scene_id))
      .map((entry) => entry.scene_id),
  );

  // biome-ignore lint/correctness/useExhaustiveDependencies: revision explicitly retries this persisted-data read after a user action.
  useEffect(() => {
    if (!open || progress.running) return;
    let disposed = false;
    setLoading(true);
    void Promise.all([listRecoverySources(), listEntries(), getOfflineScope()])
      .then(
        ([sources, entries, scope]) => {
          if (!disposed)
            setData({
              sources,
              existing: new Set(entries.map((entry) => entry.scene_id)),
              destination: scope.deploymentURL,
            });
        },
        (reason: unknown) => {
          if (!disposed)
            setError(
              reason instanceof Error ? reason : new Error(String(reason)),
            );
        },
      )
      .finally(() => {
        if (!disposed) setLoading(false);
      });
    return () => {
      disposed = true;
    };
    // Re-read sources after an import commits, including automatic migration.
  }, [open, revision, progress.running]);

  useEffect(() => () => operation?.abort(), [operation]);

  async function restore() {
    if (!source || selectedIds.size === 0 || operation) return;
    const controller = new AbortController();
    setOperation(controller);
    setError(undefined);
    try {
      const count = await restoreDownloads(
        source,
        selectedIds,
        controller.signal,
      );
      toast.success(
        intl.formatMessage(
          {
            id: "offline.recovery.restored",
            defaultMessage:
              "Restored {count, plural, one {# download} other {# downloads}}",
          },
          { count },
        ),
      );
      setSelection({ source: source.databaseName, ids: NO_SELECTION });
    } catch (reason) {
      if (!controller.signal.aborted)
        setError(reason instanceof Error ? reason : new Error(String(reason)));
    } finally {
      setOperation(undefined);
      setRevision((value) => value + 1);
    }
  }

  const sourceLabel = (candidate: RecoverySource) =>
    candidate.deploymentURL ??
    intl.formatMessage({
      id: "offline.recovery.legacy",
      defaultMessage: "Earlier Stash versions",
    });
  const status = progress.running
    ? intl.formatMessage(
        {
          id: "offline.recovery.progress",
          defaultMessage: "Restoring downloads: {completed} of {total}",
        },
        { completed: progress.completed, total: progress.total },
      )
    : progress.error
      ? intl.formatMessage({
          id: "offline.recovery.pending",
          defaultMessage: "Some saved downloads need attention.",
        })
      : null;

  return (
    <div className="flex flex-wrap items-center gap-3">
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        {progress.running && <Spinner data-icon="inline-start" />}
        {intl.formatMessage({
          id: "offline.recovery.action",
          defaultMessage: "Restore saved downloads",
        })}
      </Button>
      {status && (
        <span className="text-sm text-muted-foreground" role="status">
          {status}
        </span>
      )}
      <Dialog
        open={open}
        onOpenChange={(next) => {
          if (!next) operation?.abort();
          setOpen(next);
        }}
      >
        <DialogContent className="flex max-h-[85dvh] flex-col overflow-hidden sm:max-w-xl">
          <DialogHeader className="shrink-0">
            <DialogTitle>
              {intl.formatMessage({ id: "offline.recovery.action" })}
            </DialogTitle>
            <DialogDescription>
              {intl.formatMessage({
                id: "offline.recovery.description",
                defaultMessage:
                  "Choose downloads that belong to this library. Originals are kept, and downloads already saved here are not replaced. Close tabs running older Stash versions before restoring. Copying downloads needs additional free space.",
              })}
            </DialogDescription>
          </DialogHeader>
          <div className="flex min-h-0 flex-col gap-4 overflow-y-auto">
            {data && (
              <p className="text-sm">
                <code className="break-all">{data.destination}</code>
              </p>
            )}
            {loading && (
              <Spinner
                aria-label={intl.formatMessage({
                  id: "offline.recovery.loading",
                  defaultMessage: "Loading",
                })}
              />
            )}
            {error && (
              <QueryError
                error={error}
                retry={async () => {
                  setError(undefined);
                  setRevision((value) => value + 1);
                }}
                retrying={loading}
              />
            )}
            {!error && progress.error && (
              <FieldError>{progress.error.message}</FieldError>
            )}
            {data && typeof indexedDB.databases !== "function" && (
              <OfflineSourceAddressForm
                disabled={!!operation || progress.running}
                onFound={(candidate) => {
                  setData((previous) =>
                    previous
                      ? {
                          ...previous,
                          sources: [
                            ...previous.sources.filter(
                              (source) =>
                                source.databaseName !== candidate.databaseName,
                            ),
                            candidate,
                          ],
                        }
                      : previous,
                  );
                  setSourceKey(candidate.databaseName);
                  setSelection({
                    source: candidate.databaseName,
                    ids: NO_SELECTION,
                  });
                }}
              />
            )}
            {data && !data.sources.length && !loading && (
              <p className="text-sm text-muted-foreground">
                {intl.formatMessage({
                  id: "offline.recovery.empty",
                  defaultMessage:
                    "No downloads from earlier versions or other server addresses were found in this browser.",
                })}
              </p>
            )}
            {source && (
              <FieldGroup>
                <Field>
                  <FieldLabel htmlFor={`${id}-source`}>
                    {intl.formatMessage({
                      id: "offline.recovery.source",
                      defaultMessage: "Saved library",
                    })}
                  </FieldLabel>
                  <Select
                    value={source.databaseName}
                    disabled={!!operation || progress.running}
                    onValueChange={(value: string | null) => {
                      if (value) {
                        setSourceKey(value);
                        setSelection({ source: value, ids: NO_SELECTION });
                      }
                    }}
                  >
                    <SelectTrigger id={`${id}-source`}>
                      <SelectValue>{sourceLabel(source)}</SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        {data?.sources.map((candidate) => (
                          <SelectItem
                            key={candidate.databaseName}
                            value={candidate.databaseName}
                          >
                            {sourceLabel(candidate)}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </Field>
                {source.invalid > 0 && (
                  <p role="status" className="text-sm text-muted-foreground">
                    {intl.formatMessage(
                      {
                        id: "offline.recovery.invalid",
                        defaultMessage:
                          "{count, plural, one {# saved entry could not be read. Its original data has been kept.} other {# saved entries could not be read. Their original data has been kept.}}",
                      },
                      { count: source.invalid },
                    )}
                  </p>
                )}
                <div className="flex gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={!!operation || loading || progress.running}
                    onClick={() =>
                      setSelection({
                        source: source.databaseName,
                        ids: new Set(eligible.map((entry) => entry.scene_id)),
                      })
                    }
                  >
                    {intl.formatMessage({
                      id: "actions.select_all",
                      defaultMessage: "Select all",
                    })}
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={!!operation}
                    onClick={() =>
                      setSelection({
                        source: source.databaseName,
                        ids: NO_SELECTION,
                      })
                    }
                  >
                    {intl.formatMessage({
                      id: "actions.select_none",
                      defaultMessage: "Select none",
                    })}
                  </Button>
                </div>
                <FieldSet>
                  <FieldLegend>
                    {intl.formatMessage({
                      id: "offline.recovery.downloads",
                      defaultMessage: "Downloads to restore",
                    })}
                  </FieldLegend>
                  <FieldGroup className="p-1">
                    {source.entries.map((entry) => {
                      const exists = data?.existing.has(entry.scene_id);
                      const disabled =
                        exists || !!operation || progress.running;
                      const originalServer =
                        source.deploymentURL ?? legacyEntryDeployment(entry);
                      return (
                        <Field
                          key={entry.scene_id}
                          orientation="horizontal"
                          data-disabled={disabled}
                        >
                          <Checkbox
                            id={`${id}-${entry.scene_id}`}
                            disabled={disabled}
                            checked={selectedIds.has(entry.scene_id)}
                            onCheckedChange={(checked) => {
                              setSelection((previous) => {
                                const ids = new Set(
                                  previous.source === source.databaseName
                                    ? previous.ids
                                    : NO_SELECTION,
                                );
                                if (checked) ids.add(entry.scene_id);
                                else ids.delete(entry.scene_id);
                                return { source: source.databaseName, ids };
                              });
                            }}
                          />
                          <FieldContent>
                            <FieldLabel htmlFor={`${id}-${entry.scene_id}`}>
                              {entryDisplayTitle(entry)}
                            </FieldLabel>
                            <FieldDescription>
                              {originalServer ? (
                                <code className="break-all">
                                  {originalServer}
                                </code>
                              ) : (
                                intl.formatMessage({
                                  id: "offline.recovery.unknown",
                                  defaultMessage: "Original server unknown",
                                })
                              )}
                            </FieldDescription>
                            {entry.source_file_path && (
                              <FieldDescription>
                                <code className="break-all">
                                  {entry.source_file_path}
                                </code>
                              </FieldDescription>
                            )}
                            {exists && (
                              <FieldDescription>
                                {intl.formatMessage({
                                  id: "offline.recovery.exists",
                                  defaultMessage: "Already saved here",
                                })}
                              </FieldDescription>
                            )}
                          </FieldContent>
                        </Field>
                      );
                    })}
                  </FieldGroup>
                </FieldSet>
              </FieldGroup>
            )}
            {status && (
              <p className="text-sm" role="status">
                {status}
              </p>
            )}
          </div>
          <DialogFooter className="shrink-0">
            <Button
              variant="outline"
              onClick={() => {
                if (operation) operation.abort();
                else cancelAutomaticMigration();
                setOpen(false);
              }}
            >
              {intl.formatMessage({
                id: "actions.cancel",
                defaultMessage: "Cancel",
              })}
            </Button>
            <Button
              disabled={
                !canCoordinateDownloads() ||
                !!operation ||
                progress.running ||
                loading ||
                selectedIds.size === 0
              }
              onClick={() => void restore()}
            >
              {operation && <Spinner data-icon="inline-start" />}
              {intl.formatMessage(
                {
                  id: "offline.recovery.confirm",
                  defaultMessage: "These belong here — restore {count}",
                },
                { count: selectedIds.size },
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
