import { spawnSync } from "node:child_process";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

const [project, shard, option, ...extra] = process.argv.slice(2);
const match = /^(\d+)\/(\d+)$/.exec(shard ?? "");
const current = Number(match?.[1]);
const total = Number(match?.[2]);
if (
  !["chromium", "webkit"].includes(project) ||
  !match ||
  !Number.isSafeInteger(current) ||
  !Number.isSafeInteger(total) ||
  current < 1 ||
  current > total ||
  (option !== undefined && option !== "--list") ||
  extra.length
) {
  throw new Error(
    "Usage: node tests/browser/shard.mjs <chromium|webkit> <N/M> [--list]",
  );
}

const cli = "node_modules/@playwright/test/cli.js";
const args = [cli, "test", `--project=${project}`];
const discovery = spawnSync(
  process.execPath,
  [...args, "--list", "--reporter=list"],
  {
    encoding: "utf8",
    maxBuffer: 16 * 1024 * 1024,
    env: { ...process.env, FORCE_COLOR: "0" },
  },
);
if (discovery.error) throw discovery.error;
if (discovery.stderr) process.stderr.write(discovery.stderr);
if (discovery.status !== 0) throw new Error(discovery.stdout);

// Playwright accepts its listed test names directly with --test-list. Assign
// adjacent independent cases to different runners so player tests and their
// viewport variants do not all accumulate in the same contiguous shard.
const tests = discovery.stdout
  .split("\n")
  .map((line) => line.trim())
  .filter((line) => line.startsWith(`[${project}] › `));
const discoveredCount = Number(
  /^Total: (\d+) tests in /m.exec(discovery.stdout)?.[1],
);
if (
  !tests.length ||
  tests.length !== discoveredCount ||
  new Set(tests).size !== tests.length
) {
  throw new Error(
    "Browser test discovery was incomplete or contained duplicate names",
  );
}
const selected = tests.filter((_, index) => index % total === current - 1);
if (!selected.length) throw new Error("Browser shard is empty");
process.stderr.write(
  `${project} shard ${shard}: ${selected.length} of ${tests.length} tests\n`,
);
if (option === "--list") {
  process.stdout.write(`${selected.join("\n")}\n`);
} else {
  const directory = await mkdtemp(join(tmpdir(), "stash-browser-shard-"));
  try {
    const list = join(directory, "tests.txt");
    await writeFile(list, `${selected.join("\n")}\n`);
    const result = spawnSync(
      process.execPath,
      [...args, `--test-list=${list}`],
      {
        stdio: "inherit",
      },
    );
    if (result.error) throw result.error;
    process.exitCode = result.status ?? 1;
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
}
