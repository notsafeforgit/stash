type PaintKeyframe = Partial<Pick<Keyframe, "opacity" | "scale" | "offset">>;

/** One interruptible element animation. Entrance effects hold their first frame
 * through a paint so WebKit startup work cannot consume the animation unseen. */
export function createPaintAnimation() {
  let animation: Animation | undefined;
  let frame: number | undefined;
  let restoreStyle: (() => void) | undefined;
  let restoreLayer: (() => void) | undefined;
  let removeListeners: (() => void) | undefined;
  const cancel = () => {
    if (frame !== undefined) cancelAnimationFrame(frame);
    frame = undefined;
    animation?.cancel();
    animation = undefined;
    restoreStyle?.();
    restoreStyle = undefined;
    restoreLayer?.();
    restoreLayer = undefined;
    removeListeners?.();
    removeListeners = undefined;
  };
  return {
    cancel,
    play(
      element: HTMLElement | null,
      keyframes: PaintKeyframe[],
      options: KeyframeAnimationOptions & {
        afterPaint?: boolean;
        hold?: boolean;
      },
    ) {
      cancel();
      if (!element || document.hidden) return;
      const preference = matchMedia("(prefers-reduced-motion: reduce)");
      if (preference.matches) return;
      const { afterPaint, hold, ...timing } = options;
      preference.addEventListener("change", cancel);
      document.addEventListener("visibilitychange", cancel);
      removeListeners = () => {
        preference.removeEventListener("change", cancel);
        document.removeEventListener("visibilitychange", cancel);
      };
      const prepare = () => {
        frame = undefined;
        if (
          !element?.animate ||
          !element.isConnected ||
          document.hidden ||
          preference.matches
        ) {
          cancel();
          return;
        }
        const run = () => {
          frame = undefined;
          if (!element.isConnected || document.hidden || preference.matches) {
            cancel();
            return;
          }
          const running = element.animate(keyframes, {
            ...timing,
            fill: "both",
          });
          animation = running;
          restoreStyle?.();
          restoreStyle = undefined;
          const finish = () => {
            if (animation === running) cancel();
          };
          void running.finished.then(() => {
            if (!hold) finish();
          }, finish);
        };
        if (afterPaint) {
          // Prepare the compositing layer before its animation clock starts.
          const previous = (["opacity", "scale"] as const).flatMap(
            (property) => {
              const value = keyframes[0]?.[property];
              if (value === undefined || value === null) return [];
              const saved = {
                property,
                value: element.style.getPropertyValue(property),
                priority: element.style.getPropertyPriority(property),
              };
              element.style.setProperty(property, String(value));
              return [saved];
            },
          );
          restoreStyle = () => {
            for (const saved of previous) {
              if (saved.value)
                element.style.setProperty(
                  saved.property,
                  saved.value,
                  saved.priority,
                );
              else element.style.removeProperty(saved.property);
            }
          };
          const layer = element.style.getPropertyValue("will-change");
          const priority = element.style.getPropertyPriority("will-change");
          element.style.setProperty(
            "will-change",
            previous
              .map(({ property }) =>
                property === "scale" ? "transform" : property,
              )
              .join(", "),
          );
          restoreLayer = () => {
            if (layer)
              element.style.setProperty("will-change", layer, priority);
            else element.style.removeProperty("will-change");
          };
          frame = requestAnimationFrame(() => {
            frame = requestAnimationFrame(run);
          });
        } else run();
      };
      prepare();
    },
  };
}
