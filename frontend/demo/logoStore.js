const DEMO_COMPANY_LOGOS = new Map([
  ["google", "google-g.png"], ["openai", "openai.svg"], ["nvidia", "nvidia-eye.png"],
  ["apple", "apple.svg"], ["microsoft", "microsoft.svg"], ["datadog", "datadog.svg"],
  ["meta", "meta.svg"], ["stripe", "stripe.svg"], ["spotify", "spotify.svg"],
  ["northstar labs", "northstar-labs.svg"], ["cedar systems", "cedar-systems.svg"],
  ["bluebird analytics", "bluebird-analytics.svg"],
  ["morrow design co.", "morrow-design.svg"], ["cinder analytics", "cinder-analytics.svg"],
  ["saffron labs", "saffron-labs.svg"], ["kestrel mobility", "kestrel-mobility.svg"],
  ["lattice grove", "lattice-grove.svg"], ["orbit foundry", "orbit-foundry.svg"],
  ["stonebridge health", "stonebridge-health.svg"], ["redfern commerce", "redfern-commerce.svg"],
  ["silverpath energy", "silverpath-energy.svg"], ["atlas harbor", "atlas-harbor.svg"],
]);

export const DEMO_LOGOS_STORAGE_KEY = "war-room-demo-company-logos-v1";
let cachedDemoLogos;

/** Returns the bundled logo filename for a demo company. */
export function getDemoLogoAsset(companyName) {
  return DEMO_COMPANY_LOGOS.get(String(companyName || "").toLowerCase().trim()) || "";
}

/** Replaces the in-memory logo map after a backup import. */
export function cacheDemoLogos(logos) {
  cachedDemoLogos = logos;
}

/** Returns the browser-backed logo URL for a process. */
export function getDemoLogoUrl(companyName, jobId) {
  if (!cachedDemoLogos) {
    try {
      cachedDemoLogos = JSON.parse(window.localStorage.getItem(DEMO_LOGOS_STORAGE_KEY) || "{}");
    } catch {
      cachedDemoLogos = {};
    }
  }
  const importedLogo = cachedDemoLogos[jobId];
  if (typeof importedLogo === "string") return importedLogo;
  const asset = getDemoLogoAsset(companyName);
  return asset ? `/demo/assets/logos/${asset}` : "";
}
