import { useState } from "react";
import { createFileRoute } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { useMutation, useQuery } from "@apollo/client/react";
import { Clock, X } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { useConfigurationContext, useConfigureDLNA } from "src/hooks/config";
import { useToast } from "src/hooks/toast";
import { useMsg } from "src/hooks/message";
import { Button } from "src/components/ui/button";
import { Checkbox } from "src/components/ui/checkbox";
import { Input } from "src/components/ui/input";
import { Label } from "src/components/ui/label";
import { Spinner } from "src/components/ui/spinner";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "src/components/ui/dialog";
import {
  SettingNumber,
  SettingsSection,
  SettingSelect,
  SettingStringList,
  SettingSwitch,
  SettingText,
} from "src/components/settings/setting-row";

const DEFAULT_DLNA_PORT = 1338;

const VIDEO_SORT_OPTIONS = [
  { value: "created_at", label: "Created at" },
  { value: "date", label: "Date" },
  { value: "title", label: "Title" },
  { value: "random", label: "Random" },
  { value: "updated_at", label: "Updated at" },
];

/**
 * Shared "for how long?" dialog used by temporary enable / disable and
 * temporary IP whitelisting. Duration is in minutes, mirroring the
 * backend's expectation; "until restart" sends no duration.
 */
function TempDurationDialog({
  open,
  title,
  confirmLabel,
  onCancel,
  onConfirm,
}: {
  open: boolean;
  title: React.ReactNode;
  confirmLabel: React.ReactNode;
  onCancel: () => void;
  onConfirm: (durationMinutes: number | undefined) => void;
}) {
  const intl = useIntl();
  const [untilRestart, setUntilRestart] = useState(false);
  const [minutes, setMinutes] = useState("60");

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onCancel()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Clock className="size-4" />
            {title}
          </DialogTitle>
        </DialogHeader>
        <div className="space-y-4">
          <div className="flex items-center gap-2">
            <Checkbox
              id="dlna-until-restart"
              checked={untilRestart}
              onCheckedChange={(v) => setUntilRestart(v === true)}
            />
            <Label htmlFor="dlna-until-restart" className="font-normal">
              {intl.formatMessage({
                id: "config.dlna.until_restart",
                defaultMessage: "Until restart",
              })}
            </Label>
          </div>
          <div className="space-y-1">
            <Label htmlFor="dlna-duration">
              {intl.formatMessage({
                id: "duration",
                defaultMessage: "Duration",
              })}
            </Label>
            <Input
              id="dlna-duration"
              type="number"
              inputMode="numeric"
              className="w-28"
              value={minutes}
              disabled={untilRestart}
              onChange={(e) => setMinutes(e.currentTarget.value)}
            />
            <p className="text-xs text-muted-foreground">
              {intl.formatMessage({
                id: "config.dlna.duration_minutes",
                defaultMessage: "Duration in minutes.",
              })}
            </p>
          </div>
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onCancel}>
            {intl.formatMessage({
              id: "actions.cancel",
              defaultMessage: "Cancel",
            })}
          </Button>
          <Button
            type="button"
            onClick={() => {
              const parsed = Number(minutes);
              onConfirm(
                untilRestart || !Number.isFinite(parsed) || parsed <= 0
                  ? undefined
                  : parsed,
              );
            }}
          >
            {confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function SettingsServicesPage() {
  const intl = useIntl();
  const Toast = useToast();
  const { configuration } = useConfigurationContext();
  const dlna = configuration.dlna;
  const [configureDLNA] = useConfigureDLNA();

  const {
    data: statusData,
    loading: statusLoading,
    refetch: statusRefetch,
  } = useQuery(GQL.DlnaStatusDocument, { fetchPolicy: "cache-and-network" });

  const [enableDLNA] = useMutation(GQL.EnableDlnaDocument);
  const [disableDLNA] = useMutation(GQL.DisableDlnaDocument);
  const [addTempIP] = useMutation(GQL.AddTempDlnaipDocument);
  const [removeTempIP] = useMutation(GQL.RemoveTempDlnaipDocument);

  // undefined = dialog hidden; true = enabling, false = disabling
  const [enableDisable, setEnableDisable] = useState<boolean | undefined>();
  // undefined = dialog hidden; the IP being temporarily whitelisted
  const [tempIP, setTempIP] = useState<string | undefined>();
  const [ipEntry, setIPEntry] = useState("");

  const status = statusData?.dlnaStatus;

  function save(input: Partial<GQL.ConfigDlnaInput>) {
    void configureDLNA({ variables: { input } });
  }

  const msg = useMsg();

  function renderDeadline(until?: string | null) {
    if (!until) return "";
    return ` (until ${intl.formatDate(new Date(until), {
      dateStyle: "medium",
      timeStyle: "short",
    })})`;
  }

  async function onTempEnableConfirm(durationMinutes: number | undefined) {
    const variables = { input: { duration: durationMinutes } };
    try {
      if (enableDisable) {
        await enableDLNA({ variables });
        Toast.success(
          msg(
            "config.dlna.enabled_dlna_temporarily",
            "Enabled DLNA temporarily",
          ),
        );
      } else {
        await disableDLNA({ variables });
        Toast.success(
          msg(
            "config.dlna.disabled_dlna_temporarily",
            "Disabled DLNA temporarily",
          ),
        );
      }
    } catch (e) {
      Toast.error(e);
    } finally {
      setEnableDisable(undefined);
      void statusRefetch();
    }
  }

  async function onAllowTempIPConfirm(durationMinutes: number | undefined) {
    if (!tempIP) return;
    try {
      await addTempIP({
        variables: { input: { address: tempIP, duration: durationMinutes } },
      });
      Toast.success(
        msg("config.dlna.allowed_ip_temporarily", "Allowed IP temporarily"),
      );
    } catch (e) {
      Toast.error(e);
    } finally {
      setTempIP(undefined);
      void statusRefetch();
    }
  }

  async function onDisallowTempIP(address: string) {
    try {
      await removeTempIP({ variables: { input: { address } } });
      Toast.success(msg("config.dlna.disallowed_ip", "Disallowed IP"));
    } catch (e) {
      Toast.error(e);
    } finally {
      void statusRefetch();
    }
  }

  const canCancelTemp =
    !!status && (!!status.until || status.running !== dlna.enabled);

  async function onCancelTempBehaviour() {
    if (!status) return;
    try {
      if (status.running) {
        await disableDLNA({ variables: { input: {} } });
      } else {
        await enableDLNA({ variables: { input: {} } });
      }
      Toast.success(
        msg(
          "config.dlna.successfully_cancelled_temporary_behaviour",
          "Successfully cancelled temporary behaviour",
        ),
      );
    } catch (e) {
      Toast.error(e);
    } finally {
      void statusRefetch();
    }
  }

  return (
    <div className="max-w-3xl space-y-8 p-6">
      <SettingsSection title="DLNA">
        <div className="space-y-3">
          <div className="flex flex-wrap items-center gap-3 text-sm font-medium">
            {statusLoading && !status ? (
              <Spinner className="size-4" />
            ) : (
              <span>
                {intl.formatMessage(
                  { id: "status", defaultMessage: "Status: {statusText}" },
                  {
                    statusText:
                      (status?.running
                        ? msg("actions.running", "running")
                        : msg("actions.not_running", "not running")) +
                      renderDeadline(status?.until),
                  },
                )}
              </span>
            )}
          </div>
          <div className="flex flex-wrap items-center gap-2">
            {dlna.enabled ? (
              <Button
                type="button"
                variant="outline"
                onClick={() => setEnableDisable(false)}
              >
                {msg("actions.temp_disable", "Disable temporarily…")}
              </Button>
            ) : (
              <Button
                type="button"
                variant="outline"
                onClick={() => setEnableDisable(true)}
              >
                {msg("actions.temp_enable", "Enable temporarily…")}
              </Button>
            )}
            {canCancelTemp && (
              <Button
                type="button"
                variant="destructive"
                onClick={() => void onCancelTempBehaviour()}
              >
                {msg(
                  "config.dlna.cancel_temp_behaviour",
                  "Cancel temporary behaviour",
                )}
              </Button>
            )}
            <Button
              type="button"
              variant="ghost"
              onClick={() => void statusRefetch()}
            >
              {msg("actions.refresh", "Refresh")}
            </Button>
          </div>
        </div>

        {!!status?.allowedIPAddresses.length && (
          <div className="space-y-2">
            <h3 className="text-sm font-medium">
              {msg("config.dlna.allowed_ip_addresses", "Allowed IP addresses")}
            </h3>
            <ul className="space-y-1">
              {status.allowedIPAddresses.map((a) => (
                <li key={a.ipAddress} className="flex items-center gap-2">
                  <code className="text-sm">{a.ipAddress}</code>
                  <span className="text-xs text-muted-foreground">
                    {renderDeadline(a.until)}
                  </span>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    aria-label={msg("actions.disallow", "Disallow")}
                    onClick={() => void onDisallowTempIP(a.ipAddress)}
                  >
                    <X className="size-4" />
                  </Button>
                </li>
              ))}
            </ul>
          </div>
        )}

        <div className="space-y-2">
          <h3 className="text-sm font-medium">
            {msg("config.dlna.recent_ip_addresses", "Recent IP addresses")}
          </h3>
          <ul className="space-y-1">
            {(status?.recentIPAddresses ?? []).map((a) => (
              <li key={a} className="flex items-center gap-2">
                <code className="text-sm">{a}</code>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => setTempIP(a)}
                >
                  {msg("actions.allow_temporarily", "Allow temporarily")}…
                </Button>
              </li>
            ))}
          </ul>
          <div className="flex items-center gap-2">
            <Input
              className="w-56"
              placeholder="IP address"
              value={ipEntry}
              onChange={(e) => setIPEntry(e.currentTarget.value)}
            />
            <Button
              type="button"
              variant="outline"
              disabled={!ipEntry}
              onClick={() => setTempIP(ipEntry)}
            >
              {msg("actions.allow_temporarily", "Allow temporarily")}…
            </Button>
          </div>
        </div>
      </SettingsSection>

      <SettingsSection title={msg("settings", "Settings")}>
        <SettingText
          label={msg("config.dlna.server_display_name", "Server display name")}
          description={msg(
            "config.dlna.server_display_name_desc",
            "Display name for the DLNA server. Defaults to stash when empty.",
          )}
          value={dlna.serverName}
          onChange={(v) => save({ serverName: v })}
        />
        <SettingNumber
          label={msg("config.dlna.server_port", "Server port")}
          description={msg(
            "config.dlna.server_port_desc",
            "Port to run the DLNA server on. Requires restart.",
          )}
          value={dlna.port}
          onChange={(v) => save({ port: v || DEFAULT_DLNA_PORT })}
          min={1}
          max={65535}
          integer
        />
        <SettingSwitch
          label={msg("config.dlna.enabled_by_default", "Enabled by default")}
          checked={dlna.enabled}
          onChange={(v) => save({ enabled: v })}
        />
        <SettingStringList
          label={msg("config.dlna.network_interfaces", "Network interfaces")}
          description={msg(
            "config.dlna.network_interfaces_desc",
            "Network interfaces to run the DLNA server on. Empty for all interfaces.",
          )}
          value={dlna.interfaces}
          onChange={(v) => save({ interfaces: v })}
        />
        <SettingStringList
          label={msg(
            "config.dlna.default_ip_whitelist",
            "Default IP whitelist",
          )}
          description={msg(
            "config.dlna.default_ip_whitelist_desc",
            "IP addresses allowed to use the DLNA service by default. Use * as a wildcard.",
          )}
          value={dlna.whitelistedIPs}
          onChange={(v) => save({ whitelistedIPs: v })}
          defaultNewValue="*"
        />
        <SettingSelect
          label={msg(
            "config.dlna.video_sort_order",
            "Default video sort order",
          )}
          description={msg(
            "config.dlna.video_sort_order_desc",
            "Order to sort videos by default.",
          )}
          value={dlna.videoSortOrder}
          options={VIDEO_SORT_OPTIONS}
          onChange={(v) => save({ videoSortOrder: v })}
        />
      </SettingsSection>

      <TempDurationDialog
        open={enableDisable !== undefined}
        title={
          enableDisable
            ? msg("actions.temp_enable", "Enable temporarily")
            : msg("actions.temp_disable", "Disable temporarily")
        }
        confirmLabel={
          enableDisable
            ? msg("actions.enable", "Enable")
            : msg("actions.disable", "Disable")
        }
        onCancel={() => setEnableDisable(undefined)}
        onConfirm={(d) => void onTempEnableConfirm(d)}
      />
      <TempDurationDialog
        open={tempIP !== undefined}
        title={`${msg("actions.allow_temporarily", "Allow temporarily")}: ${tempIP ?? ""}`}
        confirmLabel={msg("actions.allow", "Allow")}
        onCancel={() => setTempIP(undefined)}
        onConfirm={(d) => void onAllowTempIPConfirm(d)}
      />
    </div>
  );
}

export const Route = createFileRoute("/settings/services")({
  component: SettingsServicesPage,
});
