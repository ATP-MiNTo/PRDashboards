#!/usr/bin/env node
// Builds the web export and the e2e binary, then runs the Playwright specs in e2e/.
// Extra arguments go to Playwright, for example: pnpm e2e --grep github
import { spawnSync } from "node:child_process";
import { cpSync, readdirSync, readFileSync, rmSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const exe = process.platform === "win32" ? ".exe" : "";

function run(cmd, args, opts = {}) {
  console.log(`==> ${cmd} ${args.join(" ")}`);
  const r = spawnSync(cmd, args, {
    cwd: root,
    stdio: "inherit",
    shell: process.platform === "win32",
    ...opts,
  });
  if (r.status !== 0) process.exit(r.status ?? 1);
}

const version = readFileSync(join(root, "VERSION"), "utf8").trim();

run("pnpm", ["-C", "web", "build"]);

const embed = join(root, "server", "web");
for (const name of readdirSync(embed)) {
  if (name !== ".gitkeep")
    rmSync(join(embed, name), { recursive: true, force: true });
}
cpSync(join(root, "web", "out"), embed, { recursive: true });

run("go", [
  "-C",
  "server",
  "build",
  "-tags",
  "e2e",
  "-trimpath",
  "-ldflags",
  `-X main.version=${version}`,
  "-o",
  `../bin/prdash-e2e${exe}`,
  "./cmd/prdash",
]);

rmSync(join(root, ".e2e-data"), { recursive: true, force: true });

run("pnpm", [
  "exec",
  "playwright",
  "test",
  "--config",
  "e2e/playwright.config.ts",
  "--trace",
  "off",
  ...process.argv.slice(2),
]);
