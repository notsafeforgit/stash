import type { ApolloClient } from "@apollo/client";
import * as GQL from "../generated-graphql";
import type { TvFeedQuery } from "./feed-query";
import type { TvPlaybackPlan } from "./playback-policy";

export type TvFeedItem =
  | { kind: "scene"; key: string; id: string; sceneId: string }
  | { kind: "marker"; key: string; id: string; sceneId: string };
export interface TvFeedSnapshot {
  items: readonly TvFeedItem[];
  selected: number;
  nextPage: number;
  total: number | null;
  exhausted: boolean;
  status: "ready" | "loading" | "error" | "continue";
  error?: string;
  tombstones: readonly string[];
  /** Scalar media state only; Apollo remains the entity cache. */
  playback?: { key: string; position: number; plan: TvPlaybackPlan };
}
const initial: TvFeedSnapshot = {
  items: [],
  selected: 0,
  nextPage: 1,
  total: null,
  exhausted: false,
  status: "ready",
  tombstones: [],
};

export function appendTvPage(
  state: TvFeedSnapshot,
  incoming: readonly TvFeedItem[],
  total: number,
  pageSize: number,
): TvFeedSnapshot {
  const known = new Set([
    ...state.items.map((item) => item.key),
    ...state.tombstones,
  ]);
  const items = [...state.items];
  for (const item of incoming) {
    if (!known.has(item.key)) {
      known.add(item.key);
      items.push(item);
    }
  }
  return {
    ...state,
    items,
    nextPage: state.nextPage + 1,
    total,
    status: "ready",
    error: undefined,
    exhausted: incoming.length < pageSize || state.nextPage * pageSize >= total,
  };
}

/** One active request and one generation, owned by the mounted route. The
 * constructor is pure; start/dispose are effect boundaries, including Strict Mode. */
export class TvFeedController {
  private state: TvFeedSnapshot;
  private listeners = new Set<() => void>();
  private generation = 0;
  private request: AbortController | undefined;
  private active = false;
  private waitingForNext = false;
  private requestedKey: string | undefined;
  constructor(
    private client: ApolloClient,
    readonly query: TvFeedQuery,
    snapshot?: TvFeedSnapshot,
  ) {
    this.state = snapshot ? { ...snapshot, status: "ready" } : initial;
  }
  getSnapshot = () => this.state;
  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };
  private publish(state: TvFeedSnapshot) {
    this.state = state;
    for (const listener of this.listeners) listener();
  }
  start() {
    this.active = true;
    this.prefetch();
  }
  dispose() {
    this.active = false;
    this.generation++;
    this.request?.abort();
    this.request = undefined;
  }
  select(direction: -1 | 1) {
    this.requestedKey = undefined;
    const selected = this.state.selected + direction;
    if (selected >= 0 && selected < this.state.items.length) {
      this.waitingForNext = false;
      this.publish({ ...this.state, selected, playback: undefined });
      this.prefetch();
    } else if (direction > 0 && !this.state.exhausted) {
      this.waitingForNext = true;
      void this.load();
    }
  }
  selectKey(key: string) {
    this.requestedKey = key;
    const selected = this.state.items.findIndex((item) => item.key === key);
    if (selected >= 0) {
      this.requestedKey = undefined;
      this.publish({ ...this.state, selected });
    }
    this.prefetch();
  }
  remember(playback: TvFeedSnapshot["playback"]) {
    this.state = { ...this.state, playback };
  }
  private prefetch() {
    if (
      this.state.status !== "error" &&
      this.state.status !== "continue" &&
      (this.requestedKey ||
        this.state.items.length - this.state.selected - 1 <=
          this.query.prefetch)
    )
      void this.load();
  }
  async load() {
    if (!this.active || this.request || this.state.exhausted) return;
    const generation = this.generation;
    const request = new AbortController();
    this.request = request;
    this.publish({ ...this.state, status: "loading", error: undefined });
    try {
      for (let burst = 0; burst < 3; burst++) {
        const filter = {
          ...this.query.filter,
          page: this.state.nextPage,
          per_page: this.query.pageSize,
        };
        const context = { fetchOptions: { signal: request.signal } };
        let items: TvFeedItem[];
        let total: number;
        if (this.query.mode === "scenes") {
          const response = await this.client.query({
            query: GQL.TvScenesDocument,
            variables: { filter, scene_filter_ast: this.query.ast },
            context,
            fetchPolicy: "network-only",
          });
          if (!response.data) throw new Error("No scene page was returned");
          const page = response.data.findScenes;
          items = page.scenes.map((scene) => ({
            kind: "scene",
            key: `scene:${scene.id}`,
            id: scene.id,
            sceneId: scene.id,
          }));
          total = page.count;
        } else {
          const response = await this.client.query({
            query: GQL.TvMarkersDocument,
            variables: { filter, scene_marker_filter_ast: this.query.ast },
            context,
            fetchPolicy: "network-only",
          });
          if (!response.data) throw new Error("No marker page was returned");
          const page = response.data.findSceneMarkers;
          items = page.scene_markers.map((marker) => ({
            kind: "marker",
            key: `marker:${marker.id}`,
            id: marker.id,
            sceneId: marker.scene.id,
          }));
          total = page.count;
        }
        if (!this.active || generation !== this.generation) return;
        let next = appendTvPage(this.state, items, total, this.query.pageSize);
        if (this.requestedKey) {
          const selected = next.items.findIndex(
            (item) => item.key === this.requestedKey,
          );
          if (selected >= 0) {
            next = { ...next, selected };
            this.requestedKey = undefined;
          } else if (next.exhausted) {
            next = { ...next, error: "The requested item is not in this feed" };
            this.requestedKey = undefined;
          }
        }
        if (this.waitingForNext && next.items.length > next.selected + 1) {
          this.waitingForNext = false;
          next = { ...next, selected: next.selected + 1, playback: undefined };
        }
        this.publish(next);
        if (
          next.exhausted ||
          (!this.requestedKey &&
            next.items.length - next.selected - 1 > this.query.prefetch)
        )
          return;
      }
      this.publish({ ...this.state, status: "continue" });
    } catch (error) {
      if (this.active && generation === this.generation)
        this.publish({
          ...this.state,
          status: "error",
          error:
            error instanceof Error
              ? error.message
              : "Could not load the TV feed",
        });
    } finally {
      if (this.request === request) this.request = undefined;
    }
  }
  reconcile(deleted?: TvFeedItem) {
    this.generation++;
    this.request?.abort();
    this.request = undefined;
    const removed = (item: TvFeedItem) =>
      deleted &&
      (item.key === deleted.key ||
        (deleted.kind === "scene" && item.sceneId === deleted.sceneId));
    const tombstones = [
      ...this.state.tombstones,
      ...this.state.items.filter(removed).map((item) => item.key),
    ];
    const survivors = this.state.items.filter((item) => !removed(item));
    const current = this.state.items[this.state.selected];
    const found = survivors.findIndex((item) => item.key === current?.key);
    const selected =
      found >= 0
        ? found
        : Math.max(0, Math.min(this.state.selected, survivors.length - 1));
    this.publish({
      ...this.state,
      items: survivors.slice(0, selected + 1),
      selected,
      tombstones,
      nextPage: 1,
      total: null,
      exhausted: false,
      status: "ready",
      playback: removed(
        current ?? { kind: "scene", id: "", sceneId: "", key: "" },
      )
        ? undefined
        : this.state.playback,
    });
    this.prefetch();
  }
}
