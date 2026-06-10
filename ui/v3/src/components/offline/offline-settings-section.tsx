/**
 * Offline-feature settings section, mounted on the Settings page.
 *
 * Three controls:
 *   - Maximum download resolution (per-device, localStorage)
 *   - Storage usage display + "Clear all offline scenes" button
 *   - Persistent storage status + request affordance
 */

import { useCallback, useEffect, useState } from "react";
import { useIntl } from "react-intl";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "src/components/ui/select";
import { Button } from "src/components/ui/button";
import type { StreamingResolutionEnum } from "src/core/generated-graphql";
import {
  loadOfflineMaxResolution,
  saveOfflineMaxResolution,
  OFFLINE_RESOLUTION_OPTIONS,
} from "./offline-settings";
import {
  clearAllScenes,
  isPersisted,
  requestPersistent,
  storageEstimate,
} from "./opfs-storage";
import { clearAll as clearAllDb } from "./offline-db";

export function OfflineSettingsSection() {
  const intl = useIntl();
  const [maxRes, setMaxRes] = useState<StreamingResolutionEnum>(
    loadOfflineMaxResolution(),
  );
  const [estimate, setEstimate] = useState<{
    usage?: number;
    quota?: number;
  }>({});
  const [persisted, setPersisted] = useState<boolean>(false);
  const [busy, setBusy] = useState(false);

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

  const onResolutionChange = (value: StreamingResolutionEnum | null) => {
    if (!value) return;
    setMaxRes(value);
    saveOfflineMaxResolution(value);
  };

  // Base UI's Select.Value defaults to rendering `String(value)` (the raw
  // enum) when closed; build a value→localised-label map so the trigger
  // and the items use the same string.
  const resolutionLabels = Object.fromEntries(
    OFFLINE_RESOLUTION_OPTIONS.map((opt) => [
      opt.value,
      intl.formatMessage({ id: opt.intl_id }),
    ]),
  ) as Record<StreamingResolutionEnum, string>;

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
      await Promise.all([clearAllScenes(), clearAllDb()]);
    } finally {
      setBusy(false);
      refreshStorageInfo();
    }
  };

  return (
    <section className="space-y-4">
      <h2 className="text-base font-medium">
        {intl.formatMessage({ id: "offline.settings.heading" })}
      </h2>

      <div className="flex items-center justify-between gap-4">
        <div>
          <p className="text-sm font-medium">
            {intl.formatMessage({ id: "offline.settings.max_resolution" })}
          </p>
          <p className="text-sm text-muted-foreground">
            {intl.formatMessage({
              id: "offline.settings.max_resolution_description",
            })}
          </p>
        </div>
        <Select value={maxRes} onValueChange={onResolutionChange}>
          <SelectTrigger className="w-40">
            <SelectValue>{resolutionLabels[maxRes]}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            {OFFLINE_RESOLUTION_OPTIONS.map((opt) => (
              <SelectItem key={opt.value} value={opt.value}>
                {resolutionLabels[opt.value]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="flex items-center justify-between gap-4">
        <div>
          <p className="text-sm font-medium">
            {intl.formatMessage({ id: "offline.settings.storage_usage" })}
          </p>
          <p className="text-sm text-muted-foreground">
            {estimate.usage != null && estimate.quota != null
              ? intl.formatMessage(
                  { id: "offline.header.storage_used" },
                  {
                    usage: formatBytes(estimate.usage),
                    quota: formatBytes(estimate.quota),
                  },
                )
              : intl.formatMessage({
                  id: "offline.settings.storage_estimate_unavailable",
                })}
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          disabled={busy}
          onClick={onClearAll}
        >
          {intl.formatMessage({ id: "offline.actions.clear_all" })}
        </Button>
      </div>

      <div className="flex items-center justify-between gap-4">
        <div>
          <p className="text-sm font-medium">
            {intl.formatMessage({ id: "offline.settings.persistent_storage" })}
          </p>
          <p className="text-sm text-muted-foreground">
            {intl.formatMessage({
              id: persisted
                ? "offline.settings.persistent_granted"
                : "offline.settings.persistent_not_granted",
            })}
          </p>
        </div>
        {!persisted && (
          <Button
            variant="outline"
            size="sm"
            disabled={busy}
            onClick={onRequestPersistent}
          >
            {intl.formatMessage({ id: "offline.actions.request" })}
          </Button>
        )}
      </div>
    </section>
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
