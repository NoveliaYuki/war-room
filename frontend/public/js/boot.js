/**
 * @fileoverview Entry point that selects the demo or backend app mode.
 * Loaded as an external module to comply with the Content Security Policy.
 */

const demo = new URLSearchParams(window.location.search).has("demo");

if (demo) {
  await import("/demo/app.js");
} else {
  await import("./app.js");
}
