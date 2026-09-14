import { useLayoutEffect, useRef } from "react";
import { useApolloClient } from "@apollo/client/react";
import type { ApolloClient } from "@apollo/client";
import type { CreatePlayerResult, VideoPlayerStore } from "@videojs/react";
import { useCommittedRef } from "@/hooks/use-committed-ref";
import { useConfigurationContextOptional } from "@/hooks/config";
import { useToast } from "@/hooks/toast";
import * as GQL from "@/core/generated-graphql";
import {
  SceneActivityVisit,
  SceneActivityWriter,
  type SceneActivityScope,
} from "@/core/scene-activity";

const writers = new WeakMap<ApolloClient, SceneActivityWriter>();

export function SceneActivityEffects({
  Player,
  scope,
  duration,
  offsetStart,
  source,
  suspended,
  ready,
}: {
  Player: CreatePlayerResult<VideoPlayerStore>;
  scope: Extract<SceneActivityScope, { kind: "online-scene" }>;
  duration: number;
  offsetStart: number;
  source: string | undefined;
  suspended: boolean;
  ready: boolean;
}) {
  const client = useApolloClient();
  const store = Player.usePlayer();
  const configuration = useConfigurationContextOptional()?.configuration;
  const latest = useCommittedRef({
    duration,
    offsetStart,
    source,
    suspended,
    ready,
    ui: configuration?.ui,
  });
  const report = useToast().error;
  // Retain the visit through Strict Mode's effect replay; a real selection
  // change creates another visit even when A → B → A uses the same video.
  const visitRef = useRef<{
    sceneId: string;
    key: string;
    visit: SceneActivityVisit;
    flush: (snapshot: ReturnType<SceneActivityVisit["snapshot"]>) => void;
  } | null>(null);
  const refreshRef = useRef<() => void>(() => {});
  useLayoutEffect(() => {
    const enabled = () =>
      (
        client.readQuery({ query: GQL.ConfigurationDocument })?.configuration
          .ui ?? latest.current.ui
      )?.trackActivity !== false;
    let writer = writers.get(client);
    if (!writer) {
      writer = new SceneActivityWriter();
      writers.set(client, writer);
    }
    if (
      !visitRef.current ||
      visitRef.current.sceneId !== scope.sceneId ||
      visitRef.current.key !== scope.visitKey
    ) {
      const sceneId = scope.sceneId;
      visitRef.current = {
        sceneId,
        key: scope.visitKey,
        visit: new SceneActivityVisit(),
        flush: writer.createVisit(
          sceneId,
          async ({ resume, delta }) => {
            const result = await client.mutate({
              mutation: GQL.SceneSaveActivityDocument,
              variables: {
                id: sceneId,
                resume_time: resume,
                playDuration: delta,
              },
            });
            if (!result.data?.sceneSaveActivity)
              throw new Error("Could not save playback activity");
            client.cache.modify({
              id: client.cache.identify({ __typename: "Scene", id: sceneId }),
              fields: {
                resume_time: () => resume,
                play_duration: (value: number = 0) => value + delta,
              },
            });
          },
          async () => {
            const result = await client.mutate({
              mutation: GQL.SceneAddPlayDocument,
              variables: { id: sceneId },
            });
            const play = result.data?.sceneAddPlay;
            if (!play) throw new Error("Could not record scene play");
            client.cache.modify({
              id: client.cache.identify({ __typename: "Scene", id: sceneId }),
              fields: {
                play_count: () => play.count,
                play_history: () => play.history,
              },
            });
          },
          enabled,
          report,
        ),
      };
    }
    const { visit, flush } = visitRef.current;
    let wasPlaying = false;
    const sample = () => {
      const options = latest.current;
      if (!enabled() || options.suspended) {
        visit.suspend();
        return;
      }
      const state = store.state;
      const playing = !state.paused && !state.ended;
      visit.observe(
        {
          now: performance.now(),
          source: options.source,
          position: options.offsetStart + state.currentTime,
          duration: options.duration,
          rate: state.playbackRate,
          playing,
          seeking: state.seeking,
          ready: options.ready && !state.waiting,
          ended: state.ended,
        },
        options.ui?.minimumPlayPercent ?? 0,
      );
      if (wasPlaying && !playing) flush(visit.snapshot());
      wasPlaying = playing;
    };
    const checkpoint = () => {
      sample();
      flush(visit.snapshot());
    };
    refreshRef.current = sample;
    const unsubscribe = store.subscribe(sample);
    const timer = window.setInterval(checkpoint, 15000);
    window.addEventListener("pagehide", checkpoint);
    const visibility = () => {
      checkpoint();
      visit.suspend();
    };
    document.addEventListener("visibilitychange", visibility);
    sample();
    return () => {
      refreshRef.current = () => {};
      unsubscribe();
      window.clearInterval(timer);
      window.removeEventListener("pagehide", checkpoint);
      document.removeEventListener("visibilitychange", visibility);
      flush(visit.snapshot());
      visit.suspend();
    };
  }, [client, store, scope.sceneId, scope.visitKey, report]);
  useLayoutEffect(() => {
    refreshRef.current();
  });
  return null;
}
