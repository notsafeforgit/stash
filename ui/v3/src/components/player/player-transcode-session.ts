import { getPlatformURL } from "@/core/platform-url";
import { isHlsPlaylist } from "./hls";
import { hlsStreamTypeName, streamResolution } from "./scene-player-source-url";

/** A stream owner outlives the visible player when TV retains a nearby item.
 * Construction is inert; selecting a source starts its lease. */
export class PlayerTranscodeSession {
  private source: string | undefined;
  private interval: ReturnType<typeof setInterval> | undefined;
  private connected = false;
  private disposed = false;
  private pageHidden = false;
  private usedHls = false;

  constructor(
    readonly sceneId: string,
    readonly id?: string,
  ) {}

  private url(action: "stop" | "keepalive", keep?: string, release = false) {
    const url = getPlatformURL(`scene/${this.sceneId}/streams.${action}`);
    if (this.id) url.searchParams.set("stream_session", this.id);
    const type = keep && hlsStreamTypeName(keep);
    if (type) {
      url.searchParams.set("keep_type", type);
      const resolution = streamResolution(keep);
      if (resolution) url.searchParams.set("keep_resolution", resolution);
    }
    if (release && this.id) url.searchParams.set("release", "1");
    return url.toString();
  }

  private stopStreams(keep?: string, release = false) {
    if (!this.usedHls) return;
    const url = this.url("stop", keep, release);
    try {
      if (navigator.sendBeacon(url)) return;
    } catch {
      // Fetch is also allowed to finish while the document is unloading.
    }
    void fetch(url, { method: "POST", keepalive: true }).catch(() => {});
  }

  private ping = () => {
    if (this.disposed || this.pageHidden || document.hidden || !this.source)
      return;
    void fetch(this.url("keepalive", this.source), {
      method: "POST",
      keepalive: true,
    }).catch(() => {});
  };

  private stopTimer() {
    clearInterval(this.interval);
    this.interval = undefined;
  }

  private onVisibility = () => {
    this.stopTimer();
    if (
      this.disposed ||
      this.pageHidden ||
      document.hidden ||
      !this.source ||
      !isHlsPlaylist(this.source)
    )
      return;
    this.ping();
    this.interval = setInterval(this.ping, 15000);
  };

  private onPageHide = (event: PageTransitionEvent) => {
    this.pageHidden = true;
    this.stopTimer();
    // A back/forward-cache restore may reuse this owner. Other departures
    // permanently release it so late requests cannot restart its encoder.
    this.stopStreams(undefined, !event.persisted);
  };

  private onPageShow = () => {
    this.pageHidden = false;
    this.onVisibility();
  };

  selectSource(source: string | undefined) {
    if (this.disposed || this.source === source) return;
    const previous = this.source;
    this.source = source;
    if (source && isHlsPlaylist(source)) this.usedHls = true;
    if (previous && isHlsPlaylist(previous)) {
      // Quality changes release only this owner's outgoing variant.
      if (
        !source ||
        hlsStreamTypeName(previous) !== hlsStreamTypeName(source) ||
        streamResolution(previous) !== streamResolution(source)
      )
        this.stopStreams(source);
    }
    if (!this.connected && this.usedHls) {
      this.connected = true;
      document.addEventListener("visibilitychange", this.onVisibility);
      window.addEventListener("pagehide", this.onPageHide);
      window.addEventListener("pageshow", this.onPageShow);
    }
    this.onVisibility();
  }

  dispose(keep?: string) {
    if (this.disposed) return;
    this.disposed = true;
    this.stopTimer();
    document.removeEventListener("visibilitychange", this.onVisibility);
    window.removeEventListener("pagehide", this.onPageHide);
    window.removeEventListener("pageshow", this.onPageShow);
    this.stopStreams(keep, !keep);
  }
}
