import { cp, mkdir, rm, writeFile } from "node:fs/promises";
import { resolve } from "node:path";

const root = resolve(import.meta.dirname, "..");
const output = resolve(root, "dist-demo");

await rm(output, { recursive: true, force: true });
await mkdir(output, { recursive: true });
await cp(resolve(root, "index.html"), resolve(output, "index.html"));
await cp(resolve(root, "public"), resolve(output, "public"), { recursive: true });
await cp(resolve(root, "demo"), resolve(output, "demo"), { recursive: true });
await rm(resolve(output, "demo/README.md"));
await cp(resolve(root, "demo/_headers"), resolve(output, "_headers"));
await rm(resolve(output, "demo/_headers"));
await writeFile(resolve(output, "runtime-config.json"), `${JSON.stringify({ mode: "demo" }, null, 2)}\n`);
