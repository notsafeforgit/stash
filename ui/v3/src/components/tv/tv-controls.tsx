import { FormattedMessage, useIntl } from "react-intl";
import { TvPlaybackMenu, type TvPlaybackMenuAction } from "./tv-playback-menu";
import {
  lazy,
  Suspense,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type RefObject,
} from "react";
import { useNavigate, useRouter } from "@tanstack/react-router";
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
import { TvIconButton } from "./tv-icon-button";
import { TvCounter, TvCounterBadge, type TvCounterAnchor } from "./tv-counter";
import { TvInfo } from "./tv-info";
import { useTvInputs } from "./use-tv-inputs";
import { useTvMutations, type TvScene } from "./use-tv-mutations";
import type { TvEditTarget } from "./tv-edit-panel";

const TvHelp = lazy(() => import("./tv-help"));
const TvEditPanel = lazy(() => import("./tv-edit-panel"));
type Panel =
  | { kind: "closed" }
  | { kind: "help" }
  | { kind: "counter"; anchor: TvCounterAnchor }
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
    <TvIconButton
      onClick={controls.toggleMuted}
      aria-label={
        muted ? msg("tv.text.unmute", "Unmute") : msg("tv.text.mute", "Mute")
      }
    >
      {muted ? <VolumeX /> : <Volume2 />}
    </TvIconButton>
  );
}

function RailEntry({
  entry,
  run,
  folder,
  setFolder,
  busy,
  fullscreen,
  count,
  counterEntry,
  infoVisible,
  leftHanded,
}: {
  entry: TvRailEntry;
  run: (action: TvAction, anchor?: TvCounterAnchor) => void;
  folder: string | null;
  setFolder: (id: string | null) => void;
  busy: boolean;
  fullscreen: boolean;
  count: number;
  counterEntry: string | null;
  infoVisible: boolean;
  leftHanded: boolean;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const folderButton = useRef<HTMLButtonElement>(null);
  const id = railEntryId(entry);
  const side = entry.pinned ? "top" : leftHanded ? "right" : "left";
  const countDescription = intl.formatMessage(
    {
      id: "tv.text.current_o_count",
      defaultMessage: "Current count: {count, number}",
    },
    { count },
  );
  if (entry.type === "action") {
    const Icon = tvActionIcon(entry.action);
    return (
      <TvIconButton
        className="relative"
        aria-haspopup={entry.action.kind === "counter" ? "dialog" : undefined}
        aria-expanded={
          entry.action.kind === "counter" ? counterEntry === id : undefined
        }
        aria-description={
          entry.action.kind === "counter" ? countDescription : undefined
        }
        aria-pressed={entry.action.kind === "info" ? infoVisible : undefined}
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
          entry.action.kind !== "visibility" &&
          entry.action.kind !== "counter" &&
          entry.action.kind !== "info"
        }
        onClick={(event) =>
          run(entry.action, { element: event.currentTarget, entryId: id, side })
        }
      >
        <Icon />
        {entry.action.kind === "counter" && (
          <TvCounterBadge count={count} overlay />
        )}
      </TvIconButton>
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
        render={<TvIconButton ref={folderButton} aria-label={entry.label} />}
      >
        <Icon />
      </DropdownMenuTrigger>
      <DropdownMenuContent
        side={side}
        finalFocus={counterEntry === id ? false : undefined}
      >
        <DropdownMenuGroup>
          {entry.actions.map((action) => {
            const Icon = tvActionIcon(action);
            return (
              <DropdownMenuItem
                key={action.id}
                disabled={busy}
                className="min-h-11"
                aria-description={
                  action.kind === "counter" ? countDescription : undefined
                }
                onClick={() => {
                  setFolder(null);
                  run(
                    action,
                    folderButton.current
                      ? { element: folderButton.current, entryId: id, side }
                      : undefined,
                  );
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
                {action.kind === "counter" && <TvCounterBadge count={count} />}
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
  const router = useRouter();
  const msg = useMsg();
  const [panel, setPanel] = useState<Panel>({ kind: "closed" });
  const [folder, setFolder] = useState<string | null>(null);
  const [visible, setVisible] = useState(settings.uiVisible);
  const [infoVisible, setInfoVisible] = useState(false);
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
    const save = () => {
      const state = controls.read();
      if (state.ready && !latest.current.leaving)
        latest.current.remember(state.position);
    };
    const timer = window.setInterval(save, 1000);
    // Capture before navigation suspends the media and resets its playhead.
    const unsubscribe = router.subscribe("onBeforeNavigate", save);
    return () => {
      window.clearInterval(timer);
      unsubscribe();
      save();
    };
  }, [controls, router]);
  useEffect(() => {
    if (leaving) controls.pause();
  }, [leaving, controls]);
  useEffect(() => {
    if (root.current) root.current.dataset.tvFit = fit;
  }, [root, fit]);
  const run = (action: TvAction, anchor?: TvCounterAnchor) => {
    const position = controls.read().position;
    switch (action.kind) {
      case "info":
        setInfoVisible((value) => !value);
        return;
      case "counter":
        if (anchor) setPanel({ kind: "counter", anchor });
        return;
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
      count={scene.o_counter ?? 0}
      counterEntry={panel.kind === "counter" ? panel.anchor.entryId : null}
      infoVisible={infoVisible}
      leftHanded={settings.leftHanded}
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
          className="pointer-events-none absolute left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 text-white/80 drop-shadow-[0_1px_2px_rgb(0_0_0/0.85)]"
          aria-hidden
        >
          <Play className="size-12 fill-current" />
        </div>
      )}
      <SourceFeedback openQuality={() => action("quality")} />
      {visible && (
        <div
          className={cn(
            "pointer-events-none relative mb-2 flex min-h-0 flex-1 items-end gap-3 px-3",
            settings.leftHanded && "flex-row-reverse",
          )}
        >
          {infoVisible && <TvInfo key={item.key} scene={scene} />}
          <aside
            aria-label={msg("tv.text.tv_actions", "TV actions")}
            data-tv-interactive
            className={cn(
              "pointer-events-none relative flex min-h-0 max-h-[35%] w-11 shrink-0 flex-col gap-2",
              settings.leftHanded ? "mr-auto" : "ml-auto",
            )}
          >
            <div className="pointer-events-auto flex min-h-0 flex-col gap-2 overflow-y-auto overscroll-contain">
              {entries.filter((entry) => !entry.pinned).map(renderEntry)}
            </div>
          </aside>
        </div>
      )}
      <div
        className="tv-footer pointer-events-none relative flex shrink-0 flex-col gap-1 px-2 pt-1 text-white [text-shadow:0_1px_2px_rgb(0_0_0/0.85)]"
        data-tv-dock
        data-tv-interactive
      >
        {visible && (
          <>
            <div className="flex min-w-0 items-center gap-2">
              <p
                aria-hidden={infoVisible}
                className={cn(
                  "min-w-0 flex-1 truncate text-sm",
                  infoVisible && "invisible",
                )}
              >
                {objectTitle(scene)}
              </p>
              {/* Background feed refreshes must not move the rail or any
                  popover anchored to it. Reserve space within this row. */}
              <span className="inline-flex size-4 shrink-0 items-center justify-center">
                {snapshot.status === "loading" && <Spinner />}
              </span>
              <span className="shrink-0 text-xs text-white/80">
                {snapshot.selected + 1} / {snapshot.total ?? "…"} ·{" "}
                {completion === "advance"
                  ? msg("tv.text.auto_advance", "Auto-advance")
                  : completion === "loop"
                    ? msg("tv.text.loop_2", "Loop")
                    : msg("tv.text.stop_at_end", "Stop at end")}
              </span>
            </div>
            {snapshot.status === "error" || snapshot.status === "continue" ? (
              <Button
                className="pointer-events-auto min-h-11 self-start"
                size="sm"
                variant="transparent"
                onClick={retry}
              >
                {snapshot.status === "error"
                  ? msg("tv.text.retry_loading", "Retry loading")
                  : msg("tv.text.load_more", "Load more")}
              </Button>
            ) : snapshot.exhausted &&
              snapshot.selected === snapshot.items.length - 1 ? (
              <Button
                className="pointer-events-auto min-h-11 self-start"
                size="sm"
                variant="transparent"
                onClick={reshuffle}
              >
                <FormattedMessage
                  id="tv.text.replay_reshuffle"
                  defaultMessage="Replay / reshuffle"
                />
              </Button>
            ) : null}
            <TvTimeline key={item.key} scene={scene} range={range} />
          </>
        )}
        <div className="flex min-w-0 items-center gap-1">
          <TvNavigationButton
            onClick={() => {
              controls.pause();
              openNavigation();
            }}
          />
          <MuteButton />
          {visible && (
            <div
              className="pointer-events-auto flex min-w-0 flex-1 gap-1 overflow-x-auto overscroll-contain"
              data-tv-pinned-actions
            >
              {entries.filter((entry) => entry.pinned).map(renderEntry)}
            </div>
          )}
          {zoomed && (
            <TvIconButton
              aria-label={msg("tv.text.reset_zoom", "Reset zoom")}
              onClick={controls.resetZoom}
            >
              <Scan />
            </TvIconButton>
          )}
          {visible && canPip && (
            <TvIconButton
              className="hidden sm:inline-flex"
              aria-label={msg(
                "tv.text.picture_in_picture",
                "Picture in picture",
              )}
              onClick={controls.togglePip}
            >
              <PictureInPicture />
            </TvIconButton>
          )}
          {!visible && (
            <>
              <div className="min-w-0 flex-1" />
              <TvIconButton
                aria-label={msg("tv.text.show_tv_controls", "Show TV controls")}
                onClick={() => setVisible(true)}
              >
                <Eye />
              </TvIconButton>
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
        {panel.kind === "counter" && (
          <TvCounter
            scene={scene}
            anchor={panel.anchor}
            mutations={mutations}
            close={() => setPanel({ kind: "closed" })}
          />
        )}
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
