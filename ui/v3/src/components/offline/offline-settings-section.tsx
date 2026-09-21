import { canDownloadScenes, useDownloadCommands } from "./use-download-queue";
import { registerOfflineWorker } from "@/pwa/register";
import { OfflineRecoveryControl } from "./offline-recovery-control";
/**
 * Per-device controls shared by the dedicated Offline settings area.
 *
 * Three controls:
 *   - Maximum download resolution (per-device, localStorage)
 *   - Storage usage display + "Clear all offline scenes" button
 *   - Persistent storage status + request affordance
 */

import { useCallback, useEffect, useState } from "react";
import { useIntl } from "react-intl";
import { Button } from "src/components/ui/button";
import {
  SettingsSection,
  SettingSelect,
  SettingDisplay,
} from "@/components/settings/setting-row";
import { useMsg } from "@/hooks/message";
import type { StreamingResolutionEnum } from "src/core/generated-graphql";
import {
  loadOfflineMaxResolution,
  saveOfflineMaxResolution,
  OFFLINE_RESOLUTION_OPTIONS,
} from "./offline-settings";
import {
  canRequestPersistence,
  isPersisted,
  requestPersistent,
  storageEstimate,
} from "./opfs-storage";

export function OfflineSettingsPage() {
  return (
    <div className="max-w-2xl p-6">
      <OfflineSettingsSection />
    </div>
  );
}

export function OfflineSettingsSection() {
  const intl = useIntl();
  const msg = useMsg();
  const downloads = useDownloadCommands();
  const [maxRes, setMaxRes] = useState<StreamingResolutionEnum>(
    loadOfflineMaxResolution,
  );
  const [estimate, setEstimate] = useState<{
    usage?: number;
    quota?: number;
  }>({});
  const [persisted, setPersisted] = useState<boolean>(false);
  const [backgroundAvailable, setBackgroundAvailable] = useState<
    boolean | null
  >(null);
  const [busy, setBusy] = useState(false);
  const persistenceSupported = canRequestPersistence();

  // Refresh storage figures on mount and after each operation completes
  // (operations change usage / persistence state).
  const refreshStorageInfo = useCallback(() => {
    void storageEstimate().then((e) =>
      setEstimate({ usage: e.usage, quota: e.quota }),
    );
    void isPersisted().then(setPersisted);
  }, []);

  useEffect(() => {
    refreshStorageInfo();
  }, [refreshStorageInfo]);

  useEffect(() => {
    let cancelled = false;
    void registerOfflineWorker().then((registration) => {
      if (!cancelled)
        setBackgroundAvailable(
          typeof registration?.backgroundFetch?.fetch === "function",
        );
    });
    return () => {
      cancelled = true;
    };
  }, []);

  const resolutionOptions = OFFLINE_RESOLUTION_OPTIONS.map((option) => ({
    value: option.value,
    label: intl.formatMessage({ id: option.intl_id }),
  }));

  const onRequestPersistent = async () => {
    setBusy(true);
    try {
      const granted = await requestPersistent();
      setPersisted(granted);
    } finally {
      setBusy(false);
      refreshStorageInfo();
    }
  };

  const onClearAll = async () => {
    if (
      !window.confirm(
        intl.formatMessage({ id: "offline.settings.clear_all_confirm" }),
      )
    ) {
      return;
    }
    setBusy(true);
    try {
      await downloads.removeAll();
    } catch {
      // The shared command reports failure; consume this event handler's promise too.
    } finally {
      setBusy(false);
      refreshStorageInfo();
    }
  };

  return (
    <SettingsSection
      title={msg("offline.settings.heading", "Offline downloads")}
    >
      <p className="text-sm text-muted-foreground">
        {intl.formatMessage({
          id: !canDownloadScenes()
            ? "offline.settings.downloads_unavailable"
            : backgroundAvailable === null
              ? "offline.background.unavailable"
              : backgroundAvailable
                ? "offline.background.ready"
                : "offline.background.foreground",
        })}
      </p>
      <OfflineRecoveryControl />

      <SettingSelect
        label={msg(
          "offline.settings.max_resolution",
          "Maximum download resolution",
        )}
        description={msg(
          "offline.settings.max_resolution_description",
          "Cap on the resolution downloaded for offline playback. Source files shorter than the cap are downloaded at original.",
        )}
        value={maxRes}
        options={resolutionOptions}
        onChange={(value) => {
          const option = resolutionOptions.find(
            (candidate) => candidate.value === value,
          );
          if (!option) return;
          setMaxRes(option.value);
          saveOfflineMaxResolution(option.value);
        }}
        triggerClassName="w-40"
      />

      <SettingDisplay
        label={msg("offline.settings.storage_usage", "Storage usage")}
        description={
          estimate.usage != null && estimate.quota != null
            ? intl.formatMessage(
                { id: "offline.header.storage_used" },
                {
                  usage: formatBytes(estimate.usage),
                  quota: formatBytes(estimate.quota),
                },
              )
            : intl.formatMessage({
                id: "offline.settings.storage_estimate_unavailable",
              })
        }
        actions={
          <Button
            variant="outline"
            size="sm"
            disabled={busy}
            onClick={onClearAll}
          >
            {intl.formatMessage({ id: "offline.actions.clear_all" })}
          </Button>
        }
      />

      <SettingDisplay
        label={msg("offline.settings.persistent_storage", "Persistent storage")}
        description={intl.formatMessage({
          id: persisted
            ? "offline.settings.persistent_granted"
            : persistenceSupported
              ? "offline.settings.persistent_not_granted"
              : "offline.settings.persistent_unavailable",
        })}
        actions={
          !persisted &&
          persistenceSupported && (
            <Button
              variant="outline"
              size="sm"
              disabled={busy}
              onClick={onRequestPersistent}
            >
              {intl.formatMessage({ id: "offline.actions.request" })}
            </Button>
          )
        }
      />
    </SettingsSection>
  );
}

function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let n = bytes;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  return `${n < 10 && i > 0 ? n.toFixed(1) : Math.round(n)} ${units[i]}`;
}
