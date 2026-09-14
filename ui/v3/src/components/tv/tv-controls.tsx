import { FormattedMessage } from "react-intl";
import { TvPlaybackMenu, type TvPlaybackMenuAction } from "./tv-playback-menu";
import {
  lazy,
  Suspense,
  useEffect,
  useLayoutEffect,
  useState,
  type RefObject,
} from "react";
import { useNavigate } from "@tanstack/react-router";
import {
  Play,
  Volume2,
  VolumeX,
  Eye,
  Scan,
  PictureInPicture,
} from "lucide-react";
import {
  useScenePlayerControls,
  useScenePlayerSourcesMenu,
  useScenePlayerValue,
} from "@/components/player/scene-player-controls";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";
import { objectTitle } from "@/core/files";
import { markerAwareSeek } from "@/core/tv/playback-policy";
import {
  createTvAction,
  railEntryId,
  type TvAction,
  type TvActionKind,
  type TvRailEntry,
} from "@/core/tv/action-config";
import type { TvRotation, TvSettings } from "@/core/tv/settings";
import type { TvFeedItem, TvFeedSnapshot } from "@/core/tv/feed-state";
import type { PlaybackRange } from "@/core/marker-range";
import { useMsg } from "@/hooks/message";
import { useCommittedRef } from "@/hooks/use-committed-ref";
import {
  tvActionIcon,
  tvActionLabels,
  tvCustomIcons,
} from "./tv-action-labels";
import { TvTimeline } from "./tv-timeline";
import { TvNavigationButton } from "./tv-navigation-button";
import { useTvInputs } from "./use-tv-inputs";
import { useTvMutations, type TvScene } from "./use-tv-mutations";
import type { TvEditTarget } from "./tv-edit-panel";

const TvHelp = lazy(() => import("./tv-help"));
const TvEditPanel = lazy(() => import("./tv-edit-panel"));
type Panel =
  | { kind: "closed" }
  | { kind: "help" }
  | {
      kind: "menu";
      action: TvPlaybackMenuAction;
    }
  | { kind: "edit"; target: TvEditTarget };

function MuteButton() {
  const msg = useMsg();
  const controls = useScenePlayerControls();
  const muted = useScenePlayerValue("muted");
  return (
    <Button
      variant="secondary"
      size="icon-lg"
      className="size-11"
      onClick={controls.toggleMuted}
      aria-label={
        muted ? msg("tv.text.unmute", "Unmute") : msg("tv.text.mute", "Mute")
      }
    >
      {muted ? <VolumeX /> : <Volume2 />}
    </Button>
  );
}

function RailEntry({
  entry,
  run,
  folder,
  setFolder,
  busy,
  fullscreen,
}: {
  entry: TvRailEntry;
  run: (action: TvAction) => void;
  folder: string | null;
  setFolder: (id: string | null) => void;
  busy: boolean;
  fullscreen: boolean;
}) {
  const msg = useMsg();
  if (entry.type === "action") {
    const Icon = tvActionIcon(entry.action);
    return (
      <Button
        variant="secondary"
        size="icon-lg"
        className="size-11 shrink-0"
        aria-label={
          entry.action.label ||
          (entry.action.kind === "fullscreen" && fullscreen
            ? msg("tv.text.exit_fullscreen", "Exit fullscreen")
            : msg(
                `tv.action.${entry.action.kind}`,
                tvActionLabels[entry.action.kind],
              ))
        }
        disabled={
          busy &&
          entry.action.kind !== "settings" &&
          entry.action.kind !== "visibility"
        }
        onClick={() => run(entry.action)}
      >
        <Icon />
      </Button>
    );
  }
  const Icon = tvCustomIcons[entry.icon];
  return (
    <DropdownMenu
      modal={false}
      open={folder === entry.id}
      onOpenChange={(open) => setFolder(open ? entry.id : null)}
    >
      <DropdownMenuTrigger
        render={
          <Button
            variant="secondary"
            size="icon-lg"
            className="size-11 shrink-0"
            aria-label={entry.label}
          />
        }
      >
        <Icon />
      </DropdownMenuTrigger>
      <DropdownMenuContent side={entry.pinned ? "top" : "left"}>
        <DropdownMenuGroup>
          {entry.actions.map((action) => {
            const Icon = tvActionIcon(action);
            return (
              <DropdownMenuItem
                key={action.id}
                disabled={busy}
                className="min-h-11"
                onClick={() => {
                  setFolder(null);
                  run(action);
                }}
              >
                <Icon />
                {action.label ||
                  (action.kind === "fullscreen" && fullscreen
                    ? msg("tv.text.exit_fullscreen", "Exit fullscreen")
                    : msg(
                        `tv.action.${action.kind}`,
                        tvActionLabels[action.kind],
                      ))}
              </DropdownMenuItem>
            );
          })}
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function SourceFeedback({ openQuality }: { openQuality: () => void }) {
  const msg = useMsg();
  const controls = useScenePlayerControls();
  const { activeSource } = useScenePlayerSourcesMenu();
  const error = useScenePlayerValue("error");
  const ready = useScenePlayerValue("ready");
  if (!activeSource || error)
    return (
      <div className="pointer-events-auto absolute left-1/2 top-1/2 w-[min(28rem,calc(100%-8rem))] -translate-x-1/2 -translate-y-1/2">
        <Alert>
          <AlertTitle>
            {error
              ? msg("tv.text.playback_unavailable", "Playback unavailable")
              : msg("tv.text.tv_quality_unavailable", "TV quality unavailable")}
          </AlertTitle>
          <AlertDescription>
            {error ??
              msg(
                "tv.text.no_compatible_stream_meets_the_saved_quality_choose_an_available",
                "No compatible stream meets the saved quality. Choose an available source for this item, or skip it.",
              )}
            <Button variant="outline" className="mt-2" onClick={openQuality}>
              <FormattedMessage
                id="tv.text.choose_quality"
                defaultMessage="Choose quality"
              />
            </Button>
            {error && (
              <Button
                variant="outline"
                className="mt-2"
                onClick={controls.retry}
              >
                <FormattedMessage
                  id="tv.text.retry_playback"
                  defaultMessage="Retry playback"
                />
              </Button>
            )}
          </AlertDescription>
        </Alert>
      </div>
    );
  return !ready ? (
    <Spinner className="pointer-events-none absolute left-1/2 top-1/2 size-10 -translate-x-1/2 -translate-y-1/2" />
  ) : null;
}

export function TvControls({
  scene,
  item,
  range,
  settings,
  surface,
  root,
  rotation,
  setRotation,
  togglePresentation,
  canFullscreen,
  fullscreen,
  exitPresentation,
  openNavigation,
  navigateItem,
  drag,
  cancelDrag,
  snapshot,
  changed,
  retry,
  reshuffle,
  completion,
  setCompletion,
  remember,
  leaving,
  onInteractionBlockedChange,
}: {
  scene: TvScene;
  item: TvFeedItem;
  range: PlaybackRange;
  settings: TvSettings;
  surface: RefObject<HTMLDivElement | null>;
  root: RefObject<HTMLDivElement | null>;
  rotation: TvRotation;
  setRotation: (rotation: TvRotation) => void;
  togglePresentation: () => true;
  canFullscreen: boolean;
  fullscreen: boolean;
  exitPresentation: () => void;
  openNavigation: () => void;
  navigateItem: (direction: -1 | 1) => void;
  drag: (offset: number) => void;
  cancelDrag: () => void;
  snapshot: TvFeedSnapshot;
  changed: (deleted?: TvFeedItem) => void;
  retry: () => void;
  reshuffle: () => void;
  completion: TvSettings["completion"];
  setCompletion: (mode: TvSettings["completion"]) => void;
  remember: (position: number) => void;
  leaving: boolean;
  onInteractionBlockedChange: (blocked: boolean) => void;
}) {
  const controls = useScenePlayerControls();
  const paused = useScenePlayerValue("paused");
  const navigate = useNavigate();
  const msg = useMsg();
  const [panel, setPanel] = useState<Panel>({ kind: "closed" });
  const [folder, setFolder] = useState<string | null>(null);
  const [visible, setVisible] = useState(settings.uiVisible);
  const [fit, setFit] = useState(settings.fit);
  const mutations = useTvMutations(changed);
  useLayoutEffect(() => {
    onInteractionBlockedChange(
      panel.kind !== "closed" || folder !== null || mutations.busy,
    );
    return () => onInteractionBlockedChange(false);
  }, [panel.kind, folder, mutations.busy, onInteractionBlockedChange]);
  const latest = useCommittedRef({ remember, item, leaving });
  useEffect(() => {
    const save = () => latest.current.remember(controls.read().position);
    const timer = window.setInterval(save, 1000);
    return () => {
      window.clearInterval(timer);
      save();
    };
  }, [controls]);
  useEffect(() => {
    if (leaving) {
      remember(controls.read().position);
      controls.pause();
    }
  }, [leaving, remember, controls]);
  useEffect(() => {
    if (root.current) root.current.dataset.tvFit = fit;
  }, [root, fit]);
  const run = (action: TvAction) => {
    const position = controls.read().position;
    switch (action.kind) {
      case "settings":
        remember(position);
        controls.pause();
        void navigate({ to: "/settings/tv" });
        return;
      case "visibility":
        setVisible((value) => !value);
        return;
      case "fit":
        setFit((value) => (value === "contain" ? "cover" : "contain"));
        return;
      case "rotation":
        setRotation(rotation === "normal" ? "clockwise" : "normal");
        return;
      case "fullscreen":
        togglePresentation();
        return;
      case "completion":
        setCompletion(
          completion === "normal"
            ? "advance"
            : completion === "advance"
              ? "loop"
              : "normal",
        );
        return;
      case "organized":
        void mutations.run(scene.id, () =>
          mutations.updateScene(scene.id, { organized: !scene.organized }),
        );
        return;
      case "quick-tag":
        void mutations.run(item.key, () =>
          mutations.quickTag(scene, item, action),
        );
        return;
      case "quick-marker":
        void mutations.run(item.key, () =>
          mutations.quickMarker(scene, position, action),
        );
        return;
      case "tags":
      case "marker":
      case "delete":
        controls.pause();
        setPanel({
          kind: "edit",
          target: { kind: action.kind, scene, item, position },
        });
        return;
      case "help":
        setPanel({ kind: "help" });
        return;
      default:
        setPanel({ kind: "menu", action: action.kind });
    }
  };
  const action = (kind: TvActionKind) => run(createTvAction(kind, kind));
  const input = useTvInputs({
    surface,
    selectionKey: item.key,
    rotation,
    blocked:
      panel.kind !== "closed" || folder !== null || mutations.busy || leaving,
    drag,
    cancelDrag,
    dispatch: (command) => {
      switch (command.type) {
        case "navigate":
          remember(controls.read().position);
          navigateItem(command.direction);
          break;
        case "seek":
          controls.seek(
            markerAwareSeek(
              controls.read().position,
              command.direction,
              scene.scene_markers,
              range,
            ),
          );
          break;
        case "action":
          if (command.action === "subtitles") controls.toggleCaptions();
          else action(command.action);
          break;
        case "exit-presentation":
          exitPresentation();
          break;
      }
    },
  });
  const canPip = useScenePlayerValue("canPip");
  const zoomed = useScenePlayerValue("zoomed");
  const entries = settings.rail.flatMap<TvRailEntry>((entry) => {
    const available = (action: TvAction) =>
      action.kind !== "fullscreen" || canFullscreen;
    if (entry.type === "action") return available(entry.action) ? [entry] : [];
    const actions = entry.actions.filter(available);
    return actions.length ? [{ ...entry, actions }] : [];
  });
  const renderEntry = (entry: TvRailEntry) => (
    <RailEntry
      key={railEntryId(entry)}
      entry={entry}
      run={run}
      folder={folder}
      setFolder={setFolder}
      busy={mutations.busy}
      fullscreen={fullscreen}
    />
  );
  return (
    <div
      className="pointer-events-none absolute inset-0 flex min-h-0 flex-col justify-end text-foreground"
      data-tv-controls
    >
      <Button
        variant="transparent"
        data-video-gesture-surface=""
        data-tv-play-surface
        aria-label={
          paused ? msg("tv.text.play", "Play") : msg("tv.text.pause", "Pause")
        }
        className="pointer-events-auto absolute inset-0 size-full touch-none rounded-none p-0 active:translate-y-0 [-webkit-touch-callout:none]"
        onClick={input.tap}
      />
      {paused && (
        <div
          className="pointer-events-none absolute left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 rounded-full bg-background/70 p-4"
          aria-hidden
        >
          <Play className="size-8" />
        </div>
      )}
      <SourceFeedback openQuality={() => action("quality")} />
      {visible && (
        <aside
          aria-label={msg("tv.text.tv_actions", "TV actions")}
          data-tv-interactive
          className={cn(
            "pointer-events-auto relative mb-3 flex min-h-0 max-h-[30%] w-11 flex-col gap-2",
            settings.leftHanded ? "ml-3 mr-auto" : "ml-auto mr-3",
          )}
        >
          <div className="flex min-h-0 flex-col gap-2 overflow-y-auto overscroll-contain">
            {entries.filter((entry) => !entry.pinned).map(renderEntry)}
          </div>
        </aside>
      )}
      <div
        className="tv-footer pointer-events-auto relative flex shrink-0 flex-col gap-2 bg-background/85 p-3"
        data-tv-dock
        data-tv-interactive
      >
        {visible && (
          <>
            <div className="flex min-w-0 items-center gap-2">
              <p className="min-w-0 flex-1 truncate text-sm">
                {objectTitle(scene)}
              </p>
              <span className="shrink-0 text-xs text-muted-foreground">
                {snapshot.selected + 1} / {snapshot.total ?? "…"} ·{" "}
                {completion === "advance"
                  ? msg("tv.text.auto_advance", "Auto-advance")
                  : completion === "loop"
                    ? msg("tv.text.loop_2", "Loop")
                    : msg("tv.text.stop_at_end", "Stop at end")}
              </span>
            </div>
            {snapshot.status === "loading" ? (
              <Spinner />
            ) : snapshot.status === "error" ||
              snapshot.status === "continue" ? (
              <Button
                className="min-h-11"
                size="sm"
                variant="link"
                onClick={retry}
              >
                {snapshot.status === "error"
                  ? msg("tv.text.retry_loading", "Retry loading")
                  : msg("tv.text.load_more", "Load more")}
              </Button>
            ) : snapshot.exhausted &&
              snapshot.selected === snapshot.items.length - 1 ? (
              <Button
                className="min-h-11"
                size="sm"
                variant="link"
                onClick={reshuffle}
              >
                <FormattedMessage
                  id="tv.text.replay_reshuffle"
                  defaultMessage="Replay / reshuffle"
                />
              </Button>
            ) : null}
            <TvTimeline scene={scene} range={range} />
          </>
        )}
        <div className="flex min-w-0 items-center gap-2">
          <TvNavigationButton
            onClick={() => {
              controls.pause();
              openNavigation();
            }}
          />
          <MuteButton />
          {visible && (
            <div
              className="flex min-w-0 flex-1 gap-2 overflow-x-auto overscroll-contain"
              data-tv-pinned-actions
            >
              {entries.filter((entry) => entry.pinned).map(renderEntry)}
            </div>
          )}
          {zoomed && (
            <Button
              variant="secondary"
              size="icon-lg"
              className="size-11"
              aria-label={msg("tv.text.reset_zoom", "Reset zoom")}
              onClick={controls.resetZoom}
            >
              <Scan />
            </Button>
          )}
          {canPip && (
            <Button
              variant="secondary"
              size="icon-lg"
              className="hidden size-11 sm:inline-flex"
              aria-label={msg(
                "tv.text.picture_in_picture",
                "Picture in picture",
              )}
              onClick={controls.togglePip}
            >
              <PictureInPicture />
            </Button>
          )}
          {!visible && (
            <>
              <div className="min-w-0 flex-1" />
              <Button
                variant="secondary"
                size="icon-lg"
                className="size-11"
                aria-label={msg("tv.text.show_tv_controls", "Show TV controls")}
                onClick={() => setVisible(true)}
              >
                <Eye />
              </Button>
            </>
          )}
        </div>
        {visible && snapshot.error && (
          <p role="alert" className="text-xs">
            {snapshot.error}
          </p>
        )}
      </div>
      <Suspense fallback={<Spinner className="absolute left-1/2 top-1/2" />}>
        {panel.kind === "help" && (
          <TvHelp close={() => setPanel({ kind: "closed" })} />
        )}
        {panel.kind === "edit" && (
          <TvEditPanel
            target={panel.target}
            mutations={mutations}
            close={() => setPanel({ kind: "closed" })}
          />
        )}
        {panel.kind === "menu" && (
          <TvPlaybackMenu
            action={panel.action}
            scene={scene}
            mutations={mutations}
            close={() => setPanel({ kind: "closed" })}
          />
        )}
      </Suspense>
    </div>
  );
}
