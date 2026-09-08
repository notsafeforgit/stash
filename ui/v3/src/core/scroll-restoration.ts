import type { ParsedLocation } from "@tanstack/react-router";
import { applicationHref } from "./platform-url";

/** The app's Back actions navigate to a saved URL, so a return must share the
 * same entry as browser Back. Include the search string to separate filtered
 * lists and pages, and the public prefix to separate deployments. */
export function getScrollRestorationKey(location: ParsedLocation): string {
  return applicationHref(location.href);
}
