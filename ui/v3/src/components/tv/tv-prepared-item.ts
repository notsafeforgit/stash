import type { ApolloClient } from "@apollo/client";
import * as GQL from "@/core/generated-graphql";
import type { TvFeedItem } from "@/core/tv/feed-state";
import type { TvSettings } from "@/core/tv/settings";
import { tvPlaybackPlan } from "@/core/tv/playback-policy";
import { PlayerTranscodeSession } from "@/components/player/player-transcode-session";
import { probeCodecsDecodableInMp4 } from "@/components/player/player-utils";
import {
  filterSources,
  selectScenePlayerSource,
} from "@/components/player/scene-player-sources";
import {
  injectStreamSession,
  scenePlayerSourceURL,
} from "@/components/player/scene-player-source-url";
import { preparePlayerSource } from "@/components/player/prepare-player-source";

/** One bounded window entry: identifiers, a lease and cancellable preparation.
 * Scene records remain in Apollo, and only the visible player records activity. */
export class TvPreparedItem {
  readonly session: PlayerTranscodeSession;
  private retainers = 0;
  private generation = 0;
  private disposed = false;
  private activated = false;
  private attempted = false;
  private request: AbortController | undefined;
  private media: { dispose: () => void } | undefined;

  constructor(
    readonly item: TvFeedItem,
    private client: ApolloClient,
    private settings: TvSettings,
    private seed: number,
  ) {
    this.session = new PlayerTranscodeSession(
      item.sceneId,
      // getRandomValues also works for Stash instances served over LAN HTTP.
      Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) =>
        byte.toString(16).padStart(2, "0"),
      ).join(""),
    );
  }

  retain() {
    this.retainers++;
    const generation = ++this.generation;
    return () => {
      this.retainers--;
      // Strict Mode and overlapping windows release/reacquire the same
      // entry in one commit. Only actual eviction closes its session.
      queueMicrotask(() => {
        if (this.retainers || this.generation !== generation) return;
        this.disposed = true;
        this.cancelPreparation();
        this.session.dispose();
      });
    };
  }

  activate() {
    this.activated = true;
    this.cancelPreparation();
  }

  cancelPreparation() {
    this.request?.abort();
    this.request = undefined;
    this.media?.dispose();
    this.media = undefined;
    this.attempted = false;
  }

  prepare() {
    if (this.disposed || !this.retainers || this.activated || this.attempted)
      return;
    this.attempted = true;
    const request = new AbortController();
    this.request = request;
    void this.load(request).catch(() => {
      // Background preparation is optional. A failed preload must neither
      // interrupt playback nor hold an encoder indefinitely after an error.
      if (!request.signal.aborted && !this.activated && !this.disposed) {
        request.abort();
        this.session.selectSource(undefined);
      }
    });
  }

  private async load(request: AbortController) {
    const { signal } = request;
    const response = await this.client.query({
      query: GQL.FindSceneDocument,
      variables: { id: this.item.sceneId },
      fetchPolicy: "cache-first",
      // A cancelled warm-up must not abort the visible player's query for
      // another marker belonging to the same scene.
      context: { queryDeduplication: false, fetchOptions: { signal } },
    });
    signal.throwIfAborted();
    const scene = response.data?.findScene;
    if (!scene || scene.id !== this.item.sceneId) return;
    const marker =
      this.item.kind === "marker"
        ? scene.scene_markers.find((marker) => marker.id === this.item.id)
        : undefined;
    if (this.item.kind === "marker" && !marker) return;
    const plan = tvPlaybackPlan(
      scene,
      this.settings,
      this.seed,
      this.item.key,
      marker,
    );
    if (plan.kind !== "ready") return;
    const file = scene.files[0];
    const [canDecode, canDecodeVideo] = await Promise.all([
      probeCodecsDecodableInMp4(file?.video_codec, file?.audio_codec),
      probeCodecsDecodableInMp4(file?.video_codec, undefined),
    ]);
    signal.throwIfAborted();
    const source = selectScenePlayerSource(
      filterSources(scene.sceneStreams, canDecode, canDecodeVideo),
      this.settings.defaultQuality,
      { width: file?.width, height: file?.height },
    );
    const url = injectStreamSession(
      scenePlayerSourceURL(source?.src, plan.range.start, plan.range, 0),
      this.session.id,
    );
    if (!url || this.activated || this.disposed) return;
    this.session.selectSource(url);
    const media = await preparePlayerSource(url, plan.range.start, signal);
    if (signal.aborted || this.activated || this.disposed) media.dispose();
    else this.media = media;
  }
}
