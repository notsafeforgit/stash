/**
 * Package manager shared by the Metadata Providers (scrapers) and Plugins
 * settings pages. Two sections:
 *
 *  - Installed: lists installed packages; "Check for updates" re-queries
 *    with each package's source counterpart so out-of-date rows can be
 *    selected and updated; uninstall removes the selection.
 *  - Available: pick one of the configured package sources, browse its
 *    index, select and install.
 *
 * Install / update / uninstall run as background jobs server-side. The
 * component watches the job-lifecycle subscription and refetches the
 * installed list when one of its own jobs finishes, so the lists stay
 * accurate without manual refreshes.
 */
import { useMemo, useRef, useState } from "react";
import { useIntl } from "react-intl";
import {
  useLazyQuery,
  useMutation,
  useQuery,
  useSubscription,
} from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import { useToast } from "src/hooks/toast";
import { Button } from "src/components/ui/button";
import { Checkbox } from "src/components/ui/checkbox";
import { Spinner } from "src/components/ui/spinner";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "src/components/ui/select";

export interface PackageSource {
  name?: string | null;
  url: string;
}

interface PackageRow {
  package_id: string;
  name: string;
  version?: string | null;
  date?: string | null;
  sourceURL: string;
  upgradeVersion?: string | null;
}

interface PackageSpec {
  id: string;
  sourceURL: string;
}

function rowKey(p: { package_id: string; sourceURL: string }) {
  return `${p.sourceURL}|${p.package_id}`;
}

// The plain Installed*Packages queries omit source_package; only the
// *Status variants select it. Narrow a row of the union accordingly.
function hasSourcePackage(
  p: object,
): p is { source_package?: GQL.PackageDataFragment | null } {
  return "source_package" in p;
}

function PackageTable({
  rows,
  loading,
  selected,
  onToggle,
  onToggleAll,
  emptyMessage,
}: {
  rows: PackageRow[];
  loading: boolean;
  selected: Set<string>;
  onToggle: (key: string) => void;
  onToggleAll: (keys: string[], check: boolean) => void;
  emptyMessage: React.ReactNode;
}) {
  const allKeys = rows.map(rowKey);
  const allChecked =
    allKeys.length > 0 && allKeys.every((k) => selected.has(k));

  if (loading) return <Spinner className="size-5" />;
  if (rows.length === 0)
    return <p className="text-sm text-muted-foreground">{emptyMessage}</p>;

  return (
    <div className="overflow-x-auto rounded-lg border">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b bg-muted/50 text-left">
            <th className="w-8 p-2">
              <Checkbox
                checked={allChecked}
                onCheckedChange={(v) => onToggleAll(allKeys, v === true)}
                aria-label="Select all"
              />
            </th>
            <th className="p-2 font-medium">Name</th>
            <th className="p-2 font-medium">Version</th>
            <th className="p-2 font-medium">Source</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => {
            const key = rowKey(row);
            return (
              <tr key={key} className="border-b last:border-b-0">
                <td className="p-2">
                  <Checkbox
                    checked={selected.has(key)}
                    onCheckedChange={() => onToggle(key)}
                    aria-label={`Select ${row.name}`}
                  />
                </td>
                <td className="p-2">
                  <div>{row.name}</div>
                  <div className="text-xs text-muted-foreground">
                    {row.package_id}
                  </div>
                </td>
                <td className="p-2 whitespace-nowrap">
                  <span>{row.version ?? "—"}</span>
                  {row.upgradeVersion && (
                    <span className="ml-1 text-primary">
                      → {row.upgradeVersion}
                    </span>
                  )}
                </td>
                <td className="max-w-48 truncate p-2 text-xs text-muted-foreground">
                  {row.sourceURL}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
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

  return { selected, setSelected, toggle, toggleAll };
}

export function PackageManager({
  type,
  sources,
}: {
  type: "scraper" | "plugin";
  sources: PackageSource[];
}) {
  const intl = useIntl();
  const Toast = useToast();

  const isPlugin = type === "plugin";

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

  // ── Installed ──────────────────────────────────────────────────────────────
  const [checkForUpgrades, setCheckForUpgrades] = useState(false);

  const installedQueryDoc = checkForUpgrades
    ? isPlugin
      ? GQL.InstalledPluginPackagesStatusDocument
      : GQL.InstalledScraperPackagesStatusDocument
    : isPlugin
      ? GQL.InstalledPluginPackagesDocument
      : GQL.InstalledScraperPackagesDocument;

  const {
    data: installedData,
    loading: installedLoading,
    refetch: refetchInstalled,
  } = useQuery(installedQueryDoc, { fetchPolicy: "cache-and-network" });

  const installedRows: PackageRow[] = useMemo(
    () =>
      (installedData?.installedPackages ?? []).map((p) => {
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
          upgradeVersion:
            sourcePackage && sourcePackage.version !== p.version
              ? sourcePackage.version
              : undefined,
        };
      }),
    [installedData],
  );

  const installedSelection = useSelection();

  // ── Available ──────────────────────────────────────────────────────────────
  const [sourceURL, setSourceURL] = useState<string>(sources[0]?.url ?? "");

  const [
    loadAvailable,
    { data: availableData, loading: availableLoading, called: availableCalled },
  ] = useLazyQuery(
    isPlugin
      ? GQL.AvailablePluginPackagesDocument
      : GQL.AvailableScraperPackagesDocument,
    { fetchPolicy: "cache-and-network" },
  );

  const availableRows: PackageRow[] = useMemo(
    () =>
      (availableData?.availablePackages ?? []).map((p) => ({
        package_id: p.package_id,
        name: p.name,
        version: p.version,
        date: p.date,
        sourceURL,
      })),
    [availableData, sourceURL],
  );

  const availableSelection = useSelection();

  // ── Job tracking ───────────────────────────────────────────────────────────
  // Install / update / uninstall return a job id. Watch lifecycle REMOVE
  // events for our jobs and refetch the installed list when they finish.
  const pendingJobsRef = useRef<Set<string>>(new Set());
  useSubscription(GQL.JobsLifecycleSubscribeDocument, {
    onData: ({ data: payload }) => {
      const event = payload.data?.jobsLifecycleSubscribe;
      if (!event || event.type !== GQL.JobStatusUpdateType.Remove) return;
      if (!pendingJobsRef.current.delete(event.job.id)) return;
      void refetchInstalled();
    },
  });

  function specsFromSelection(rows: PackageRow[], selected: Set<string>) {
    return rows
      .filter((r) => selected.has(rowKey(r)))
      .map((r): PackageSpec => ({ id: r.package_id, sourceURL: r.sourceURL }));
  }

  async function runPackageJob(
    run: () => Promise<string | null | undefined>,
    successMessage: string,
  ) {
    try {
      const jobID = await run();
      if (jobID) pendingJobsRef.current.add(jobID);
      Toast.success(successMessage);
    } catch (e) {
      Toast.error(e);
    }
  }

  const msg = (id: string, defaultMessage: string) =>
    intl.formatMessage({ id, defaultMessage });

  const installedSpecs = specsFromSelection(
    installedRows,
    installedSelection.selected,
  );
  const availableSpecs = specsFromSelection(
    availableRows,
    availableSelection.selected,
  );

  return (
    <div className="space-y-8">
      <section className="space-y-3">
        <h3 className="text-sm font-medium">
          {msg("package_manager.installed", "Installed packages")}
        </h3>
        <PackageTable
          rows={installedRows}
          loading={installedLoading && !installedData}
          selected={installedSelection.selected}
          onToggle={installedSelection.toggle}
          onToggleAll={installedSelection.toggleAll}
          emptyMessage={msg(
            "package_manager.no_packages_installed",
            "No packages installed.",
          )}
        />
        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            variant="outline"
            onClick={() => {
              setCheckForUpgrades(true);
              void refetchInstalled();
            }}
          >
            {msg("package_manager.check_for_updates", "Check for updates")}
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={installedSpecs.length === 0}
            onClick={() =>
              void runPackageJob(
                async () => {
                  const result = await updateMutation({
                    variables: { packages: installedSpecs },
                  });
                  installedSelection.setSelected(new Set());
                  return result.data?.updatePackages;
                },
                msg("package_manager.update_started", "Package update started"),
              )
            }
          >
            {msg("package_manager.update_selected", "Update selected")}
          </Button>
          <Button
            type="button"
            variant="destructive"
            disabled={installedSpecs.length === 0}
            onClick={() =>
              void runPackageJob(
                async () => {
                  const result = await uninstallMutation({
                    variables: { packages: installedSpecs },
                  });
                  installedSelection.setSelected(new Set());
                  return result.data?.uninstallPackages;
                },
                msg(
                  "package_manager.uninstall_started",
                  "Package uninstall started",
                ),
              )
            }
          >
            {msg("package_manager.uninstall_selected", "Uninstall selected")}
          </Button>
        </div>
      </section>

      <section className="space-y-3">
        <h3 className="text-sm font-medium">
          {msg("package_manager.available", "Available packages")}
        </h3>
        <div className="flex flex-wrap items-center gap-2">
          <Select
            value={sourceURL}
            onValueChange={(v) => {
              if (v !== null) setSourceURL(v);
            }}
          >
            <SelectTrigger className="w-72 max-w-full">
              <SelectValue>
                {sources.find((s) => s.url === sourceURL)?.name ?? sourceURL}
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
            disabled={!sourceURL}
            onClick={() =>
              void loadAvailable({ variables: { source: sourceURL } })
            }
          >
            {msg("package_manager.load_packages", "Load packages")}
          </Button>
        </div>
        {availableCalled && (
          <>
            <PackageTable
              rows={availableRows}
              loading={availableLoading && !availableData}
              selected={availableSelection.selected}
              onToggle={availableSelection.toggle}
              onToggleAll={availableSelection.toggleAll}
              emptyMessage={msg(
                "package_manager.no_packages_available",
                "No packages available from this source.",
              )}
            />
            <Button
              type="button"
              disabled={availableSpecs.length === 0}
              onClick={() =>
                void runPackageJob(
                  async () => {
                    const result = await installMutation({
                      variables: { packages: availableSpecs },
                    });
                    availableSelection.setSelected(new Set());
                    return result.data?.installPackages;
                  },
                  msg(
                    "package_manager.install_started",
                    "Package install started",
                  ),
                )
              }
            >
              {msg("package_manager.install_selected", "Install selected")}
            </Button>
          </>
        )}
      </section>
    </div>
  );
}
