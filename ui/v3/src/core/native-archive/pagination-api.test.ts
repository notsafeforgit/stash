import { expect, it, vi } from "vitest";
import {
  archivePageInfoPath,
  createArchivePaginationAPI,
} from "./pagination-api";

it("counts the selected filter and cursor using the authenticated read API", async () => {
  const transport = vi.fn<typeof fetch>(
    async () => new Response(JSON.stringify({ total: 50, offset: 25 })),
  );
  const path = archivePageInfoPath({
    resource: "accounts",
    q: "a&b",
    namespace: "native:reddit",
    ownership: "all",
    cursor: "00000000-0000-4000-8000-000000000025",
    state: "",
  });
  const api = createArchivePaginationAPI(
    "https://stash.test/mount/api/v3/archive/",
    transport,
  );
  expect(await api(path)).toEqual({ total: 50, offset: 25 });
  const [url, options] = transport.mock.calls[0]!;
  expect(new URL(String(url)).searchParams.get("q")).toBe("a&b");
  expect(new URL(String(url)).searchParams.has("ownership")).toBe(false);
  expect(new URL(String(url)).pathname).toBe("/mount/api/v3/archive/page-info");
  expect(options).toMatchObject({
    method: "GET",
    credentials: "same-origin",
    cache: "no-store",
  });
});

it("rejects invalid counts instead of displaying misleading page numbers", async () => {
  for (const info of [
    { total: -1, offset: 0 },
    { total: 1.5, offset: 0 },
    { total: 2, offset: 3 },
    { offset: 0 },
  ]) {
    const api = createArchivePaginationAPI(
      "https://stash.test/api/v3/archive/",
      async () => new Response(JSON.stringify(info)),
    );
    await expect(api("page-info?resource=posts")).rejects.toMatchObject({
      code: "invalid_response",
    });
  }
});
