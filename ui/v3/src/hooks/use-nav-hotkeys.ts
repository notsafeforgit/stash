import { useEffect, useMemo, useRef } from "react";
import { useRouter } from "@tanstack/react-router";
import { NAV_ITEMS } from "src/components/layout/nav-items";
import { getRegisteredNavItems } from "@/plugins";

// Chord timeout: how long to wait for the second key after 'g'
const CHORD_TIMEOUT_MS = 1500;

function isTypingContext(el: Element | null): boolean {
  if (!el) return false;
  const tag = el.tagName.toLowerCase();
  if (tag === "input" || tag === "textarea" || tag === "select") return true;
  if ((el as HTMLElement).isContentEditable) return true;
  return false;
}

// Build a map from the second key of each hotkey to the route path.
// Built-in NAV_ITEMS hotkeys are in the form "g <key>" (e.g. "g s",
// "g p"); plugins can register their own with the same syntax.
function buildChordMap(): Map<string, string> {
  const map = new Map<string, string>();
  const items: Array<{ to: string; hotkey?: string }> = [
    ...NAV_ITEMS,
    ...getRegisteredNavItems(),
  ];
  for (const item of items) {
    if (!item.hotkey) continue;
    const parts = item.hotkey.split(" ");
    if (parts.length === 2 && parts[0] === "g") {
      // First registration wins; built-ins are listed first so plugins
      // can't shadow them.
      if (!map.has(parts[1])) {
        map.set(parts[1], item.to);
      }
    }
  }
  return map;
}

/**
 * Registers global two-key chord hotkeys for navigation.
 * After pressing 'g', a subsequent key press within CHORD_TIMEOUT_MS
 * navigates to the matching route.
 */
export function useNavHotkeys() {
  const router = useRouter();
  const pendingChord = useRef(false);
  const chordTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Plugins register their nav (and hence hotkeys) once at boot — by
  // the time this hook mounts the registry is frozen. Building the
  // chord map at mount-time picks up any plugin entries.
  const chordMap = useMemo(buildChordMap, []);

  useEffect(() => {
    function clearChord() {
      pendingChord.current = false;
      if (chordTimer.current !== null) {
        clearTimeout(chordTimer.current);
        chordTimer.current = null;
      }
    }

    function onKeyDown(e: KeyboardEvent) {
      // Ignore when typing in inputs
      if (isTypingContext(document.activeElement)) return;
      // Ignore modifier combos
      if (e.ctrlKey || e.metaKey || e.altKey) return;

      if (pendingChord.current) {
        clearChord();
        const target = chordMap.get(e.key);
        if (target) {
          e.preventDefault();
          router.navigate({ to: target });
        }
        return;
      }

      if (e.key === "g") {
        e.preventDefault();
        pendingChord.current = true;
        chordTimer.current = setTimeout(clearChord, CHORD_TIMEOUT_MS);
      }
    }

    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
      clearChord();
    };
  }, [router, chordMap]);
}
