import { readFile, stat } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const frontendRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const output = path.join(frontendRoot, "dist-demo");
const requiredFiles = [
  "index.html",
  "runtime-config.json",
  "_headers",
  "public/js/boot.js",
  "demo/demoStore.js",
];

for (const filename of requiredFiles) {
  const metadata = await stat(path.join(output, filename)).catch(() => null);
  if (!metadata?.isFile()) throw new Error(`Static demo build is missing ${filename}`);
}

const runtimeConfig = JSON.parse(await readFile(path.join(output, "runtime-config.json"), "utf8"));
if (runtimeConfig.mode !== "demo") throw new Error("Static demo build does not select demo mode");

const headers = await readFile(path.join(output, "_headers"), "utf8");
for (const name of [
  "X-Content-Type-Options",
  "X-Frame-Options",
  "Referrer-Policy",
  "Cross-Origin-Opener-Policy",
  "Content-Security-Policy",
  "Permissions-Policy",
]) {
  if (!new RegExp(`^\\s+${name}:`, "m").test(headers)) {
    throw new Error(`Static demo deployment headers are missing ${name}`);
  }
}

if (await stat(path.join(output, "demo/_headers")).then(() => true, () => false)) {
  throw new Error("Static demo deployment headers must be at the output root");
}

console.log("Static demo build includes its entry point, demo adapter, and root security headers.");
