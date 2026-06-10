import { useEffect, useRef, useState } from "react";
import { createFileRoute } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { useQuery, useSubscription } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import { cn } from "src/lib/utils";
import { Spinner } from "src/components/ui/spinner";
import {
  SettingsSection,
  SettingSelect,
} from "src/components/settings/setting-row";

// Keep a generous in-memory buffer but only render the latest slice —
// mirrors v2.5's behaviour so a chatty Trace session doesn't lock the tab.
const MAX_LOG_ENTRIES = 50000;
const MAX_DISPLAY_LOG_ENTRIES = 1000;
const LOG_LEVELS = ["Trace", "Debug", "Info", "Warning", "Error"];

interface LogEntry {
  id: number;
  time: string;
  level: string;
  message: string;
}

function formatTime(time: string) {
  const date = new Date(time);
  if (Number.isNaN(date.getTime())) return time;
  const pad = (v: number) => String(v).padStart(2, "0");
  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}` +
    ` ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
  );
}

const LEVEL_CLASSES: Record<string, string> = {
  error: "text-destructive",
  warning: "text-amber-500",
  debug: "text-sky-500",
  trace: "text-muted-foreground",
  progress: "text-muted-foreground",
};

function SettingsLogsPage() {
  const intl = useIntl();
  const [logLevel, setLogLevel] = useState("Info");
  const [entries, setEntries] = useState<LogEntry[]>([]);
  const nextIdRef = useRef(0);

  function toEntries(raw: GQL.LogEntryDataFragment[]): LogEntry[] {
    return raw.map((e) => ({
      id: nextIdRef.current++,
      time: formatTime(e.time),
      level: e.level,
      message: e.message,
    }));
  }

  const { data: initialData, loading } = useQuery(GQL.LogsDocument, {
    fetchPolicy: "network-only",
  });

  // Seed once from the initial query; live entries arrive via the
  // subscription below and are prepended (newest first).
  const seededRef = useRef(false);
  useEffect(() => {
    if (seededRef.current || !initialData?.logs) return;
    seededRef.current = true;
    const initial = toEntries(initialData.logs);
    setEntries((prev) => [...prev, ...initial].slice(0, MAX_LOG_ENTRIES));
  });

  const { error: subscriptionError } = useSubscription(
    GQL.LoggingSubscribeDocument,
    {
      onData: ({ data }) => {
        const incoming = data.data?.loggingSubscribe;
        if (!incoming?.length) return;
        const newEntries = toEntries(incoming);
        newEntries.reverse();
        setEntries((prev) =>
          [...newEntries, ...prev].slice(0, MAX_LOG_ENTRIES),
        );
      },
    },
  );

  const levelIndex = LOG_LEVELS.indexOf(logLevel);
  const displayEntries = entries
    .filter(
      (e) => logLevel === "Trace" || LOG_LEVELS.indexOf(e.level) >= levelIndex,
    )
    .slice(0, MAX_DISPLAY_LOG_ENTRIES);

  return (
    <div className="flex h-full min-h-0 flex-col gap-4 p-6">
      <div className="max-w-3xl">
        <SettingsSection
          title={intl.formatMessage({
            id: "config.categories.logs",
            defaultMessage: "Logs",
          })}
        >
          <SettingSelect
            label={intl.formatMessage({
              id: "config.logs.log_level",
              defaultMessage: "Log level",
            })}
            value={logLevel}
            options={LOG_LEVELS.map((l) => ({ value: l, label: l }))}
            onChange={setLogLevel}
            triggerClassName="w-32"
          />
        </SettingsSection>
      </div>

      {subscriptionError && (
        <p className="text-sm text-destructive">
          Error connecting to log server: {subscriptionError.message}
        </p>
      )}

      <div className="min-h-0 flex-1 overflow-y-auto rounded-lg border bg-card p-3 font-mono text-xs leading-5">
        {loading && entries.length === 0 && <Spinner className="size-5" />}
        {displayEntries.map((entry) => (
          <div key={entry.id} className="flex gap-2 whitespace-pre-wrap">
            <span className="shrink-0 text-muted-foreground">{entry.time}</span>
            <span
              className={cn(
                "w-16 shrink-0 uppercase",
                LEVEL_CLASSES[entry.level.toLowerCase().trim()],
              )}
            >
              {entry.level}
            </span>
            <span className="break-all">{entry.message}</span>
          </div>
        ))}
        {!loading && displayEntries.length === 0 && (
          <p className="text-muted-foreground">No log entries.</p>
        )}
      </div>
    </div>
  );
}

export const Route = createFileRoute("/settings/logs")({
  component: SettingsLogsPage,
});
