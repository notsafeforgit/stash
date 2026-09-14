import { FormattedMessage } from "react-intl";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import {
  skipToken,
  useApolloClient,
  useFragment,
  useQuery,
} from "@apollo/client/react";
import { Link, useNavigate, useRouter } from "@tanstack/react-router";
import * as GQL from "@/core/generated-graphql";
import { TvFeedController, type TvFeedItem } from "@/core/tv/feed-state";
import { tvQueryIdentity, type TvFeedQuery } from "@/core/tv/feed-query";
import { tvPlaybackPlan, type TvPlaybackPlan } from "@/core/tv/playback-policy";
import type { TvSearch, TvSettings } from "@/core/tv/settings";
import { ScenePlayer } from "@/components/player/scene-player";
import { PreviewImage } from "@/components/shared/preview-image";
import { Button, buttonVariants } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import {
  Empty,
  EmptyHeader,
  EmptyTitle,
  EmptyDescription,
  EmptyContent,
} from "@/components/ui/empty";
import { OverlayContainerProvider } from "@/components/ui/overlay-container";
import { useMobileNavigation } from "@/components/layout/mobile-navigation";
import { useConfigurationContext } from "@/hooks/config";
import { useTvSettings } from "@/hooks/use-tv-settings";
import { useMsg } from "@/hooks/message";
import { useCommittedRef } from "@/hooks/use-committed-ref";
import { cn } from "@/lib/utils";
import { readTvSession, saveTvSession } from "./tv-session-state";
import { useTvPresentation } from "./use-tv-presentation";
import { useTvNavigation } from "./use-tv-navigation";
import { TvControls } from "./tv-controls";
import { TvNavigationButton } from "./tv-navigation-button";
import { TvRotationProvider } from "./tv-slider";

function ScenePoster({ item }: { item: TvFeedItem }) {
  const { data, complete } = useFragment({
    fragment: GQL.TvSceneSummaryFragmentDoc,
    fragmentName: "TvSceneSummary",
    from: { __typename: "Scene", id: item.id },
  });
  return complete ? (
    <PreviewImage
      preview={data.preview_image}
      src={data.paths.screenshot ?? undefined}
      alt=""
      className="size-full object-contain"
    />
  ) : null;
}
function MarkerPoster({ item }: { item: TvFeedItem }) {
  const { data, complete } = useFragment({
    fragment: GQL.TvMarkerSummaryFragmentDoc,
    fragmentName: "TvMarkerSummary",
    from: { __typename: "SceneMarker", id: item.id },
  });
  return complete ? (
    <PreviewImage
      preview={data.preview_image}
      src={data.screenshot}
      alt=""
      className="size-full object-contain"
    />
  ) : null;
}
function Poster({ item }: { item: TvFeedItem | undefined }) {
  return item ? (
    item.kind === "scene" ? (
      <ScenePoster item={item} />
    ) : (
      <MarkerPoster item={item} />
    )
  ) : null;
}

export interface TvPageProps {
  query: TvFeedQuery;
  settings: TvSettings;
  seed: number;
  search: TvSearch;
}
export function TvPage({ query, settings, seed, search }: TvPageProps) {
  const client = useApolloClient();
  const identity = tvQueryIdentity(query);
  const [owner, setOwner] = useState(() => ({
    identity,
    controller: new TvFeedController(
      client,
      query,
      readTvSession(client, identity),
    ),
  }));
  if (owner.identity !== identity)
    setOwner({
      identity,
      controller: new TvFeedController(
        client,
        query,
        readTvSession(client, identity),
      ),
    });
  const controller = owner.controller;
  const snapshot = useSyncExternalStore(
    controller.subscribe,
    controller.getSnapshot,
  );
  const active = snapshot.items[snapshot.selected];
  const strip = useRef<HTMLDivElement>(null);
  const router = useRouter();
  const navigate = useNavigate();
  const {
    root: presentationRoot,
    surface: presentationSurface,
    portals: presentationPortals,
    mode: presentationMode,
    toggle: togglePresentation,
    exit: exitPresentation,
    canFullscreen,
  } = useTvPresentation();
  const { rotation, setRotation: storeRotation } = useTvSettings();
  const setRotation = (next: typeof rotation) => {
    storeRotation(next);
    if (settings.orientation === "match") void router.invalidate();
  };
  useEffect(() => {
    if (settings.orientation !== "match") return;
    const orientation = window.matchMedia("(orientation: portrait)");
    const update = () => {
      void router.invalidate();
    };
    orientation.addEventListener("change", update);
    return () => orientation.removeEventListener("change", update);
  }, [settings.orientation, router]);
  const { configuration } = useConfigurationContext();
  const openDrawer = useMobileNavigation();
  const openNavigation = () => {
    if (presentationMode === "fullscreen")
      void exitPresentation().then(openDrawer);
    else openDrawer();
  };
  const [leaving, setLeaving] = useState(false);
  const [interactionBlocked, setInteractionBlocked] = useState(false);
  const [completion, setCompletion] = useState(settings.completion);
  const selection = useTvNavigation(
    strip,
    useCallback(
      (direction: -1 | 1) => controller.select(direction),
      [controller],
    ),
  );
  const { data, loading, error, refetch } = useQuery(
    GQL.FindSceneDocument,
    active
      ? { variables: { id: active.sceneId }, fetchPolicy: "cache-first" }
      : skipToken,
  );
  const scene =
    active && data?.findScene?.id === active.sceneId
      ? data.findScene
      : undefined;
  const [retainedScene, setRetainedScene] = useState(scene);
  if (scene && scene !== retainedScene) setRetainedScene(scene);
  const playerScene = scene ?? retainedScene;
  const currentKey = `${identity}:${active?.key ?? "pending"}`;
  const marker =
    active?.kind === "marker"
      ? scene?.scene_markers.find((marker) => marker.id === active.id)
      : undefined;
  const [resolvedPlan, setResolvedPlan] = useState<
    { key: string; plan: TvPlaybackPlan } | undefined
  >(undefined);
  if (scene && active && resolvedPlan?.key !== currentKey) {
    const plan =
      snapshot.playback?.key === active.key
        ? snapshot.playback.plan
        : active.kind === "marker" && !marker
          ? {
              kind: "invalid" as const,
              reason: "This marker is no longer available",
            }
          : tvPlaybackPlan(scene, settings, seed, active.key, marker);
    setResolvedPlan({ key: currentKey, plan });
  }
  const plan = resolvedPlan?.key === currentKey ? resolvedPlan.plan : undefined;
  const planReady = plan?.kind === "ready";
  const pending = !scene || !planReady;
  const latest = useCommittedRef({ identity, controller });
  useEffect(() => {
    controller.start();
    return () => {
      saveTvSession(client, owner.identity, controller.getSnapshot());
      controller.dispose();
    };
  }, [client, controller, owner.identity]);
  useEffect(() => {
    if (search.seed === undefined)
      void navigate({ to: "/tv", search: { ...search, seed }, replace: true });
  }, [search, seed, navigate]);
  useEffect(() => {
    if (search.item) controller.selectKey(search.item);
  }, [controller, search.item]);
  useEffect(
    () =>
      router.subscribe("onBeforeNavigate", ({ toLocation }) => {
        if (
          toLocation.pathname === router.buildLocation({ to: "/tv" }).pathname
        )
          return;
        setLeaving(true);
        selection.cancel();
        saveTvSession(
          client,
          latest.current.identity,
          latest.current.controller.getSnapshot(),
        );
        void exitPresentation();
      }),
    [router, client, selection.cancel, exitPresentation],
  );
  useLayoutEffect(() => {
    if (leaving) controller.dispose();
  }, [leaving, controller]);
  const next = snapshot.items[snapshot.selected + 1];
  const prefetchRequest = useRef<Promise<void> | null>(null);
  useEffect(() => {
    if (!next || leaving || prefetchRequest.current) return;
    // One upcoming detail only. This resolves metadata, never a media URL load.
    const request = client
      .query({
        query: GQL.FindSceneDocument,
        variables: { id: next.sceneId },
        fetchPolicy: "cache-first",
      })
      .then(
        () => {},
        () => {},
      )
      .finally(() => {
        if (prefetchRequest.current === request) prefetchRequest.current = null;
      });
    prefetchRequest.current = request;
  }, [client, next, leaving]);
  const remember = useCallback(
    (position: number) => {
      if (!active || !plan || !Number.isFinite(position)) return;
      // The outgoing effect cannot attach its old playhead to a new selection.
      if (
        controller.getSnapshot().items[controller.getSnapshot().selected]
          ?.key !== active.key
      )
        return;
      controller.remember({ key: active.key, position, plan });
    },
    [active, plan, controller],
  );
  const changed = useCallback(
    (deleted?: TvFeedItem) => controller.reconcile(deleted),
    [controller],
  );
  const reshuffle = () => {
    void navigate({
      to: "/tv",
      search: {
        ...search,
        seed: (crypto.getRandomValues(new Uint32Array(1))[0] ?? 0) % 2147483647,
        item: undefined,
      },
    });
  };
  const msg = useMsg();
  const range = planReady
    ? (plan.range ?? { start: 0, end: scene?.files[0]?.duration ?? 0 })
    : { start: 0, end: 0 };
  return (
    <div
      ref={presentationRoot}
      data-tv
      data-tv-presentation={presentationMode}
      data-tv-rotation={rotation}
      className={cn(
        "tv-viewport relative isolate size-full min-h-0 overflow-hidden bg-black",
        presentationMode !== "normal" && "fixed inset-0 z-40 h-dvh",
      )}
    >
      <div
        ref={presentationSurface}
        className="tv-surface absolute inset-0 touch-none overflow-hidden"
        data-tv-surface
      >
        <OverlayContainerProvider container={presentationPortals}>
          <TvRotationProvider value={rotation}>
            <div ref={strip} className="absolute inset-0" data-tv-strip>
              <div
                className="absolute inset-0 -translate-y-full"
                data-tv-slot="previous"
                aria-hidden
              >
                <Poster item={snapshot.items[snapshot.selected - 1]} />
              </div>
              <div className="absolute inset-0" data-tv-slot="active">
                {playerScene && (
                  <ScenePlayer
                    scene={playerScene}
                    playbackKey={currentKey}
                    suspended={pending || leaving}
                    controls="external"
                    nativeFullscreenAllowed={false}
                    presentationRotation={
                      rotation === "clockwise"
                        ? 90
                        : rotation === "counterclockwise"
                          ? -90
                          : 0
                    }
                    initiallyMuted
                    qualityPreference={settings.defaultQuality}
                    initialTimestamp={
                      planReady
                        ? snapshot.playback &&
                          snapshot.playback.key === active?.key
                          ? snapshot.playback.position
                          : plan.start
                        : undefined
                    }
                    clipRange={planReady ? plan.range : undefined}
                    autoplay={settings.autoplay}
                    autostartEnabled={
                      settings.autoplay &&
                      (configuration.interface.autostartVideo ?? true)
                    }
                    completionMode={completion}
                    onCompletionModeChange={setCompletion}
                    onNext={
                      interactionBlocked ? undefined : () => selection.move(1)
                    }
                    onPrevious={
                      interactionBlocked ? undefined : () => selection.move(-1)
                    }
                    fill
                    enablePinchZoom
                    preload="metadata"
                    onToggleFullscreenOverride={togglePresentation}
                    posterSrc={marker?.screenshot}
                    posterImage={marker?.preview_image}
                    activityScope={
                      active?.kind === "scene"
                        ? {
                            kind: "online-scene",
                            sceneId: active.sceneId,
                            visitKey: currentKey,
                          }
                        : { kind: "marker" }
                    }
                    topOverlay={
                      active && (
                        <TvControls
                          scene={playerScene}
                          item={active}
                          range={range}
                          settings={settings}
                          surface={presentationSurface}
                          root={presentationRoot}
                          rotation={rotation}
                          setRotation={setRotation}
                          togglePresentation={togglePresentation}
                          canFullscreen={canFullscreen}
                          fullscreen={presentationMode === "fullscreen"}
                          exitPresentation={exitPresentation}
                          openNavigation={openNavigation}
                          navigateItem={selection.move}
                          drag={selection.drag}
                          cancelDrag={selection.cancel}
                          snapshot={snapshot}
                          changed={changed}
                          retry={() => {
                            void controller.load();
                          }}
                          reshuffle={reshuffle}
                          completion={completion}
                          setCompletion={setCompletion}
                          remember={remember}
                          leaving={leaving}
                          onInteractionBlockedChange={setInteractionBlocked}
                        />
                      )
                    }
                  />
                )}
                {pending && active && (
                  <div className="pointer-events-none absolute inset-0">
                    <Poster item={active} />
                  </div>
                )}
              </div>
              <div
                className="absolute inset-0 translate-y-full"
                data-tv-slot="next"
                aria-hidden
              >
                <Poster item={next} />
              </div>
            </div>
            {(!active || pending) && (
              <div className="pointer-events-auto absolute inset-0 flex items-center justify-center bg-background/60 p-4">
                <div className="tv-footer absolute bottom-0 left-0 p-3">
                  <TvNavigationButton onClick={openNavigation} />
                </div>
                <Empty>
                  <EmptyHeader>
                    <EmptyTitle>
                      {snapshot.status === "loading" || loading
                        ? msg("tv.loading", "Loading TV…")
                        : plan?.kind === "invalid"
                          ? msg(
                              "tv.text.cannot_play_this_item",
                              "Cannot play this item",
                            )
                          : error || snapshot.status === "error"
                            ? msg(
                                "tv.text.could_not_load_tv",
                                "Could not load TV",
                              )
                            : active
                              ? msg(
                                  "tv.text.media_unavailable",
                                  "Media unavailable",
                                )
                              : msg(
                                  "tv.text.no_matching_items",
                                  "No matching items",
                                )}
                    </EmptyTitle>
                    <EmptyDescription>
                      {plan?.kind === "invalid"
                        ? plan.reason
                        : (error?.message ??
                          snapshot.error ??
                          msg(
                            "tv.text.choose_another_filter_retry_or_continue_to_the_next_item",
                            "Choose another filter, retry, or continue to the next item.",
                          ))}
                    </EmptyDescription>
                  </EmptyHeader>
                  <EmptyContent>
                    {snapshot.status === "loading" || loading ? (
                      <Spinner />
                    ) : (
                      <Button
                        onClick={() => {
                          if (active) void refetch();
                          else void controller.load();
                        }}
                      >
                        <FormattedMessage
                          id="tv.text.retry"
                          defaultMessage="Retry"
                        />
                      </Button>
                    )}
                    {active && (
                      <Button
                        variant="outline"
                        onClick={() => controller.select(1)}
                      >
                        <FormattedMessage
                          id="tv.text.skip_item"
                          defaultMessage="Skip item"
                        />
                      </Button>
                    )}
                    <Link
                      to="/settings/tv"
                      className={buttonVariants({ variant: "outline" })}
                    >
                      <FormattedMessage
                        id="tv.text.tv_settings"
                        defaultMessage="TV settings"
                      />
                    </Link>
                  </EmptyContent>
                </Empty>
              </div>
            )}
            <div
              ref={presentationPortals}
              className="absolute inset-0 pointer-events-none [&>*]:pointer-events-auto"
              data-tv-portals
            />
          </TvRotationProvider>
        </OverlayContainerProvider>
      </div>
    </div>
  );
}
