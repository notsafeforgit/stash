let installed = false;

// Safari's threaded animations refresh their off-main-thread copy when an
// animation's current time is set, but not its start time. ui-sdk's drawer moves
// its slide's start time as it opens, so on iOS the sheet stayed off-screen until
// the slide ended and then jumped open. Re-setting the current time leaves the
// timing unchanged and refreshes that copy.
export function installAnimationStartTimeSync() {
  if (installed || typeof Animation === "undefined") return;
  const descriptor = Object.getOwnPropertyDescriptor(
    Animation.prototype,
    "startTime",
  );
  const setStartTime = descriptor?.set;
  if (!descriptor || !setStartTime) return;
  installed = true;
  Object.defineProperty(Animation.prototype, "startTime", {
    ...descriptor,
    set(this: Animation, value: CSSNumberish | null) {
      setStartTime.call(this, value);
      const current = this.currentTime;
      if (value !== null && current !== null && this.playbackRate !== 0) {
        this.currentTime = current;
      }
    },
  });
}
