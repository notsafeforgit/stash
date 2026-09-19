import { spawn, execFileSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { webkit } from "@playwright/test";

const require = createRequire(import.meta.url);
const server = spawn(process.execPath, ["node_modules/vite/bin/vite.js", "--config", "tests/browser/vite.config.ts"], { stdio: ["ignore", "pipe", "pipe"] });
server.stdout.on("data", (data) => process.stdout.write(data));
server.stderr.on("data", (data) => process.stderr.write(data));
const delay = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const deadline = async (promise, phase, ms = 6000) => {
  let timer;
  try {
    return await Promise.race([promise, new Promise((_, reject) => { timer = setTimeout(() => reject(new Error("Deadline: " + phase)), ms); })]);
  } finally { clearTimeout(timer); }
};
for (let n = 0; n < 100; n++) {
  try { if ((await fetch("http://127.0.0.1:3025")).ok) break; } catch {}
  await delay(100);
}
const cases = [
  { name: "bare-hls-no-frames", preload: "auto", hls: true, frames: false },
  { name: "direct-native-loop", preload: "auto", nativeLoop: true, frames: true },
];

for (const scenario of cases) {
  console.log("CASE_START " + JSON.stringify(scenario));
  const browser = await webkit.launch({ headless: !scenario.headed });
  const context = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });
  const page = await context.newPage();
  page.on("console", (message) => console.log(scenario.name + " " + message.text()));
  page.on("pageerror", (error) => console.log(scenario.name + " PAGE_ERROR " + error.message));
  page.on("crash", () => console.log(scenario.name + " PAGE_CRASH"));
  page.on("requestfailed", (request) => console.log(scenario.name + " REQUEST_FAILED " + request.url() + " " + request.failure()?.errorText));
  await page.addInitScript((frames) => {
    localStorage.setItem("stash-lightbox-loop", "true");
    const emit = (type, detail = {}) => console.log("MEDIA " + JSON.stringify({ type, wall: performance.now(), ...detail }));
    for (const method of ["play", "pause", "load"]) {
      const original = HTMLMediaElement.prototype[method];
      HTMLMediaElement.prototype[method] = function (...args) {
        emit(method + "-call", { stack: new Error().stack?.split("\n").slice(0, 5).join("\n") });
        const value = original.apply(this, args);
        emit(method + "-return");
        if (value instanceof Promise) value.catch((error) => emit(method + "-rejected", { message: error.message }));
        return value;
      };
    }
    const currentTime = Object.getOwnPropertyDescriptor(HTMLMediaElement.prototype, "currentTime");
    Object.defineProperty(HTMLMediaElement.prototype, "currentTime", {
      ...currentTime,
      set(value) {
        emit("seek-call", { value });
        currentTime.set.call(this, value);
        emit("seek-return");
      },
    });
    const ranges = (r) => Array.from({ length: r.length }, (_, n) => [r.start(n), r.end(n)]);
    for (const type of ["loadedmetadata", "canplay", "seeking", "seeked", "playing", "waiting", "pause", "ended", "error"]) {
      document.addEventListener(type, (event) => {
        const v = event.target;
        if (!(v instanceof HTMLMediaElement)) return;
        emit(type, { t: v.currentTime, paused: v.paused, seeking: v.seeking, ready: v.readyState, duration: v.duration, buffered: ranges(v.buffered), error: v.error?.message });
      }, true);
    }
    setInterval(() => emit("heartbeat"), 1000);
    window.mediaProbe = { frames: [], loops: [] };
    const installFrames = () => {
      const v = document.querySelector("video");
      if (!v || v.dataset.probed) return;
      v.dataset.probed = "true";
      let previous;
      let loopAt = -Infinity;
      const frame = (now, data) => {
        if (previous && data.mediaTime < previous.mediaTime - 0.5) {
          loopAt = now;
          emit("frame-loop", { previous, ...data, now });
        }
        if (now - loopAt < 700) emit("loop-frame", { now, ...data });
        previous = { now, mediaTime: data.mediaTime, frames: data.presentedFrames };
        v.requestVideoFrameCallback(frame);
      };
      v.requestVideoFrameCallback(frame);
    };
    if (frames) new MutationObserver(installFrames).observe(document, { childList: true, subtree: true });
  }, scenario.frames);
  try {
    if (scenario.app) {
      await page.route("**/scene/*/**", async (route) => {
        const url = new URL(route.request().url());
        if (/\/streams\.(stop|keepalive)$/.test(url.pathname)) return route.fulfill({ status: 204 });
        if (url.pathname.endsWith("/caption")) return route.fulfill({ contentType: "text/vtt", body: "WEBVTT\n" });
        if (url.pathname.endsWith("/stream")) return route.fulfill({ response: await route.fetch({ url: "http://127.0.0.1:3025/media/loop-bframes.mp4" }) });
        if (url.pathname.endsWith(".m3u8")) return route.fulfill({ contentType: "application/vnd.apple.mpegurl", body: '#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:2\n#EXT-X-MEDIA-SEQUENCE:3\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-MAP:URI="/media/hls/init.mp4"\n#EXTINF:2.000000,\n/media/hls/segment-3.m4s\n#EXT-X-ENDLIST\n' });
        throw new Error(url.href);
      });
      await deadline(page.goto("http://127.0.0.1:3025/scene-lightbox" + (scenario.app === "markers" ? "?mode=markers" : "")), "app navigation", 20000);
      await deadline(page.getByRole("button", { name: "Open scenes" }).click(), "open");
      await delay(5000);
    } else {
      await page.route("**/native-media", (route) => route.fulfill({ contentType: "text/html", body: '<!doctype html><video playsinline></video><button>Play</button>' }));
      await page.goto("http://127.0.0.1:3025/native-media");
      if (scenario.hls) await page.addScriptTag({ path: join(dirname(createRequire(require.resolve("@videojs/hlsjs-video")).resolve("hls.js")), "hls.min.js") });
      await deadline(page.evaluate((scenario) => {
        const v = document.querySelector("video");
        v.preload = scenario.preload;
        v.loop = Boolean(scenario.nativeLoop);
        document.querySelector("button").onclick = () => v.play();
        if (scenario.hls) {
          const hls = new Hls({ startPosition: 0 });
          hls.on(Hls.Events.ERROR, (_, data) => console.log("HLS_ERROR " + JSON.stringify({ details: data.details, fatal: data.fatal, message: data.error?.message })));
          for (const name of ["MEDIA_ATTACHED", "BUFFER_CREATED", "BUFFER_APPENDING", "BUFFER_APPENDED", "FRAG_LOADED"]) hls.on(Hls.Events[name], () => console.log("HLS_EVENT " + name));
          hls.attachMedia(v);
          hls.loadSource("/media/clip/stream.m3u8");
          window.probeHls = hls;
        } else v.src = "/media/loop-bframes.mp4";
        if (scenario.early) setInterval(() => {
          if (!v.paused && !v.seeking && v.duration - v.currentTime < scenario.early) v.currentTime = 0;
        }, 5);
      }, scenario), "assign source");
      await delay(2500);
    }
    const snapshot = () => page.evaluate(() => {
      const v = document.querySelector("video");
      return { userAgent: navigator.userAgent, platform: navigator.platform, touchPoints: navigator.maxTouchPoints, visibility: document.visibilityState, hidden: document.hidden, preload: v.preload, autoplay: v.autoplay, muted: v.muted, paused: v.paused, t: v.currentTime, duration: v.duration, ready: v.readyState, seeking: v.seeking, buffered: Array.from({ length: v.buffered.length }, (_, n) => [v.buffered.start(n), v.buffered.end(n)]), error: v.error?.message };
    });
    console.log("SNAPSHOT_BEFORE " + scenario.name + " " + JSON.stringify(await deadline(snapshot(), "snapshot before")));
    if (scenario.app === "markers") await deadline(page.locator("[data-player-native-button]").click(), "app play", 5000);
    if (!scenario.app) await deadline(page.getByRole("button", { name: "Play", exact: true }).click(), "bare play");
    await delay(8500);
    console.log("SNAPSHOT_AFTER " + scenario.name + " " + JSON.stringify(await deadline(snapshot(), "snapshot after")));
  } catch (error) {
    console.log("CASE_ERROR " + scenario.name + " " + error.stack);
    mkdirSync("native-stacks", { recursive: true });
    const processes = execFileSync("ps", ["-axo", "pid,command"], { encoding: "utf8" });
    for (const line of processes.split("\n").filter((line) => /WebKit\.(WebContent|GPU)\.Development/.test(line))) {
      const pid = line.trim().split(/\s+/)[0];
      console.log("SAMPLE_PROCESS " + line.trim());
      try { console.log(execFileSync("sample", [pid, "2", "-file", `native-stacks/${scenario.name}-${pid}.txt`], { encoding: "utf8", timeout: 10000 })); }
      catch (error) { console.log("SAMPLE_ERROR " + error.message); }
    }
  } finally {
    await deadline(browser.close(), "browser close", 3000).catch((error) => console.log(error.message));
  }
  console.log("CASE_END " + scenario.name);
}
server.kill("SIGTERM");
process.exit(0);
