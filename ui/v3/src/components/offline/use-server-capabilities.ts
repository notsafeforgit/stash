/**
 * Apollo wrapper around the `serverCapabilities` query — small enough
 * to inline at every call site, but a hook keeps the empty-default
 * + cache-policy boilerplate out of the format-pick logic.
 *
 * Cache policy: the server caps are constant for the server's
 * process lifetime (HW encoder availability doesn't change without
 * a restart), so `cache-first` is the right policy. Apollo's default
 * already does this — we just lean on it.
 */

import { useQuery } from "@apollo/client/react";
import { ServerCapabilitiesDocument } from "src/core/generated-graphql";

export interface ServerCapabilities {
  downloadFormats: string[];
}

const EMPTY: ServerCapabilities = { downloadFormats: [] };

export function useServerCapabilities(): ServerCapabilities {
  const { data } = useQuery(ServerCapabilitiesDocument);
  return data?.serverCapabilities ?? EMPTY;
}
