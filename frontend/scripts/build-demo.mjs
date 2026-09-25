import { cp, mkdir, rm } from "node:fs/promises";
import { resolve } from "node:path";
import { readFile, writeFile } from "node:fs/promises";

const root = resolve(import.meta.dirname, "..");
const output = resolve(root, "dist-demo");

await rm(output, { recursive: true, force: true });
await mkdir(output, { recursive: true });
await cp(resolve(root, "index.html"), resolve(output, "index.html"));
await cp(resolve(root, "public"), resolve(output, "public"), { recursive: true });
await cp(resolve(root, "demo"), resolve(output, "demo"), { recursive: true });
await rm(resolve(output, "demo/README.md"));
const bootPath = resolve(output, "public/js/boot.js");
const boot = (await readFile(bootPath, "utf8")).replace('new URLSearchParams(window.location.search).has("demo")', 'true');
await writeFile(bootPath, boot);
