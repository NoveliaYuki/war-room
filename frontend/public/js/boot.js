/**
 * @fileoverview Entry point that selects the demo or backend app mode.
 * Loaded as an external module to comply with the Content Security Policy.
 */

const configuredMode = await fetch("/runtime-config.json", { cache: "no-store" })
  .then((response) => response.ok ? response.json() : null)
  .catch(() => null);
const demo = new URLSearchParams(window.location.search).has("demo") || configuredMode?.mode === "demo";
document.body.dataset.demo = String(demo);

if (demo) {
  const [{ demoApi }, { useApiAdapter }] = await Promise.all([
    import("/demo/demoStore.js"),
    import("./api.js"),
  ]);
  useApiAdapter(demoApi);
}

await import("./app.js");
