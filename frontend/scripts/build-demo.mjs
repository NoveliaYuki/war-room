import { createHash } from "node:crypto";
import { cp, mkdir, readdir, readFile, rm, writeFile } from "node:fs/promises";
import { resolve } from "node:path";

const root = resolve(import.meta.dirname, "..");
const output = resolve(root, "dist-demo");

async function collectJavaScriptFiles(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const files = await Promise.all(entries.map(async (entry) => {
    const path = resolve(directory, entry.name);
    if (entry.isDirectory()) return collectJavaScriptFiles(path);
    return entry.isFile() && entry.name.endsWith(".js") ? [path] : [];
  }));
  return files.flat();
}

await rm(output, { recursive: true, force: true });
await mkdir(output, { recursive: true });
const indexPath = resolve(output, "index.html");
const index = await readFile(resolve(root, "index.html"), "utf8");
await writeFile(indexPath, index.replace("<body>", '<body data-demo="true">'));
await cp(resolve(root, "public"), resolve(output, "public"), { recursive: true });
await cp(resolve(root, "demo"), resolve(output, "demo"), { recursive: true });
await rm(resolve(output, "demo/README.md"));

const javascriptFiles = await collectJavaScriptFiles(resolve(output, "public/js"));
javascriptFiles.push(...await collectJavaScriptFiles(resolve(output, "demo")));
const buildHash = createHash("sha256");
for (const path of javascriptFiles) {
  buildHash.update(path.slice(output.length));
  buildHash.update(await readFile(path));
}
const buildId = buildHash.digest("hex").slice(0, 12);

await Promise.all(javascriptFiles.map(async (path) => {
  const source = await readFile(path, "utf8");
  const versionedSource = source.replace(/(["'])(\.{1,2}\/|\/)([^"']+\.js)(["'])/g,
    (_match, quote, prefix, modulePath) => `${quote}${prefix}${modulePath}?v=${buildId}${quote}`);
  await writeFile(path, versionedSource);
}));

const bootPath = resolve(output, "public/js/boot.js");
const boot = (await readFile(bootPath, "utf8")).replace('new URLSearchParams(window.location.search).has("demo")', 'true');
await writeFile(bootPath, boot);

const generatedIndexPath = resolve(output, "index.html");
const generatedIndex = (await readFile(generatedIndexPath, "utf8"))
  .replace('<script type="module" src="/public/js/boot.js"></script>', `<script type="module" src="/public/js/boot.js?v=${buildId}"></script>`);
await writeFile(generatedIndexPath, generatedIndex);
