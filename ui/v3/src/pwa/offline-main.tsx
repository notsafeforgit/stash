import { StrictMode, useState } from "react";
import { createRoot } from "react-dom/client";
import { IntlProvider, useIntl } from "react-intl";
import { ArrowLeft, Play, Trash2 } from "lucide-react";
import { ThemeProvider } from "@/components/theme-provider";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from "@/components/ui/empty";
import { Spinner } from "@/components/ui/spinner";
import { Toaster } from "@/components/ui/sonner";
import { ScenePlayer } from "@/components/player/scene-player";
import { useOfflineEntries } from "@/components/offline/use-offline-entries";
import { useOpfsBlobUrl } from "@/components/offline/use-opfs-blob";
import { useOfflineResumeWriter } from "@/components/offline/use-offline-resume-writer";
import { useDownloadCommands } from "@/components/offline/use-download-queue";
import {
  entryDisplayTitle,
  type OfflineEntry,
} from "@/components/offline/offline-db";
import { offlineEntryToSceneData } from "@/components/offline/offline-scene-adapter";
import { applicationBaseURL } from "@/core/platform-url";
import { installPagePinchZoomGuard } from "@/lib/prevent-page-pinch-zoom";
import flattenMessages from "@/utils/flatten-messages";
import messages from "@/locales/en-GB.json";
import "@/styles/globals.css";

function OfflinePlayer({ entry }: { entry: OfflineEntry }) {
  const intl = useIntl();
  const blob = useOpfsBlobUrl(entry.scene_id);
  const resume = useOfflineResumeWriter(
    entry.scene_id,
    entry.last_position_seconds,
  );
  if (blob.missing || blob.error)
    return (
      <Alert variant="destructive">
        <AlertTitle>
          {intl.formatMessage({ id: "offline.launch.file_unavailable" })}
        </AlertTitle>
        <AlertDescription>
          {blob.error ??
            intl.formatMessage({ id: "offline.launch.file_evicted" })}
        </AlertDescription>
      </Alert>
    );
  if (!blob.url) return <Spinner />;
  // Local playback must not request stale remote posters or preview assets.
  const scene = offlineEntryToSceneData(
    {
      ...entry,
      paths: { screenshot: null, preview: null, sprite: null, vtt: null },
    },
    blob.url,
  );
  return (
    <div className="relative aspect-video max-h-[70dvh] w-full">
      <ScenePlayer
        scene={scene}
        autostartEnabled={false}
        sendGetCurrentTime={resume.sendGetCurrentTime}
        fill
      />
    </div>
  );
}

function OfflineLibrary() {
  const intl = useIntl();
  const { entries, loading, error, refresh } = useOfflineEntries();
  const downloads = useDownloadCommands();
  const [selection, setSelection] = useState<string>();
  const selected = entries.find(
    (entry) => entry.scene_id === selection && entry.status === "complete",
  );
  const complete = entries.filter((entry) => entry.status === "complete");
  return (
    <main
      data-offline-launch=""
      className="viewport-controls pt-[max(1rem,var(--safe-area-top))] mx-auto flex min-h-dvh max-w-5xl flex-col gap-6 p-4 sm:p-6"
    >
      <header className="flex items-center justify-between gap-3">
        <h1 className="text-xl font-semibold">
          {intl.formatMessage({ id: "offline.launch.title" })}
        </h1>
        <Button
          variant="outline"
          render={
            <a href={applicationBaseURL().href}>
              {intl.formatMessage({ id: "offline.launch.reconnect" })}
            </a>
          }
        >
          {intl.formatMessage({ id: "offline.launch.reconnect" })}
        </Button>
      </header>
      <p className="text-sm text-muted-foreground">
        {intl.formatMessage({ id: "offline.launch.description" })}
      </p>
      {error && (
        <Alert variant="destructive">
          <AlertTitle>
            {intl.formatMessage({ id: "offline.launch.storage_error" })}
          </AlertTitle>
          <AlertDescription>
            {error.message}
            <Button variant="outline" onClick={refresh}>
              {intl.formatMessage({
                id: "actions.retry",
                defaultMessage: "Retry",
              })}
            </Button>
          </AlertDescription>
        </Alert>
      )}
      {loading && <Spinner />}
      {selected ? (
        <>
          <Button
            variant="ghost"
            className="self-start"
            onClick={() => setSelection(undefined)}
          >
            <ArrowLeft data-icon="inline-start" />
            {intl.formatMessage({ id: "actions.back", defaultMessage: "Back" })}
          </Button>
          <h2 className="text-lg font-medium">{entryDisplayTitle(selected)}</h2>
          <OfflinePlayer key={selected.scene_id} entry={selected} />
        </>
      ) : complete.length ? (
        <ul className="flex flex-col gap-2">
          {complete.map((entry) => (
            <li key={entry.scene_id} className="flex items-center gap-2">
              <Button
                variant="outline"
                className="min-h-12 min-w-0 flex-1 justify-start"
                onClick={() => setSelection(entry.scene_id)}
              >
                <Play data-icon="inline-start" />
                <span className="truncate">{entryDisplayTitle(entry)}</span>
              </Button>
              <Button
                variant="ghost"
                size="icon"
                aria-label={intl.formatMessage({
                  id: "offline.actions.remove",
                  defaultMessage: "Remove download",
                })}
                onClick={() => {
                  void downloads.remove(entry.scene_id).catch(() => {});
                }}
              >
                <Trash2 data-icon="inline-start" />
              </Button>
            </li>
          ))}
        </ul>
      ) : (
        !loading && (
          <Empty>
            <EmptyHeader>
              <EmptyTitle>
                {intl.formatMessage({ id: "offline.launch.empty" })}
              </EmptyTitle>
              <EmptyDescription>
                {intl.formatMessage({ id: "offline.launch.empty_description" })}
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        )
      )}
    </main>
  );
}

installPagePinchZoomGuard();
const root = document.getElementById("root");
if (!root) throw new Error("Missing application root");
createRoot(root).render(
  <StrictMode>
    <IntlProvider locale="en-GB" messages={flattenMessages(messages)}>
      <ThemeProvider>
        <TooltipProvider>
          <OfflineLibrary />
          <Toaster />
        </TooltipProvider>
      </ThemeProvider>
    </IntlProvider>
  </StrictMode>,
);
