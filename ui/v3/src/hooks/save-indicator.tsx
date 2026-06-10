import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type PropsWithChildren,
} from "react";
import { Check, X } from "lucide-react";
import { Spinner } from "src/components/ui/spinner";
import { cn } from "src/lib/utils";

type SaveState = "idle" | "saving" | "success" | "error";

interface ISaveIndicator {
  state: SaveState;
  /**
   * Register a promise representing an in-flight save. The promise is
   * passed through unchanged; this is for tracking only. Resolves keep
   * `state` at "saving" until the count returns to zero, then flip to
   * "success" or "error" briefly before returning to "idle".
   */
  track<T>(promise: Promise<T>): Promise<T>;
}

const NOOP: ISaveIndicator = {
  state: "idle",
  track: (p) => p,
};

const SaveIndicatorContext = createContext<ISaveIndicator>(NOOP);

/**
 * Provides a single app-wide save-state pulse, surfaced as a small
 * floating indicator (spinner / check / X). Hooks that write user
 * config (`useConfigureUISetting`, `useConfigureInterface`, anything
 * built on top of them like `useTaskDefaults`) auto-register via
 * `track()`, so callers don't need to do anything to participate.
 *
 * The indicator is intentionally tiny and out of the way — it answers
 * the question "did my change save?" without claiming screen real
 * estate the way a banner or modal would.
 */
export function SaveIndicatorProvider({ children }: PropsWithChildren) {
  const [state, setState] = useState<SaveState>("idle");
  // Pending count and last-result timeout live in refs because they
  // shouldn't drive re-renders independently; only the derived state
  // does.
  const pendingRef = useRef(0);
  const settleTimeoutRef = useRef<number | null>(null);

  const clearSettle = useCallback(() => {
    if (settleTimeoutRef.current !== null) {
      window.clearTimeout(settleTimeoutRef.current);
      settleTimeoutRef.current = null;
    }
  }, []);

  const track = useCallback(
    <T,>(promise: Promise<T>) => {
      pendingRef.current += 1;
      clearSettle();
      setState("saving");
      promise.then(
        () => {
          pendingRef.current -= 1;
          if (pendingRef.current === 0) {
            setState("success");
            settleTimeoutRef.current = window.setTimeout(() => {
              setState("idle");
              settleTimeoutRef.current = null;
            }, 2000);
          }
        },
        () => {
          pendingRef.current -= 1;
          if (pendingRef.current === 0) {
            setState("error");
            // Longer than success — errors are easier to miss and the
            // user may need a beat to realise something went wrong
            // before the toast scrolls past.
            settleTimeoutRef.current = window.setTimeout(() => {
              setState("idle");
              settleTimeoutRef.current = null;
            }, 4000);
          }
        },
      );
      return promise;
    },
    [clearSettle],
  );

  useEffect(() => () => clearSettle(), [clearSettle]);

  return (
    <SaveIndicatorContext.Provider value={{ state, track }}>
      {children}
      <SaveIndicatorBadge state={state} />
    </SaveIndicatorContext.Provider>
  );
}

/**
 * Hook used by mutating hooks to register a save promise. Falls back to
 * a no-op if no Provider is mounted, so hooks remain usable from tests
 * / storybook / detached contexts without crashing.
 */
export function useSaveIndicator(): ISaveIndicator {
  return useContext(SaveIndicatorContext);
}

function SaveIndicatorBadge({ state }: { state: SaveState }) {
  // Always render so we can fade rather than pop; only opaque when
  // we actually have something to say.
  const visible = state !== "idle";
  return (
    <div
      aria-hidden={!visible}
      className={cn(
        // Bottom-right, lifted above the mobile bottom-tab-bar
        // (`h-11`) on small screens, plain corner on md+.
        "pointer-events-none fixed right-4 bottom-16 z-50 flex size-8 items-center justify-center rounded-full bg-background ring-1 ring-foreground/10 transition-opacity duration-200 md:bottom-4",
        visible ? "opacity-100" : "opacity-0",
      )}
      role="status"
    >
      {state === "saving" && <Spinner className="size-4" />}
      {state === "success" && (
        <Check className="size-4 text-emerald-500" aria-label="Saved" />
      )}
      {state === "error" && (
        <X className="size-4 text-destructive" aria-label="Save failed" />
      )}
    </div>
  );
}
