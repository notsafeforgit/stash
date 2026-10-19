// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { z } from "zod";
import { createArchiveRequest } from "./native-archive/client";
import {
  SessionRecovery,
  sessionFetch,
  sessionRecovery,
  withSessionRecovery,
} from "./session-recovery";
import { getPlatformURL } from "./platform-url";

afterEach(() => {
  vi.unstubAllGlobals();
  document.head.innerHTML = "";
});

it("retains upload fields without consuming or changing the original FormData", async () => {
  const unsubscribe = sessionRecovery.subscribe(() => {});
  const transport = vi
    .fn<typeof fetch>()
    .mockResolvedValueOnce(new Response(null, { status: 401 }))
    .mockResolvedValueOnce(new Response(null, { status: 204 }));
  vi.stubGlobal("fetch", transport);
  const body = new FormData();
  body.set("operation", "original");
  body.set("file", new File(["media"], "clip.mp4"));
  const pending = sessionFetch(getPlatformURL("graphql"), {
    method: "POST",
    body,
  });
  body.set("operation", "later edit");
  await vi.waitFor(() => expect(sessionRecovery.getSnapshot()).toBe(true));
  sessionRecovery.authenticated();
  expect((await pending).status).toBe(204);
  for (const [, options] of transport.mock.calls) {
    const saved = options?.body as FormData;
    expect(saved.get("operation")).toBe("original");
    expect((saved.get("file") as File).size).toBe(5);
  }
  expect(body.get("operation")).toBe("later edit");
  unsubscribe();
});

it("shares one recovery across simultaneous requests and retries the exact native body", async () => {
  const changes = vi.fn();
  const unsubscribe = sessionRecovery.subscribe(changes);
  const target = getPlatformURL("api/v3/archive/");
  const body =
    '{"request_id":"7a9d23c2-68eb-49ba-87b6-14c1074a5375","value":"original"}';
  const transport = vi
    .fn<typeof fetch>()
    .mockResolvedValueOnce(new Response(null, { status: 401 }))
    .mockResolvedValueOnce(new Response(null, { status: 401 }))
    .mockImplementation(async () => Response.json({ saved: true }));
  try {
    const request = createArchiveRequest(target.href, transport);
    const first = request("first", z.object({ saved: z.boolean() }), body);
    const second = request("second", z.object({ saved: z.boolean() }), body);
    await vi.waitFor(() => expect(changes).toHaveBeenCalledTimes(2));
    expect(sessionRecovery.getSnapshot()).toBe(true);
    expect(transport).toHaveBeenCalledTimes(2);
    sessionRecovery.authenticated();
    await expect(first).resolves.toEqual({ saved: true });
    await expect(second).resolves.toEqual({ saved: true });
    expect(transport).toHaveBeenCalledTimes(4);
    for (const [, options] of transport.mock.calls)
      expect(options?.body).toBe(body);
    expect(sessionRecovery.getSnapshot()).toBe(false);
  } finally {
    unsubscribe();
  }
});

it("cancels only the abandoned caller while another waits for authentication", async () => {
  const recovery = new SessionRecovery();
  const unsubscribe = recovery.subscribe(() => {});
  const controller = new AbortController();
  const first = recovery.wait(controller.signal);
  const rejected = expect(first).rejects.toMatchObject({ name: "AbortError" });
  const second = recovery.wait();
  controller.abort();
  await rejected;
  expect(recovery.getSnapshot()).toBe(true);
  recovery.authenticated();
  await expect(second).resolves.toBeUndefined();
  expect(recovery.getSnapshot()).toBe(false);
  unsubscribe();
});

it("does not prompt twice when an old response arrives after another request signed in", async () => {
  const recovery = new SessionRecovery();
  const changes = vi.fn();
  const unsubscribe = recovery.subscribe(changes);
  let respond!: (response: Response) => void;
  const request = vi
    .fn<() => Promise<Response>>()
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          respond = resolve;
        }),
    )
    .mockResolvedValue(new Response(null, { status: 204 }));
  const pending = withSessionRecovery(
    request,
    getPlatformURL("graphql"),
    undefined,
    recovery,
  );
  recovery.authenticated();
  respond(new Response(null, { status: 401 }));
  expect((await pending).status).toBe(204);
  expect(changes).toHaveBeenCalledOnce();
  unsubscribe();
});

it.each([403, 409, 500])(
  "does not replay status %s or confuse it with session expiry",
  async (status) => {
    const recovery = new SessionRecovery();
    const unsubscribe = recovery.subscribe(() => {});
    const request = vi.fn(async () => new Response(null, { status }));
    expect(
      (
        await withSessionRecovery(
          request,
          getPlatformURL("graphql"),
          undefined,
          recovery,
        )
      ).status,
    ).toBe(status);
    expect(request).toHaveBeenCalledOnce();
    expect(recovery.getSnapshot()).toBe(false);
    unsubscribe();
  },
);

it("does not recover guest/foreign requests or retry a second authentication rejection", async () => {
  const recovery = new SessionRecovery();
  const unsubscribe = recovery.subscribe(() => {});
  const request = vi.fn(async () => new Response(null, { status: 401 }));
  for (const target of [
    getPlatformURL("share/api/example"),
    "https://foreign.test/graphql",
  ]) {
    expect(
      (await withSessionRecovery(request, target, undefined, recovery)).status,
    ).toBe(401);
  }
  const pending = withSessionRecovery(
    request,
    getPlatformURL("graphql"),
    undefined,
    recovery,
  );
  await vi.waitFor(() => expect(recovery.getSnapshot()).toBe(true));
  recovery.authenticated();
  expect((await pending).status).toBe(401);
  expect(request).toHaveBeenCalledTimes(4);
  expect(recovery.getSnapshot()).toBe(false);
  unsubscribe();
});
