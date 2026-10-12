import type { ParsedLocation } from "@tanstack/react-router";
import { resolveLibraryRoute } from "./library-route";

export function resolvePerformerRoute(
  id: string,
  location: Pick<ParsedLocation, "search" | "hash" | "state">,
  signal?: AbortSignal,
) {
  return resolveLibraryRoute("performer", id, location, signal);
}
