// Built-asset fixture: no backend, credentials, or private media involved.
import http from "node:http";
import { readFile } from "node:fs/promises";
import { extname, resolve } from "node:path";

const root = resolve("build");
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
      });
      response.end(data);
    } catch {
      response.writeHead(404);
      response.end();
    }
  })
  .listen(3034, "0.0.0.0");
