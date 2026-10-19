// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { SessionRecovery } from "./session-recovery";
import {
  websocketFailureKind,
  websocketRecoveryOptions,
} from "./websocket-recovery";

afterEach(() => vi.useRealTimers());

it("classifies protocol failures separately from network and authentication failures", () => {
  expect(websocketFailureKind({ code: 4401 })).toBe("authentication");
  expect(websocketFailureKind({ code: 4403 })).toBe("authentication");
  for (const code of [1006, 1012, 1013, 4499])
    expect(websocketFailureKind({ code })).toBe("transient");
  for (const code of [1002, 1008, 4400, 4406, 4409, 4429])
    expect(websocketFailureKind({ code })).toBe("fatal");
  expect(
    websocketFailureKind(new TypeError("invalid connection options")),
  ).toBe("fatal");
});

it("waits for shared authentication when an opaque failed handshake was a 401", async () => {
  const recovery = new SessionRecovery();
  const unsubscribe = recovery.subscribe(() => {});
  const probe = vi.fn<typeof fetch>(
    async () => new Response(null, { status: 401 }),
  );
  const options = websocketRecoveryOptions(recovery, probe);
  expect(options.shouldRetry({ code: 1006 })).toBe(true);
  const pending = options.retryWait(0);
  await vi.waitFor(() => expect(recovery.getSnapshot()).toBe(true));
  recovery.authenticated();
  await expect(pending).resolves.toBeUndefined();
  expect(probe.mock.calls[0]?.[1]).toMatchObject({
    credentials: "same-origin",
    redirect: "error",
  });
  unsubscribe();
});

it("backs off during an outage without asking for credentials and stops permission failures", async () => {
  vi.useFakeTimers();
  const recovery = new SessionRecovery();
  const options = websocketRecoveryOptions(
    recovery,
    vi.fn<typeof fetch>(async () => {
      throw new TypeError("offline");
    }),
  );
  expect(options.shouldRetry({ code: 1006 })).toBe(true);
  const pending = options.retryWait(20);
  await vi.advanceTimersByTimeAsync(30_000);
  await expect(pending).resolves.toBeUndefined();
  expect(recovery.getSnapshot()).toBe(false);
  const forbidden = websocketRecoveryOptions(
    recovery,
    vi.fn<typeof fetch>(async () => new Response(null, { status: 403 })),
  );
  await expect(forbidden.retryWait(0)).rejects.toThrow("access was rejected");
});
