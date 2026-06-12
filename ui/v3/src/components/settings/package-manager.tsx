/**
 * Package manager shared by the Metadata Providers (scrapers) and Plugins
 * settings pages. Three concerns:
 *
 *  - Installed: lists installed packages; "Check for updates" re-queries
 *    with each package's source counterpart so out-of-date rows can be
 *    selected and updated; uninstall (with confirmation) removes the
 *    selection.
 *  - Available: pick one of the configured package sources, browse its
 *    index, select (dependencies are pulled in automatically) and install.
 *    Already-installed packages are flagged and not re-installable.
 *  - Sources: add / edit / delete the package sources themselves, saved to
 *    the server's general configuration.
 *
 * Install / update / uninstall run as background jobs server-side. While a
 * job started here is running, the action buttons are disabled and a status
 * bar shows its progress (via the jobs progress subscription); when the
 * job's lifecycle REMOVE event arrives the installed list refetches and the
 * outcome is toasted, so the lists stay accurate without manual refreshes.
 */
import { useMemo, useRef, useState } from "react";
import { type IntlShape, useIntl } from "react-intl";
import { useForm } from "@tanstack/react-form";
import {
  useApolloClient,
  useLazyQuery,
  useMutation,
  useQuery,
  useSubscription,
} from "@apollo/client/react";
import { ArrowUp, Pencil, Plus, RefreshCw, Trash2 } from "lucide-react";
import { z } from "zod";
import * as GQL from "src/core/generated-graphql";
import { useConfigurationContext, useConfigureGeneral } from "src/hooks/config";
import { useToast } from "src/hooks/toast";
import { useMsg } from "src/hooks/message";
import { cn } from "src/lib/utils";
import { Badge } from "src/components/ui/badge";
import { Button } from "src/components/ui/button";
import { Checkbox } from "src/components/ui/checkbox";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "src/components/ui/dialog";
import { Field, FieldError, FieldLabel } from "src/components/ui/field";
import { Input } from "src/components/ui/input";
import { Progress } from "src/components/ui/progress";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "src/components/ui/select";
import { ScrollArea } from "src/components/ui/scroll-area";
import { Spinner } from "src/components/ui/spinner";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "src/components/ui/table";
import { DestructiveConfirmDialog } from "src/components/shared/destructive-confirm-dialog";

interface SourceEntry {
  name?: string | null;
  url: string;
  local_path?: string | null;
}

interface PackageRow {
  package_id: string;
  name: string;
  version?: string | null;
  date?: string | null;
  sourceURL: string;
  description?: string | null;
  /** package_ids this package depends on (available rows only). */
  requires: string[];
  /** Source counterpart, present once "Check for updates" has run. */
  latest?: { version?: string | null; date?: string | null };
}

interface PackageSpec {
  id: string;
  sourceURL: string;
}

type JobKind = "install" | "update" | "uninstall";

function rowKey(p: { package_id: string; sourceURL: string }) {
  return `${p.sourceURL}|${p.package_id}`;
}

/**
 * Prefer comparing release dates (matches v2.5 and survives non-semver
 * version strings); fall back to a version mismatch when either side has
 * no date.
 */
function hasUpgrade(row: PackageRow) {
  if (!row.latest) return false;
  if (row.date && row.latest.date)
    return new Date(row.latest.date) > new Date(row.date);
  return !!row.latest.version && row.latest.version !== row.version;
}

function matchesFilter(row: PackageRow, filter: string) {
  if (!filter) return true;
  const f = filter.toLowerCase();
  return (
    row.name.toLowerCase().includes(f) ||
    row.package_id.toLowerCase().includes(f)
  );
}

function formatDate(intl: IntlShape, date: string | null | undefined) {
  if (!date) return undefined;
  return intl.formatDate(new Date(date), { timeZone: "utc" });
}

// The plain Installed*Packages queries omit source_package; only the
// *Status variants select it. Narrow a row of the union accordingly.
function hasSourcePackage(
  p: object,
): p is { source_package?: GQL.PackageDataFragment | null } {
  return "source_package" in p;
}

function packageDescription(metadata: { [key: string]: unknown }) {
  const description = metadata.description;
  return typeof description === "string" ? description : undefined;
}

function PackageTable({
  rows,
  loading,
  busy,
  selected,
  onToggle,
  onToggleAll,
  emptyMessage,
  showLatest = false,
  showDescription = false,
  installedIds,
}: {
  rows: PackageRow[];
  loading: boolean;
  /** A package job is running — selection is frozen. */
  busy: boolean;
  selected: Set<string>;
  onToggle: (key: string) => void;
  onToggleAll: (keys: string[], check: boolean) => void;
  emptyMessage: React.ReactNode;
  showLatest?: boolean;
  showDescription?: boolean;
  /** package_ids that are already installed (available list only). */
  installedIds?: Set<string>;
}) {
  const intl = useIntl();

  const selectableKeys = rows
    .filter((r) => !installedIds?.has(r.package_id))
    .map(rowKey);
  const allChecked =
    selectableKeys.length > 0 && selectableKeys.every((k) => selected.has(k));

  if (loading && rows.length === 0) return <Spinner className="size-5" />;
  if (rows.length === 0)
    return <p className="text-sm text-muted-foreground">{emptyMessage}</p>;

  // Height-capped with internal scroll (via the styled ScrollArea, not
  // native overflow) so long package lists don't swallow the settings page;
  // the header stays pinned and needs a solid background so scrolled rows
  // don't show through it. The Table's own overflow-x-auto container is
  // neutralised — a nested scroll container would detach the sticky header
  // from the viewport that actually scrolls.
  return (
    <ScrollArea
      className="rounded-lg border"
      viewportClassName="max-h-96"
      horizontal
    >
      <Table containerClassName="overflow-x-visible">
        <TableHeader className="sticky top-0 z-10 bg-muted">
          <TableRow>
            <TableHead className="w-8">
              <Checkbox
                checked={allChecked}
                onCheckedChange={(v) => onToggleAll(selectableKeys, v === true)}
                disabled={busy || selectableKeys.length === 0}
                aria-label={intl.formatMessage({
                  id: "actions.select_all",
                  defaultMessage: "Select all",
                })}
              />
            </TableHead>
            <TableHead>
              {intl.formatMessage({
                id: "package_manager.package",
                defaultMessage: "Package",
              })}
            </TableHead>
            <TableHead>
              {intl.formatMessage({
                id: showLatest
                  ? "package_manager.installed_version"
                  : "package_manager.version",
                defaultMessage: showLatest ? "Installed version" : "Version",
              })}
            </TableHead>
            {showLatest && (
              <TableHead>
                {intl.formatMessage({
                  id: "package_manager.latest_version",
                  defaultMessage: "Latest version",
                })}
              </TableHead>
            )}
            {showDescription && (
              <TableHead>
                {intl.formatMessage({
                  id: "package_manager.description",
                  defaultMessage: "Description",
                })}
              </TableHead>
            )}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((row) => {
            const key = rowKey(row);
            const installed = installedIds?.has(row.package_id) ?? false;
            const upgrade = hasUpgrade(row);
            const selectable = !busy && !installed;
            return (
              <TableRow
                key={key}
                className={cn(selectable && "cursor-pointer")}
                onClick={() => selectable && onToggle(key)}
              >
                <TableCell
                  className="align-top"
                  onClick={(e) => e.stopPropagation()}
                >
                  <Checkbox
                    checked={selected.has(key)}
                    onCheckedChange={() => onToggle(key)}
                    disabled={busy || installed}
                    aria-label={intl.formatMessage(
                      {
                        id: "package_manager.select_package",
                        defaultMessage: "Select {name}",
                      },
                      { name: row.name },
                    )}
                  />
                </TableCell>
                <TableCell className="align-top">
                  <div className="flex flex-wrap items-center gap-2">
                    <span>{row.name}</span>
                    {installed && (
                      <Badge variant="secondary">
                        {intl.formatMessage({
                          id: "package_manager.installed_label",
                          defaultMessage: "Installed",
                        })}
                      </Badge>
                    )}
                  </div>
                  <div className="text-xs text-muted-foreground">
                    {row.package_id}
                  </div>
                </TableCell>
                <TableCell className="align-top whitespace-nowrap">
                  <div>{row.version || "—"}</div>
                  {row.date && (
                    <div className="text-xs text-muted-foreground">
                      {formatDate(intl, row.date)}
                    </div>
                  )}
                </TableCell>
                {showLatest && (
                  <TableCell className="align-top whitespace-nowrap">
                    {row.latest ? (
                      <>
                        <div
                          className={cn(
                            upgrade &&
                              "flex items-center gap-1 font-medium text-primary",
                          )}
                        >
                          {row.latest.version || "—"}
                          {upgrade && <ArrowUp className="size-3.5" />}
                        </div>
                        {row.latest.date && (
                          <div className="text-xs text-muted-foreground">
                            {formatDate(intl, row.latest.date)}
                          </div>
                        )}
                      </>
                    ) : (
                      // The update check found no counterpart for this
                      // package in its source (delisted, renamed, or the
                      // source was unreachable) — there is no version to
                      // compare against.
                      <span className="text-xs text-muted-foreground italic">
                        {intl.formatMessage({
                          id: "package_manager.not_in_source",
                          defaultMessage: "Not found in source",
                        })}
                      </span>
                    )}
                  </TableCell>
                )}
                {showDescription && (
                  <TableCell className="align-top">
                    {row.description && (
                      <div className="text-sm text-muted-foreground">
                        {row.description}
                      </div>
                    )}
                    {row.requires.length > 0 && (
                      <div className="text-xs text-muted-foreground italic">
                        {intl.formatMessage(
                          {
                            id: "package_manager.requires",
                            defaultMessage: "Requires {packages}",
                          },
                          { packages: row.requires.join(", ") },
                        )}
                      </div>
                    )}
                  </TableCell>
                )}
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </ScrollArea>
  );
}

function useSelection() {
  const [selected, setSelected] = useState<Set<string>>(new Set());

  function toggle(key: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }

  function toggleAll(keys: string[], check: boolean) {
    setSelected((prev) => {
      const next = new Set(prev);
      for (const k of keys) {
        if (check) next.add(k);
        else next.delete(k);
      }
      return next;
    });
  }

  function clear() {
    setSelected(new Set());
  }

  /** Replace the whole selection (e.g. "select exactly the updates"). */
  function replace(keys: string[]) {
    setSelected(new Set(keys));
  }

  return { selected, toggle, toggleAll, clear, replace };
}

function SourceEditDialog({
  existing,
  otherSources,
  onSave,
  onClose,
}: {
  existing?: SourceEntry;
  /** All configured sources except `existing` — for uniqueness checks. */
  otherSources: SourceEntry[];
  onSave: (value: SourceEntry) => void;
  onClose: () => void;
}) {
  const intl = useIntl();

  const requiredMessage = intl.formatMessage({
    id: "validation.required_field",
    defaultMessage: "Required",
  });
  const uniqueMessage = intl.formatMessage({
    id: "validation.unique_value",
    defaultMessage: "Must be unique",
  });

  const nameSchema = z
    .string()
    .min(1, requiredMessage)
    .refine((v) => !otherSources.some((s) => s.name === v), uniqueMessage);
  const urlSchema = z
    .string()
    .min(1, requiredMessage)
    .refine((v) => !otherSources.some((s) => s.url === v), uniqueMessage);

  const form = useForm({
    defaultValues: {
      name: existing?.name ?? "",
      url: existing?.url ?? "",
      local_path: existing?.local_path ?? "",
    },
    onSubmit: ({ value }) => {
      onSave({
        name: value.name.trim(),
        url: value.url.trim(),
        local_path: value.local_path.trim() || undefined,
      });
      onClose();
    },
  });

  function fieldError(field: {
    name: string;
    state: { meta: { isTouched: boolean; errors: unknown[] } };
  }) {
    const hasError =
      field.state.meta.isTouched && field.state.meta.errors.length > 0;
    return hasError ? (
      <FieldError
        id={`${field.name}-error`}
        errors={field.state.meta.errors.map((e) =>
          e != null ? { message: String(e) } : undefined,
        )}
      />
    ) : null;
  }

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {intl.formatMessage({
              id: existing
                ? "package_manager.edit_source"
                : "package_manager.add_source",
              defaultMessage: existing ? "Edit source" : "Add source",
            })}
          </DialogTitle>
        </DialogHeader>
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            void form.handleSubmit();
          }}
        >
          <form.Field name="name" validators={{ onChange: nameSchema }}>
            {(field) => (
              <Field>
                <FieldLabel htmlFor={field.name}>
                  {intl.formatMessage({
                    id: "package_manager.source.name",
                    defaultMessage: "Name",
                  })}
                </FieldLabel>
                <Input
                  id={field.name}
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(e) => field.handleChange(e.target.value)}
                  spellCheck={false}
                />
                {fieldError(field)}
              </Field>
            )}
          </form.Field>
          <form.Field name="url" validators={{ onChange: urlSchema }}>
            {(field) => (
              <Field>
                <FieldLabel htmlFor={field.name}>
                  {intl.formatMessage({
                    id: "package_manager.source.url",
                    defaultMessage: "Source URL",
                  })}
                </FieldLabel>
                <Input
                  id={field.name}
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(e) => field.handleChange(e.target.value)}
                  spellCheck={false}
                  autoComplete="off"
                />
                {fieldError(field)}
              </Field>
            )}
          </form.Field>
          <form.Field name="local_path">
            {(field) => (
              <Field>
                <FieldLabel htmlFor={field.name}>
                  {intl.formatMessage({
                    id: "package_manager.source.local_path.heading",
                    defaultMessage: "Local path",
                  })}
                </FieldLabel>
                <Input
                  id={field.name}
                  value={field.state.value}
                  onChange={(e) => field.handleChange(e.target.value)}
                  spellCheck={false}
                />
                <p className="text-xs text-muted-foreground">
                  {intl.formatMessage({
                    id: "package_manager.source.local_path.description",
                    defaultMessage:
                      "Relative path to store packages for this source. Note that changing this requires the packages to be moved manually.",
                  })}
                </p>
              </Field>
            )}
          </form.Field>
          <DialogFooter>
            <DialogClose
              render={
                <Button type="button" variant="outline">
                  {intl.formatMessage({
                    id: "actions.cancel",
                    defaultMessage: "Cancel",
                  })}
                </Button>
              }
            />
            <form.Subscribe selector={(s) => ({ canSubmit: s.canSubmit })}>
              {({ canSubmit }) => (
                <Button type="submit" disabled={!canSubmit}>
                  {intl.formatMessage({
                    id: "actions.confirm",
                    defaultMessage: "Confirm",
                  })}
                </Button>
              )}
            </form.Subscribe>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export function PackageManager({
  type,
  onPackagesChanged,
}: {
  type: "scraper" | "plugin";
  /** Called after one of this manager's jobs finishes, so the page can
   * refresh whatever the packages feed (plugin list, scraper lists). */
  onPackagesChanged?: () => void;
}) {
  const intl = useIntl();
  const Toast = useToast();
  const apolloClient = useApolloClient();
  const { configuration } = useConfigurationContext();
  const [configureGeneral] = useConfigureGeneral();

  const isPlugin = type === "plugin";

  const sources: SourceEntry[] =
    (isPlugin
      ? configuration.general.pluginPackageSources
      : configuration.general.scraperPackageSources) ?? [];

  // Per-type documents. The scraper and plugin operations have
  // structurally identical result shapes, so everything below the
  // document selection is shared and stays fully typed via the unions.
  const [installMutation] = useMutation(
    isPlugin
      ? GQL.InstallPluginPackagesDocument
      : GQL.InstallScraperPackagesDocument,
  );
  const [updateMutation] = useMutation(
    isPlugin
      ? GQL.UpdatePluginPackagesDocument
      : GQL.UpdateScraperPackagesDocument,
  );
  const [uninstallMutation] = useMutation(
    isPlugin
      ? GQL.UninstallPluginPackagesDocument
      : GQL.UninstallScraperPackagesDocument,
  );

  const msg = useMsg();

  // ── Job tracking ───────────────────────────────────────────────────────────
  // Install / update / uninstall return a job id. While that job runs, the
  // action buttons are frozen and a status bar shows progress; the job's
  // lifecycle REMOVE event ends it. The ref mirrors the state so the
  // always-mounted subscription callback never reads a stale value.
  const [activeJob, setActiveJob] = useState<{
    id: string;
    kind: JobKind;
  } | null>(null);
  const activeJobRef = useRef<typeof activeJob>(null);
  const [jobProgress, setJobProgress] = useState<number | null>(null);
  const busy = !!activeJob;

  const completeMessages: Record<JobKind, string> = {
    install: msg(
      "package_manager.install_complete",
      "Package install complete",
    ),
    update: msg("package_manager.update_complete", "Package update complete"),
    uninstall: msg(
      "package_manager.uninstall_complete",
      "Package uninstall complete",
    ),
  };
  const runningMessages: Record<JobKind, string> = {
    install: msg("package_manager.installing", "Installing packages…"),
    update: msg("package_manager.updating", "Updating packages…"),
    uninstall: msg("package_manager.uninstalling", "Uninstalling packages…"),
  };

  function finishJob(jobId: string, error?: string | null) {
    if (activeJobRef.current?.id !== jobId) return;
    const kind = activeJobRef.current.kind;
    activeJobRef.current = null;
    setActiveJob(null);
    setJobProgress(null);
    if (error) Toast.error(error);
    else Toast.success(completeMessages[kind]);
    void refetchInstalled();
    onPackagesChanged?.();
  }

  useSubscription(GQL.JobsLifecycleSubscribeDocument, {
    onData: ({ data: payload }) => {
      const event = payload.data?.jobsLifecycleSubscribe;
      if (!event || event.type !== GQL.JobStatusUpdateType.Remove) return;
      finishJob(event.job.id, event.job.error);
    },
  });

  useSubscription(GQL.JobsProgressSubscribeDocument, {
    skip: !activeJob,
    onData: ({ data: payload }) => {
      const event = payload.data?.jobsProgressSubscribe;
      if (!event || event.job.id !== activeJobRef.current?.id) return;
      if (typeof event.job.progress === "number")
        setJobProgress(event.job.progress);
    },
  });

  async function startPackageJob(
    kind: JobKind,
    run: () => Promise<string | null | undefined>,
  ) {
    try {
      const jobID = await run();
      if (!jobID) return;
      activeJobRef.current = { id: jobID, kind };
      setActiveJob({ id: jobID, kind });
      // A small package job can finish before this point — its REMOVE
      // event arrives while activeJobRef is still null and gets dropped.
      // Verify against findJob and settle immediately if the job is gone
      // *or already terminal*: the server keeps finished jobs in a
      // graveyard for status reporting, so findJob still returns them
      // (status FINISHED/FAILED/CANCELLED) after the queue REMOVE.
      // finishJob is idempotent, so racing the real REMOVE is harmless.
      const { data } = await apolloClient.query({
        query: GQL.FindJobDocument,
        variables: { input: { id: jobID } },
        fetchPolicy: "network-only",
      });
      const found = data?.findJob;
      const terminal =
        !found ||
        found.status === GQL.JobStatus.Finished ||
        found.status === GQL.JobStatus.Failed ||
        found.status === GQL.JobStatus.Cancelled;
      if (terminal) finishJob(jobID, found?.error);
    } catch (e) {
      Toast.error(e);
    }
  }

  // ── Installed ──────────────────────────────────────────────────────────────
  const [checkForUpgrades, setCheckForUpgrades] = useState(false);
  const [installedFilter, setInstalledFilter] = useState("");

  const installedQueryDoc = checkForUpgrades
    ? isPlugin
      ? GQL.InstalledPluginPackagesStatusDocument
      : GQL.InstalledScraperPackagesStatusDocument
    : isPlugin
      ? GQL.InstalledPluginPackagesDocument
      : GQL.InstalledScraperPackagesDocument;

  const {
    data: installedData,
    previousData: previousInstalledData,
    loading: installedLoading,
    refetch: refetchInstalled,
  } = useQuery(installedQueryDoc, { fetchPolicy: "cache-and-network" });

  // Keep showing the last result while the *Status re-query is in flight
  // (switching documents resets `data` to undefined until it lands).
  const installedPackages = (installedData ?? previousInstalledData)
    ?.installedPackages;

  const installedRows: PackageRow[] = useMemo(
    () =>
      (installedPackages ?? [])
        .map((p): PackageRow => {
          // Only the *Status query variants select source_package.
          const sourcePackage = hasSourcePackage(p)
            ? p.source_package
            : undefined;
          return {
            package_id: p.package_id,
            name: p.name,
            version: p.version,
            date: p.date,
            sourceURL: p.sourceURL,
            requires: [],
            latest: sourcePackage
              ? { version: sourcePackage.version, date: sourcePackage.date }
              : undefined,
          };
        })
        // Upgradable packages first, then stable order by id.
        .sort(
          (a, b) =>
            Number(hasUpgrade(b)) - Number(hasUpgrade(a)) ||
            a.package_id.localeCompare(b.package_id),
        ),
    [installedPackages],
  );

  const updatesLoaded = checkForUpgrades && !installedLoading;
  const updateCount = updatesLoaded
    ? installedRows.filter(hasUpgrade).length
    : 0;

  const visibleInstalledRows = useMemo(
    () => installedRows.filter((r) => matchesFilter(r, installedFilter)),
    [installedRows, installedFilter],
  );

  const installedSelection = useSelection();
  const installedIdSet = useMemo(
    () => new Set(installedRows.map((r) => r.package_id)),
    [installedRows],
  );

  const [confirmingUninstall, setConfirmingUninstall] = useState(false);

  function onCheckForUpdates() {
    // The first check swaps to the *Status document, which queries the
    // sources by itself; only re-run it explicitly on subsequent checks.
    if (!checkForUpgrades) setCheckForUpgrades(true);
    else void refetchInstalled();
  }

  // ── Available ──────────────────────────────────────────────────────────────
  const [sourceURL, setSourceURL] = useState<string>(sources[0]?.url ?? "");
  const [availableFilter, setAvailableFilter] = useState("");
  // Which source the current availableData belongs to; the list is hidden
  // when it doesn't match the selected source (stale after edits/switches).
  const [loadedSourceURL, setLoadedSourceURL] = useState<string | null>(null);

  const effectiveSourceURL = sources.some((s) => s.url === sourceURL)
    ? sourceURL
    : (sources[0]?.url ?? "");

  const [loadAvailable, { data: availableData, loading: availableLoading }] =
    useLazyQuery(
      isPlugin
        ? GQL.AvailablePluginPackagesDocument
        : GQL.AvailableScraperPackagesDocument,
      { fetchPolicy: "cache-and-network" },
    );

  const availableSelection = useSelection();

  function loadSource(url: string) {
    setLoadedSourceURL(url);
    void loadAvailable({ variables: { source: url } });
  }

  function onChangeSource(url: string | null) {
    if (url === null) return;
    setSourceURL(url);
    availableSelection.clear();
    setAvailableFilter("");
    // Once the user has loaded one source, switching loads the next one
    // without requiring another explicit "Load packages" click.
    if (loadedSourceURL) loadSource(url);
  }

  const availableRows: PackageRow[] = useMemo(
    () =>
      (availableData?.availablePackages ?? []).map(
        (p): PackageRow => ({
          package_id: p.package_id,
          name: p.name,
          version: p.version,
          date: p.date,
          sourceURL: effectiveSourceURL,
          description: packageDescription(p.metadata),
          requires: p.requires.map((r) => r.package_id),
        }),
      ),
    [availableData, effectiveSourceURL],
  );

  const availableById = useMemo(
    () => new Map(availableRows.map((r) => [r.package_id, r])),
    [availableRows],
  );

  const visibleAvailableRows = useMemo(
    () => availableRows.filter((r) => matchesFilter(r, availableFilter)),
    [availableRows, availableFilter],
  );

  const availableLoaded = loadedSourceURL === effectiveSourceURL;

  /** Selecting a package also selects its (transitive) dependencies. */
  function toggleAvailableRow(key: string) {
    if (availableSelection.selected.has(key)) {
      availableSelection.toggle(key);
      return;
    }
    const row = availableRows.find((r) => rowKey(r) === key);
    if (!row) return;
    const keys = [key];
    const seen = new Set([row.package_id]);
    const queue = [...row.requires];
    while (queue.length > 0) {
      const id = queue.pop();
      if (!id || seen.has(id) || installedIdSet.has(id)) continue;
      seen.add(id);
      const dep = availableById.get(id);
      if (dep) {
        keys.push(rowKey(dep));
        queue.push(...dep.requires);
      }
    }
    availableSelection.toggleAll(keys, true);
  }

  // ── Sources ────────────────────────────────────────────────────────────────
  const [sourceDialog, setSourceDialog] = useState<{
    existing?: SourceEntry;
  } | null>(null);
  const [deletingSource, setDeletingSource] = useState<SourceEntry | null>(
    null,
  );

  // Failures are toasted by the config hook itself.
  function saveSources(next: SourceEntry[]) {
    const inputs = next.map((s) => ({
      name: s.name || undefined,
      url: s.url,
      local_path: s.local_path || undefined,
    }));
    return configureGeneral({
      variables: {
        input: isPlugin
          ? { pluginPackageSources: inputs }
          : { scraperPackageSources: inputs },
      },
    });
  }

  function onSourceSaved(value: SourceEntry) {
    const existing = sourceDialog?.existing;
    if (existing) {
      void saveSources(
        sources.map((s) => (s.url === existing.url ? value : s)),
      );
    } else {
      void saveSources([...sources, value]);
      setSourceURL(value.url);
    }
  }

  const effectiveSource = sources.find((s) => s.url === effectiveSourceURL);

  // ── Selection → specs ──────────────────────────────────────────────────────
  function specsFromSelection(rows: PackageRow[], selected: Set<string>) {
    return rows
      .filter((r) => selected.has(rowKey(r)))
      .map((r): PackageSpec => ({ id: r.package_id, sourceURL: r.sourceURL }));
  }

  const installedSpecs = specsFromSelection(
    installedRows,
    installedSelection.selected,
  );
  const availableSpecs = specsFromSelection(
    availableRows,
    availableSelection.selected,
  );

  function withCount(label: string, count: number) {
    return count > 0 ? `${label} (${count})` : label;
  }

  const checkingForUpdates = checkForUpgrades && installedLoading;
  const jobProgressPct =
    jobProgress !== null ? Math.round(jobProgress * 100) : null;

  return (
    <div className="space-y-8">
      {activeJob && (
        <div className="flex items-center gap-3 rounded-md border bg-muted/30 px-3 py-2">
          <Spinner className="size-4 shrink-0" />
          <span className="text-sm whitespace-nowrap">
            {runningMessages[activeJob.kind]}
          </span>
          {jobProgressPct !== null && (
            <>
              <Progress value={jobProgressPct} className="flex-1" />
              <span className="w-10 text-right text-xs tabular-nums text-muted-foreground">
                {jobProgressPct}%
              </span>
            </>
          )}
        </div>
      )}

      <section className="space-y-3">
        <h3 className="text-sm font-medium">
          {msg("package_manager.installed", "Installed packages")}
        </h3>
        <div className="flex flex-wrap items-center gap-2">
          <Input
            className="w-56"
            placeholder={`${msg("filter", "Filter")}...`}
            value={installedFilter}
            onChange={(e) => setInstalledFilter(e.currentTarget.value)}
          />
          {updatesLoaded && (
            <Badge variant={updateCount > 0 ? "default" : "secondary"}>
              {updateCount > 0
                ? intl.formatMessage(
                    {
                      id: "package_manager.updates_available",
                      defaultMessage:
                        "{count, plural, one {# update available} other {# updates available}}",
                    },
                    { count: updateCount },
                  )
                : msg("package_manager.up_to_date", "Everything up to date")}
            </Badge>
          )}
          {updatesLoaded && updateCount > 0 && (
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={busy}
              onClick={() =>
                installedSelection.replace(
                  installedRows.filter(hasUpgrade).map(rowKey),
                )
              }
            >
              {msg("package_manager.select_updates", "Select updates")}
            </Button>
          )}
          <div className="grow" />
          <Button
            type="button"
            variant="outline"
            disabled={busy || checkingForUpdates}
            onClick={onCheckForUpdates}
          >
            {checkingForUpdates ? (
              <Spinner className="size-4" />
            ) : (
              <RefreshCw className="size-4" />
            )}
            {msg("package_manager.check_for_updates", "Check for updates")}
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={busy || installedSpecs.length === 0}
            onClick={() => {
              void startPackageJob("update", async () => {
                const result = await updateMutation({
                  variables: { packages: installedSpecs },
                });
                installedSelection.clear();
                return result.data?.updatePackages;
              });
            }}
          >
            {withCount(
              msg("package_manager.update_selected", "Update selected"),
              installedSpecs.length,
            )}
          </Button>
          <Button
            type="button"
            variant="destructive"
            disabled={busy || installedSpecs.length === 0}
            onClick={() => setConfirmingUninstall(true)}
          >
            {withCount(
              msg("package_manager.uninstall_selected", "Uninstall selected"),
              installedSpecs.length,
            )}
          </Button>
        </div>
        <PackageTable
          rows={visibleInstalledRows}
          loading={installedLoading && installedRows.length === 0}
          busy={busy}
          selected={installedSelection.selected}
          onToggle={installedSelection.toggle}
          onToggleAll={installedSelection.toggleAll}
          showLatest={updatesLoaded}
          emptyMessage={
            installedFilter
              ? msg(
                  "package_manager.no_matching",
                  "No packages match the filter.",
                )
              : msg(
                  "package_manager.no_packages_installed",
                  "No packages installed.",
                )
          }
        />
      </section>

      <section className="space-y-3">
        <h3 className="text-sm font-medium">
          {msg("package_manager.available", "Available packages")}
        </h3>
        {sources.length === 0 ? (
          <div className="space-y-2">
            <p className="text-sm text-muted-foreground">
              {msg("package_manager.no_sources", "No sources configured")}
            </p>
            <Button
              type="button"
              variant="outline"
              onClick={() => setSourceDialog({})}
            >
              <Plus className="size-4" />
              {msg("package_manager.add_source", "Add source")}
            </Button>
          </div>
        ) : (
          <>
            <div className="flex flex-wrap items-center gap-2">
              <Select value={effectiveSourceURL} onValueChange={onChangeSource}>
                <SelectTrigger className="w-72 max-w-full">
                  <SelectValue>
                    {effectiveSource?.name ?? effectiveSourceURL}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {sources.map((s) => (
                    <SelectItem key={s.url} value={s.url}>
                      {s.name ?? s.url}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button
                type="button"
                variant="outline"
                size="icon"
                aria-label={msg("package_manager.edit_source", "Edit source")}
                onClick={() =>
                  effectiveSource &&
                  setSourceDialog({ existing: effectiveSource })
                }
              >
                <Pencil className="size-4" />
              </Button>
              <Button
                type="button"
                variant="outline"
                size="icon"
                aria-label={msg("actions.delete", "Delete")}
                onClick={() =>
                  effectiveSource && setDeletingSource(effectiveSource)
                }
              >
                <Trash2 className="size-4" />
              </Button>
              <Button
                type="button"
                variant="outline"
                size="icon"
                aria-label={msg("package_manager.add_source", "Add source")}
                onClick={() => setSourceDialog({})}
              >
                <Plus className="size-4" />
              </Button>
              <div className="grow" />
              <Button
                type="button"
                variant="outline"
                disabled={!effectiveSourceURL || availableLoading}
                onClick={() => loadSource(effectiveSourceURL)}
              >
                {availableLoading ? (
                  <Spinner className="size-4" />
                ) : (
                  <RefreshCw className="size-4" />
                )}
                {msg("package_manager.load_packages", "Load packages")}
              </Button>
            </div>
            {availableLoaded && (
              <>
                <div className="flex flex-wrap items-center gap-2">
                  <Input
                    className="w-56"
                    placeholder={`${msg("filter", "Filter")}...`}
                    value={availableFilter}
                    onChange={(e) => setAvailableFilter(e.currentTarget.value)}
                  />
                  <div className="grow" />
                  <Button
                    type="button"
                    disabled={busy || availableSpecs.length === 0}
                    onClick={() => {
                      void startPackageJob("install", async () => {
                        const result = await installMutation({
                          variables: { packages: availableSpecs },
                        });
                        availableSelection.clear();
                        return result.data?.installPackages;
                      });
                    }}
                  >
                    {withCount(
                      msg(
                        "package_manager.install_selected",
                        "Install selected",
                      ),
                      availableSpecs.length,
                    )}
                  </Button>
                </div>
                <PackageTable
                  rows={visibleAvailableRows}
                  loading={availableLoading && availableRows.length === 0}
                  busy={busy}
                  selected={availableSelection.selected}
                  onToggle={toggleAvailableRow}
                  onToggleAll={availableSelection.toggleAll}
                  showDescription
                  installedIds={installedIdSet}
                  emptyMessage={
                    availableFilter
                      ? msg(
                          "package_manager.no_matching",
                          "No packages match the filter.",
                        )
                      : msg(
                          "package_manager.no_packages_available",
                          "No packages available from this source.",
                        )
                  }
                />
              </>
            )}
          </>
        )}
      </section>

      <DestructiveConfirmDialog
        open={confirmingUninstall}
        onOpenChange={setConfirmingUninstall}
        title={msg("package_manager.uninstall_selected", "Uninstall selected")}
        confirmText={msg("package_manager.uninstall", "Uninstall")}
        onConfirm={() => {
          setConfirmingUninstall(false);
          void startPackageJob("uninstall", async () => {
            const result = await uninstallMutation({
              variables: { packages: installedSpecs },
            });
            installedSelection.clear();
            return result.data?.uninstallPackages;
          });
        }}
      >
        <p className="text-sm">
          {intl.formatMessage(
            {
              id: "package_manager.confirm_uninstall",
              defaultMessage:
                "Are you sure you want to uninstall {number} packages?",
            },
            { number: installedSpecs.length },
          )}
        </p>
      </DestructiveConfirmDialog>

      {deletingSource && (
        <DestructiveConfirmDialog
          open
          onOpenChange={(o) => !o && setDeletingSource(null)}
          title={msg("package_manager.delete_source", "Delete source")}
          confirmText={msg("actions.delete", "Delete")}
          onConfirm={() => {
            void saveSources(
              sources.filter((s) => s.url !== deletingSource.url),
            );
            setDeletingSource(null);
          }}
        >
          <p className="text-sm">
            {intl.formatMessage(
              {
                id: "package_manager.confirm_delete_source",
                defaultMessage:
                  "Are you sure you want to delete source {name} ({url})?",
              },
              { name: deletingSource.name, url: deletingSource.url },
            )}
          </p>
        </DestructiveConfirmDialog>
      )}

      {sourceDialog && (
        <SourceEditDialog
          existing={sourceDialog.existing}
          otherSources={sources.filter(
            (s) => s.url !== sourceDialog.existing?.url,
          )}
          onSave={onSourceSaved}
          onClose={() => setSourceDialog(null)}
        />
      )}
    </div>
  );
}
