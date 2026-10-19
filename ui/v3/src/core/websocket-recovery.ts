import { getPlatformURL } from "./platform-url";
import { sessionRecovery, type SessionRecovery } from "./session-recovery";

export function websocketFailureKind(
  error: unknown,
): "authentication" | "transient" | "fatal" {
  if (typeof error === "object" && error !== null && "code" in error) {
    const code = error.code;
    if (code === 4401 || code === 4403) return "authentication";
    // Going away, abnormal closure, service restart/overload and local terminate.
    if ([1000, 1001, 1005, 1006, 1011, 1012, 1013, 4499].includes(Number(code)))
      return "transient";
    return "fatal";
  }
  // Browser WebSockets hide failed-handshake HTTP status behind an error event.
  return typeof Event !== "undefined" && error instanceof Event
    ? "transient"
    : "fatal";
}

export function websocketRecoveryOptions(
  recovery: SessionRecovery = sessionRecovery,
  transport: typeof fetch = fetch,
) {
  let failure: ReturnType<typeof websocketFailureKind> = "transient";
  return {
    retryAttempts: Infinity,
    shouldRetry(error: unknown) {
      failure = websocketFailureKind(error);
      return failure !== "fatal";
    },
    async retryWait(attempt: number) {
      // Probe the same session because a browser 1006 could mean either an
      // expired cookie or an unavailable server. A network outage never logs out.
      let response: Response | undefined;
      const generation = recovery.generation;
      try {
        response = await transport(getPlatformURL("session"), {
          credentials: "same-origin",
          cache: "no-store",
          redirect: "error",
          signal: AbortSignal.timeout(5_000),
        });
      } catch {
        /* Offline/server restart: keep the reconnect backoff. */
      }
      if (response?.status === 401) {
        if (recovery.generation === generation) await recovery.wait();
        return;
      }
      if (
        response?.status === 403 ||
        (response?.ok && failure === "authentication")
      ) {
        throw new Error(
          "WebSocket access was rejected for this authenticated session.",
        );
      }
      await new Promise<void>((resolve) =>
        setTimeout(
          resolve,
          Math.min(30_000, 1_000 * 2 ** Math.min(attempt, 5)),
        ),
      );
    },
  };
}
