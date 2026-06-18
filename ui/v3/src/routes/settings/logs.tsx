import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createFileRoute } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { useQuery, useSubscription } from "@apollo/client/react";
import { ArrowDownToLine } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { cn } from "src/lib/utils";
import { Button } from "src/components/ui/button";
import { Spinner } from "src/components/ui/spinner";
import { useConfigurationContext, useConfigureGeneral } from "src/hooks/config";
import {
  SettingsSection,
  SettingSelect,
} from "src/components/settings/setting-row";

// Keep a generous in-memory buffer but only render the latest slice —
// mirrors v2.5's behaviour so a chatty Trace session doesn't lock the tab.
const MAX_LOG_ENTRIES = 50000;
const MAX_DISPLAY_LOG_ENTRIES = 1000;
const LOG_BOTTOM_THRESHOLD_PX = 48;
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
  const { configuration } = useConfigurationContext();
  const [configureGeneral] = useConfigureGeneral();
  const [logLevel, setLogLevel] = useState(configuration.general.logLevel);
  const [entries, setEntries] = useState<LogEntry[]>([]);
  const [isAtBottom, setIsAtBottom] = useState(true);
  const logViewportRef = useRef<HTMLDivElement>(null);
  const nextIdRef = useRef(0);

  useEffect(() => {
    setLogLevel(configuration.general.logLevel);
  }, [configuration.general.logLevel]);

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

  function isLogViewportAtBottom() {
    const viewport = logViewportRef.current;
    if (!viewport) return true;
    return (
      viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight <=
      LOG_BOTTOM_THRESHOLD_PX
    );
  }

  function scrollLogsToBottom(behavior: ScrollBehavior = "auto") {
    const viewport = logViewportRef.current;
    if (!viewport) return;
    viewport.scrollTo({ top: viewport.scrollHeight, behavior });
  }

  // Seed once from the initial query; live entries arrive via the
  // subscription below and append to the bottom.
  const seededRef = useRef(false);
  useEffect(() => {
    if (seededRef.current || !initialData?.logs) return;
    seededRef.current = true;
    // The backend cache is newest-first; render logs like a terminal so the
    // oldest retained entry is at the top and new entries append at the bottom.
    const initial = toEntries([...initialData.logs].reverse());
    setEntries((prev) => [...initial, ...prev].slice(-MAX_LOG_ENTRIES));
  });

  const { error: subscriptionError } = useSubscription(
    GQL.LoggingSubscribeDocument,
    {
      onData: ({ data }) => {
        const incoming = data.data?.loggingSubscribe;
        if (!incoming?.length) return;
        const newEntries = toEntries(incoming);
        setEntries((prev) => [...prev, ...newEntries].slice(-MAX_LOG_ENTRIES));
      },
    },
  );

  const levelIndex = LOG_LEVELS.indexOf(logLevel);
  const displayEntries = entries
    .filter(
      (e) => logLevel === "Trace" || LOG_LEVELS.indexOf(e.level) >= levelIndex,
    )
    .slice(-MAX_DISPLAY_LOG_ENTRIES);
  const newestDisplayEntryId = displayEntries.at(-1)?.id;
  const jumpToBottomLabel = intl.formatMessage({
    id: "actions.jump_to_bottom",
    defaultMessage: "Jump to bottom",
  });

  function handleLogLevelChange(value: string) {
    setLogLevel(value);
    void configureGeneral({
      variables: { input: { logLevel: value } },
    }).catch(() => {
      setLogLevel(configuration.general.logLevel);
    });
  }

  useLayoutEffect(() => {
    if (!isAtBottom || newestDisplayEntryId === undefined) return;
    const frame = window.requestAnimationFrame(() => {
      const viewport = logViewportRef.current;
      if (!viewport) return;
      viewport.scrollTo({ top: viewport.scrollHeight });
    });
    return () => window.cancelAnimationFrame(frame);
  }, [isAtBottom, newestDisplayEntryId]);

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
            onChange={handleLogLevelChange}
            triggerClassName="w-32"
          />
        </SettingsSection>
      </div>

      {subscriptionError && (
        <p className="text-sm text-destructive">
          Error connecting to log server: {subscriptionError.message}
        </p>
      )}

      <div className="relative min-h-0 flex-1">
        <div
          ref={logViewportRef}
          data-selectable-text
          onScroll={() => setIsAtBottom(isLogViewportAtBottom())}
          className="h-full overflow-y-auto rounded-lg border bg-card p-3 font-mono text-xs leading-5"
        >
          {loading && entries.length === 0 && <Spinner className="size-5" />}
          {displayEntries.map((entry) => (
            <div
              key={entry.id}
              className="grid min-w-0 gap-x-2 gap-y-0.5 py-0.5 sm:grid-cols-[10.5rem_4rem_minmax(0,1fr)]"
            >
              <span className="text-muted-foreground">{entry.time}</span>
              <span
                className={cn(
                  "uppercase",
                  LEVEL_CLASSES[entry.level.toLowerCase().trim()],
                )}
              >
                {entry.level}
              </span>
              <span className="min-w-0 whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
                {entry.message}
              </span>
            </div>
          ))}
          {!loading && displayEntries.length === 0 && (
            <p className="text-muted-foreground">No log entries.</p>
          )}
        </div>
        {!isAtBottom && displayEntries.length > 0 && (
          <Button
            type="button"
            size="sm"
            aria-label={jumpToBottomLabel}
            className="absolute right-3 bottom-3 shadow-lg"
            onClick={() => {
              setIsAtBottom(true);
              scrollLogsToBottom("smooth");
            }}
          >
            <ArrowDownToLine />
            {jumpToBottomLabel}
          </Button>
        )}
      </div>
    </div>
  );
}

export const Route = createFileRoute("/settings/logs")({
  component: SettingsLogsPage,
});
