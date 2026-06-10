import { useEffect, useState } from "react";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { toast } from "sonner";
import {
  Download,
  EllipsisVertical,
  RefreshCcw,
  Save,
  Trash2,
} from "lucide-react";
import { Button } from "src/components/ui/button";
import { Spinner } from "src/components/ui/spinner";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "src/components/ui/dropdown-menu";
import { ScenePlayer } from "src/components/player/scene-player";
import {
  MediaDetailLayout,
  type DetailTab,
} from "src/components/detail/media-detail-layout";
import {
  SceneDetailsTab,
  SceneFileInfoTab,
} from "src/components/detail/scene-detail-tabs";
import { useConfigurationContext } from "src/hooks/config";
import { useMediaQuery } from "src/utils/screen";
import { objectTitle } from "src/core/files";
import {
  ensureDownloadQueueInit,
  useDownloadQueue,
} from "src/components/offline/use-download-queue";
import {
  getEntry,
  subscribeToEntries,
  type OfflineEntry,
} from "src/components/offline/offline-db";
import {
  FileMissingError,
  saveToFiles,
} from "src/components/offline/save-to-files";
import { offlineEntryToSceneData } from "src/components/offline/offline-scene-adapter";
import { useOpfsBlobUrl } from "src/components/offline/use-opfs-blob";
import { useOfflineResumeWriter } from "src/components/offline/use-offline-resume-writer";

function OfflineScenePage() {
  const intl = useIntl();
  const { sceneId } = Route.useParams();
  const navigate = useNavigate();
  const queue = useDownloadQueue();
  const [entry, setEntry] = useState<OfflineEntry | null | undefined>(
    undefined,
  );

  // Only resolve the OPFS file once the entry exists and is complete.
  // Passing `null` keeps the hook idle until then so we don't fire a
  // stray `readScene` while the IDB lookup is in flight.
  const blob = useOpfsBlobUrl(
    entry && entry.status === "complete" ? entry.scene_id : null,
  );

  // Subscribe to IDB writes so a download finishing (status flips to
  // `complete`) or being cancelled / deleted from elsewhere updates
  // the page in place rather than waiting for a navigation event.
  useEffect(() => {
    let cancelled = false;
    const reload = () => {
      void (async () => {
        await ensureDownloadQueueInit();
        const row = await getEntry(sceneId);
        if (cancelled) return;
        setEntry(row ?? null);
      })();
    };
    reload();
    const unsubscribe = subscribeToEntries(reload);
    return () => {
      cancelled = true;
      unsubscribe();
    };
  }, [sceneId]);

  const goBack = () => navigate({ to: "/offline" });

  // Re-download → flip back to the list view (the entry won't be in
  // `complete` state until after the download finishes; the list
  // surfaces queue progress better than this single-scene view).
  const onRedownload = async () => {
    await queue.retry(sceneId);
    goBack();
  };

  const onDelete = async () => {
    await queue.remove(sceneId);
    goBack();
  };

  // Save the OPFS file to the user's regular device storage. Surfaces
  // a real save dialog on desktop Chrome / Edge (File System Access
  // API), or a normal download (→ share sheet on iOS) on Safari /
  // Firefox. Toast-surfaced errors; "file missing" routes to
  // re-download because the OPFS entry was likely evicted.
  const onSaveToFiles = async () => {
    if (entry?.status !== "complete") return;
    try {
      await saveToFiles(entry);
    } catch (err) {
      if (err instanceof FileMissingError) {
        toast.error(
          intl.formatMessage({ id: "offline.notifications.file_missing" }),
        );
        return;
      }
      // The tier-1 path throws AbortError when the user cancels the
      // file-picker dialog. Not worth surfacing.
      if (err instanceof DOMException && err.name === "AbortError") return;
      toast.error(
        intl.formatMessage(
          { id: "offline.notifications.save_failed" },
          { error: err instanceof Error ? err.message : String(err) },
        ),
      );
    }
  };

  if (entry === undefined) {
    return (
      <OfflineMessageLayout title={null} onBack={goBack}>
        <Spinner className="size-8" />
      </OfflineMessageLayout>
    );
  }

  if (entry === null) {
    return (
      <OfflineMessageLayout title={null} onBack={goBack}>
        <p className="text-sm text-muted-foreground">
          {intl.formatMessage({ id: "offline.player.not_downloaded" })}
        </p>
        <Button variant="outline" size="sm" onClick={goBack}>
          {intl.formatMessage({ id: "offline.actions.back_to_offline" })}
        </Button>
      </OfflineMessageLayout>
    );
  }

  if (entry.status !== "complete") {
    return (
      <IncompleteOfflineEntry
        entry={entry}
        onRedownload={onRedownload}
        onBack={goBack}
      />
    );
  }

  if (blob.missing) {
    return (
      <OfflineMessageLayout title={entry.title} onBack={goBack}>
        <p className="text-sm text-muted-foreground">
          {intl.formatMessage({ id: "offline.notifications.file_missing" })}
        </p>
        <Button variant="outline" size="sm" onClick={onRedownload}>
          {intl.formatMessage({ id: "offline.actions.retry_download" })}
        </Button>
      </OfflineMessageLayout>
    );
  }
  if (blob.error) {
    return (
      <OfflineMessageLayout title={entry.title} onBack={goBack}>
        <p className="text-sm text-destructive">
          {intl.formatMessage(
            { id: "offline.player.load_failed" },
            { error: blob.error },
          )}
        </p>
      </OfflineMessageLayout>
    );
  }

  if (!blob.url) {
    return (
      <OfflineMessageLayout title={entry.title} onBack={goBack}>
        <Spinner className="size-8" />
      </OfflineMessageLayout>
    );
  }

  return (
    <ReadyOfflineScenePage
      entry={entry}
      blobUrl={blob.url}
      onBack={goBack}
      onRedownload={onRedownload}
      onDelete={onDelete}
      onSaveToFiles={onSaveToFiles}
    />
  );
}

// ── Ready state ───────────────────────────────────────────────────────────────
// Split out so the player only mounts once entry + blob URL are both
// resolved — avoids a double-mount when state lands in two ticks.

function ReadyOfflineScenePage({
  entry,
  blobUrl,
  onBack,
  onRedownload,
  onDelete,
  onSaveToFiles,
}: {
  entry: OfflineEntry;
  blobUrl: string;
  onBack: () => void;
  onRedownload: () => void;
  onDelete: () => void;
  onSaveToFiles: () => void;
}) {
  const intl = useIntl();
  const isDesktop = useMediaQuery("(min-width: 1024px)");
  const { configuration } = useConfigurationContext();
  const autostartEnabled = configuration.interface.autostartVideo ?? true;

  // Fake `SceneData` for the streaming `<ScenePlayer>` to consume. New
  // identity on every render is fine — `ScenePlayer` keys its provider
  // on the source URL (the blob URL inside `sceneStreams`), which is
  // stable for this mount.
  const fakeScene = offlineEntryToSceneData(entry, blobUrl);

  // Resume-position writer — local-only (writes to IDB via patchEntry,
  // not the streaming SaveActivity mutation).
  const { sendGetCurrentTime } = useOfflineResumeWriter(
    entry.scene_id,
    entry.last_position_seconds,
  );

  const tabs: DetailTab[] = [
    {
      id: "details",
      label: intl.formatMessage({ id: "details", defaultMessage: "Details" }),
      shortcut: "a",
      content: <SceneDetailsTab scene={fakeScene} />,
    },
    {
      id: "fileinfo",
      label: intl.formatMessage({
        id: "file_info",
        defaultMessage: "File info",
      }),
      shortcut: "i",
      content: <SceneFileInfoTab scene={fakeScene} />,
    },
  ];

  const player = (
    <ScenePlayer
      scene={fakeScene}
      autostartEnabled={autostartEnabled}
      sendGetCurrentTime={sendGetCurrentTime}
      enablePinchZoom={isDesktop}
    />
  );

  const toolbar = (
    <div className="flex items-center gap-3 py-1.5 flex-wrap">
      <span className="text-xs text-muted-foreground">
        {intl.formatMessage(
          { id: "offline.detail.downloaded_at" },
          { date: new Date(entry.downloaded_at).toLocaleDateString() },
        )}
      </span>
      <div className="ml-auto">
        <OfflineActionsMenu
          onSaveToFiles={onSaveToFiles}
          onRedownload={onRedownload}
          onDelete={onDelete}
          serverMissing={entry.server_status === "missing"}
        />
      </div>
    </div>
  );

  return (
    <MediaDetailLayout
      title={objectTitle(fakeScene) || undefined}
      primaryContent={player}
      headerContent={toolbar}
      tabs={tabs}
      onBack={onBack}
      mobilePageScroll
    />
  );
}

// ── Offline actions menu ──────────────────────────────────────────────────────

function OfflineActionsMenu({
  onSaveToFiles,
  onRedownload,
  onDelete,
  serverMissing,
}: {
  onSaveToFiles: () => void;
  onRedownload: () => void;
  onDelete: () => void;
  serverMissing: boolean;
}) {
  const intl = useIntl();
  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger
        render={<Button variant="outline" size="sm" />}
        aria-label={intl.formatMessage({
          id: "operations",
          defaultMessage: "Operations",
        })}
      >
        <EllipsisVertical />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem onClick={onSaveToFiles}>
          <Save />
          {intl.formatMessage({ id: "offline.actions.save_to_files" })}
        </DropdownMenuItem>
        {!serverMissing && (
          <DropdownMenuItem onClick={onRedownload}>
            {/* Same icon as the card-context "Re-download" so the visual
                language matches the rest of the offline UI. */}
            <RefreshCcw />
            {intl.formatMessage({ id: "offline.actions.redownload" })}
          </DropdownMenuItem>
        )}
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onClick={onDelete}>
          <Trash2 />
          {intl.formatMessage({ id: "offline.actions.delete_from_device" })}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

// ── Helpers ──────────────────────────────────────────────────────────────────
// Lightweight "in this state we just show a message" wrapper. Re-uses
// the MediaDetailLayout shell with empty tabs so the back-button and basic
// chrome match the player view's chrome.

function OfflineMessageLayout({
  title,
  onBack,
  children,
}: {
  title: string | null;
  onBack: () => void;
  children: React.ReactNode;
}) {
  const intl = useIntl();
  return (
    <MediaDetailLayout
      title={title ?? intl.formatMessage({ id: "offline.title" })}
      primaryContent={
        <div className="flex flex-1 flex-col items-center justify-center gap-3 p-8 min-h-[40vh]">
          {children}
        </div>
      }
      tabs={[]}
      onBack={onBack}
      mobilePageScroll
    />
  );
}

function IncompleteOfflineEntry({
  entry,
  onRedownload,
  onBack,
}: {
  entry: OfflineEntry;
  onRedownload: () => void;
  onBack: () => void;
}) {
  const intl = useIntl();
  const statusMessage = (() => {
    if (entry.status === "downloading") {
      return intl.formatMessage({
        id: "offline.player.download_in_progress",
      });
    }
    if (entry.status === "queued") {
      return intl.formatMessage({ id: "offline.player.download_queued" });
    }
    if (entry.status === "error") {
      return intl.formatMessage(
        { id: "offline.player.download_failed_status" },
        {
          error:
            entry.error ??
            intl.formatMessage({ id: "offline.card.error_unknown" }),
        },
      );
    }
    return intl.formatMessage(
      { id: "offline.player.download_status_other" },
      { status: entry.status },
    );
  })();
  return (
    <OfflineMessageLayout title={entry.title} onBack={onBack}>
      <p className="text-sm text-muted-foreground">{statusMessage}</p>
      {entry.status === "error" && (
        <Button variant="outline" size="sm" onClick={onRedownload}>
          <Download className="size-4" />
          {intl.formatMessage({ id: "offline.actions.retry_download" })}
        </Button>
      )}
      <Button variant="ghost" size="sm" onClick={onBack}>
        {intl.formatMessage({ id: "offline.actions.back_to_offline" })}
      </Button>
    </OfflineMessageLayout>
  );
}

export const Route = createFileRoute("/offline/$sceneId")({
  component: OfflineScenePage,
});
