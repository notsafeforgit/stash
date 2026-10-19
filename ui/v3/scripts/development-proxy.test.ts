import { createServer as createHTTPServer, request } from "node:http";
import { createServer as createViteServer } from "vite";
import { expect, it } from "vitest";
import { developmentProxy } from "./development-proxy";

it("forwards login cookies and the same Host/Origin through HTTP and WebSocket proxies", async () => {
  const seen: { cookie?: string; host?: string; origin?: string }[] = [];
  const backend = createHTTPServer((req, res) => {
    if (req.url === "/login") {
      res.setHeader("Set-Cookie", "stashSession=fixture; Path=/; HttpOnly");
    } else {
      seen.push(req.headers);
    }
    res.end("ok");
  });
  backend.on("upgrade", (req, socket) => {
    seen.push(req.headers);
    socket.end(
      "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n",
    );
  });
  await new Promise<void>((resolve) => backend.listen(0, "127.0.0.1", resolve));
  const address = backend.address();
  if (!address || typeof address === "string")
    throw new Error("Missing backend address");
  const vite = await createViteServer({
    configFile: false,
    logLevel: "silent",
    server: {
      host: "127.0.0.1",
      port: 0,
      proxy: developmentProxy(`http://127.0.0.1:${address.port}`),
    },
    optimizeDeps: { noDiscovery: true, include: [] },
  });
  try {
    await vite.listen();
    const origin = vite.resolvedUrls?.local[0]?.replace(/\/$/, "");
    if (!origin) throw new Error("Missing proxy address");
    const login = await fetch(`${origin}/login`, { method: "POST" });
    const cookie = login.headers.get("set-cookie")?.split(";")[0];
    expect(cookie).toBe("stashSession=fixture");
    const headers = { Cookie: cookie ?? "", Origin: origin };
    for (const route of ["/graphql", "/api/v3/archive/status", "/session"]) {
      const response = await fetch(`${origin}${route}`, { headers });
      expect(response.status).toBe(200);
      await response.text();
    }
    await new Promise<void>((resolve, reject) => {
      const upgrade = request(`${origin}/graphql`, {
        headers: { ...headers, Connection: "Upgrade", Upgrade: "websocket" },
      });
      upgrade.on("upgrade", (response, socket) => {
        socket.destroy();
        if (response.statusCode === 101) resolve();
        else reject(new Error("WebSocket upgrade failed"));
      });
      upgrade.on("error", reject);
      upgrade.on("response", () =>
        reject(new Error("Expected WebSocket upgrade")),
      );
      upgrade.end();
    });
    expect(seen).toHaveLength(4);
    for (const headers of seen) {
      expect(headers).toMatchObject({
        cookie,
        origin,
        host: new URL(origin).host,
      });
    }
  } finally {
    await vite.close();
    await new Promise<void>((resolve, reject) =>
      backend.close((err) => (err ? reject(err) : resolve())),
    );
  }
});
