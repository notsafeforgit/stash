// Built-asset fixture: no backend, credentials, or private media involved.
import http from "node:http";
import { readFile } from "node:fs/promises";
import { extname, resolve } from "node:path";

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
http
  .createServer(async (request, response) => {
    try {
      const url = new URL(request.url, "http://localhost");
      const file = url.pathname.replace(/^\/(?:stash\/)?/, "");
      if (/^scene\/90000000[12]\/download\.mp4$/.test(file)) {
        const media = await readFile("tests/browser/fixture/media/short.mp4");
        response.writeHead(200, {
          "content-type": "video/mp4",
          "content-length": media.length,
        });
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
