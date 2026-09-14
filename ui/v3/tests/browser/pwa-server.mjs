// Built-asset fixture: no backend, credentials, or private media involved.
import http from "node:http";
import { readFile } from "node:fs/promises";
import { extname, resolve } from "node:path";
import { once } from "node:events";

const root = resolve("build");
// Match the production page policy: registration and blob playback must work
// with CSP enabled. The Go v3 security-header test covers the server's policy.
const pagePolicy =
  "default-src data: 'self' 'unsafe-inline'; connect-src data: 'self' ws: wss:; img-src data: *; script-src 'self' http://www.gstatic.com https://www.gstatic.com 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; media-src blob: 'self'; worker-src blob: 'self'; child-src 'none'; object-src 'none'; form-action 'self';";
const types = {
  ".html": "text/html",
  ".js": "text/javascript",
  ".css": "text/css",
  ".png": "image/png",
  ".woff2": "font/woff2",
};
let networkAvailable = true;
http
  .createServer(async (request, response) => {
    try {
      const url = new URL(request.url, "http://localhost");
      if (url.pathname === "/__pwa_network" && request.method === "POST") {
        networkAvailable = url.searchParams.get("online") === "1";
        response.writeHead(204);
        response.end();
        return;
      }
      if (!networkAvailable) {
        request.socket.destroy();
        return;
      }
      const file = url.pathname.replace(/^\/(?:stash\/)?/, "");
      if (/^scene\/90000000[123]\/download\.mp4$/.test(file)) {
        const media = await readFile("tests/browser/fixture/media/short.mp4");
        // A valid MP4 followed by a padding box: exercise streaming storage
        // beyond the obsolete 50 MB Safari limit without a large fixture file.
        const large = file.includes("900000003");
        const total = large ? 64 * 1024 * 1024 : media.length;
        response.writeHead(200, {
          "content-type": "video/mp4",
          "content-length": total,
        });
        if (large) {
          response.write(media);
          const box = Buffer.alloc(8);
          box.writeUInt32BE(total - media.length);
          box.write("free", 4);
          response.write(box);
          const chunk = Buffer.alloc(64 * 1024);
          for (
            let remaining = total - media.length - box.length;
            remaining > 0;
          ) {
            if (response.destroyed) return;
            const length = Math.min(chunk.length, remaining);
            if (!response.write(chunk.subarray(0, length)))
              await once(response, "drain");
            remaining -= length;
          }
          response.end();
          return;
        }
        for (let offset = 0; offset < media.length; offset += 4096) {
          if (response.destroyed) return;
          response.write(media.subarray(offset, offset + 4096));
          await new Promise((done) => setTimeout(done, 300));
        }
        response.end();
        return;
      }
      if (!file || file.includes("..")) {
        response.writeHead(404);
        response.end();
        return;
      }
      const data = await readFile(resolve(root, file));
      response.writeHead(200, {
        "content-type": types[extname(file)] ?? "application/octet-stream",
        "cache-control": "no-cache",
        ...(extname(file) === ".html" && {
          "content-security-policy": pagePolicy,
          "referrer-policy": "same-origin",
        }),
      });
      response.end(data);
    } catch {
      response.writeHead(404);
      response.end();
    }
  })
  .listen(3034, "0.0.0.0");
