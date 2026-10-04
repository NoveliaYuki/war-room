const DEMO_COMPANY_LOGOS = new Map([
  ["google", "google-g.png"], ["openai", "openai.svg"], ["nvidia", "nvidia-eye.png"],
  ["apple", "apple.svg"], ["microsoft", "microsoft.svg"], ["datadog", "datadog.svg"],
  ["meta", "meta.svg"], ["stripe", "stripe.svg"], ["spotify", "spotify.svg"],
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
