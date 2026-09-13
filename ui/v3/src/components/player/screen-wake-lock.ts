/** One visible, locally playing video owns a screen lock. Requests can finish
 * after pause/unmount; late sentinels must be released too. Browser refusal is
 * normal (power saving, permissions policy), and never interrupts playback. */
export function createScreenWakeLock() {
  let active = false;
  let disposed = false;
  let generation = 0;
  let sentinel: WakeLockSentinel | undefined;

  const release = () => {
    generation++;
    const previous = sentinel;
    sentinel = undefined;
    void previous?.release().catch(() => {});
  };
  const reconcile = () => {
    release();
    if (
      disposed ||
      !active ||
      document.visibilityState !== "visible" ||
      !navigator.wakeLock
    )
      return;
    const requestGeneration = generation;
    void navigator.wakeLock
      .request("screen")
      .then((lock) => {
        if (disposed || requestGeneration !== generation) {
          void lock.release().catch(() => {});
          return;
        }
        sentinel = lock;
        lock.addEventListener(
          "release",
          () => {
            if (sentinel === lock) sentinel = undefined;
          },
          { once: true },
        );
      })
      .catch(() => {});
  };
  document.addEventListener("visibilitychange", reconcile);
  return {
    setActive(value: boolean) {
      if (value === active) return;
      active = value;
      reconcile();
    },
    dispose() {
      disposed = true;
      document.removeEventListener("visibilitychange", reconcile);
      release();
    },
  };
}
