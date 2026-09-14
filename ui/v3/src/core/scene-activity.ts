/** Activity is attached to a selection visit, independently of its source or
 * playback window. Markers and local downloads never write server activity. */
export type SceneActivityScope =
  | { kind: "online-scene"; sceneId: string; visitKey: string }
  | { kind: "marker" }
  | { kind: "offline" }
  | { kind: "disabled" };

export interface ActivityObservation {
  now: number;
  source: string | undefined;
  position: number;
  duration: number;
  rate: number;
  playing: boolean;
  seeking: boolean;
  ready: boolean;
  ended: boolean;
}

export interface ActivitySnapshot {
  watched: number;
  resume: number;
  qualified: boolean;
}

export class SceneActivityVisit {
  private previous: ActivityObservation | undefined;
  private watched = 0;
  private resume = 0;
  private qualified = false;

  observe(next: ActivityObservation, minimumPercent: number): ActivitySnapshot {
    const previous = this.previous;
    // UI/store updates between media ticks do not restart the media clock.
    // Otherwise a control-hover event just before timeupdate loses watch time.
    if (
      previous &&
      previous.position === next.position &&
      previous.source === next.source &&
      previous.playing === next.playing &&
      previous.ready === next.ready &&
      previous.seeking === next.seeking &&
      previous.ended === next.ended &&
      previous.rate === next.rate
    )
      return this.snapshot();
    this.previous = next;
    if (
      previous &&
      previous.source === next.source &&
      previous.ready &&
      next.ready &&
      previous.playing &&
      !previous.seeking &&
      !next.seeking
    ) {
      const elapsed = (next.now - previous.now) / 1000;
      const progress = next.position - previous.position;
      const expected = elapsed * Math.max(previous.rate, next.rate);
      // Reject seeks and suspended/background timer gaps. Small scheduling
      // tolerance accommodates media clocks publishing at different cadences.
      if (
        elapsed > 0 &&
        elapsed <= 5 &&
        progress > 0 &&
        progress <= expected + 0.75
      ) {
        this.watched += Math.min(progress, expected);
        this.resume = Math.max(0, Math.min(next.duration, next.position));
      }
    }
    if (
      next.ready &&
      next.ended &&
      next.position >= next.duration - 0.25 &&
      this.watched > 0
    ) {
      this.resume = 0;
    }
    const fraction = Math.max(0, Math.min(100, minimumPercent)) / 100;
    if (
      this.watched > 0 &&
      next.duration > 0 &&
      this.watched >= next.duration * fraction
    ) {
      this.qualified = true;
    }
    return this.snapshot();
  }

  suspend() {
    this.previous = undefined;
  }
  snapshot(): ActivitySnapshot {
    return {
      watched: this.watched,
      resume: this.resume,
      qualified: this.qualified,
    };
  }
}

export interface ActivityWrite {
  sceneId: string;
  resume: number;
  delta: number;
}

/** The server increments duration and play count: retrying an uncertain write
 * can double count. Quarantine that delta; only later, new watch time is sent.
 * One coordinator per Apollo client serializes all surfaces and visits. */
export class SceneActivityWriter {
  private chains = new Map<string, Promise<void>>();

  createVisit(
    sceneId: string,
    save: (write: ActivityWrite) => Promise<void>,
    addPlay: () => Promise<void>,
    enabled: () => boolean,
    report: (error: unknown) => void,
  ) {
    let accounted = 0;
    let acknowledged = 0;
    let counted = false;
    let lastResume: number | undefined;
    return (snapshot: ActivitySnapshot) => {
      if (!enabled() || snapshot.watched <= 0) return;
      const previous = this.chains.get(sceneId) ?? Promise.resolve();
      const run = previous.then(async () => {
        if (!enabled()) return;
        const delta = Math.max(0, snapshot.watched - accounted);
        if (delta > 0 || lastResume !== snapshot.resume) {
          accounted = snapshot.watched;
          lastResume = snapshot.resume;
          try {
            await save({ sceneId, resume: snapshot.resume, delta });
            acknowledged += delta;
          } catch (error) {
            report(error);
          }
        }
        if (snapshot.qualified && !counted && acknowledged > 0 && enabled()) {
          counted = true;
          try {
            await addPlay();
          } catch (error) {
            report(error);
          }
        }
      });
      this.chains.set(sceneId, run);
      void run.finally(() => {
        if (this.chains.get(sceneId) === run) this.chains.delete(sceneId);
      });
      return run;
    };
  }
}
