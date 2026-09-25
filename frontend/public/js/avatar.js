import { escapeAttr } from "./utils/sanitize.js";

const DEMO_COMPANY_LOGOS = new Map([
  ["google", "google-g.png"],
  ["openai", "openai.svg"],
  ["factorial", "factorial-mark.png"],
  ["apple", "apple.svg"],
  ["microsoft", "microsoft.svg"],
  ["datadog", "datadog.svg"],
  ["meta", "meta.svg"],
  ["stripe", "stripe.svg"],
  ["spotify", "spotify.svg"],
]);

/**
 * @fileoverview Procedural Bauhaus/Geometric SVG Avatar Generator.
 * Creates deterministic, aesthetically pleasing vector avatars from string seeds.
 */

/**
 * Computes a 32-bit integer hash from any string seed.
 */
function hashString(str) {
  let hash = 0;
  for (let i = 0; i < str.length; i++) {
    const char = str.charCodeAt(i);
    hash = (hash << 5) - hash + char;
    hash |= 0;
  }
  return Math.abs(hash);
}

/**
 * Curated high-contrast aesthetic palettes.
 */
const PALETTES = [
  ["#3b82f6", "#1d4ed8", "#93c5fd", "#1e293b"], // Azure Blue
  ["#8b5cf6", "#6d28d9", "#c4b5fd", "#1e1b4b"], // Deep Violet
  ["#10b981", "#047857", "#a7f3d0", "#064e3b"], // Emerald
  ["#f59e0b", "#d97706", "#fde68a", "#451a03"], // Warm Amber
  ["#06b6d4", "#0891b2", "#a5f3fc", "#164e63"], // Cyan
  ["#ec4899", "#be185d", "#fbcfe8", "#500724"], // Rose Pink
  ["#6366f1", "#4338ca", "#c7d2fe", "#1e1b4b"], // Indigo
  ["#14b8a6", "#0f766e", "#99f6e4", "#134e4a"], // Teal
];

const BACKDROP_SHAPES = [
  (color) => `<polygon points="0,0 100,0 0,100" fill="${color}" opacity="0.85"/>`,
  (color) => `<circle cx="0" cy="0" r="80" fill="${color}" opacity="0.85"/>`,
  (color) => `<rect x="15" y="15" width="70" height="70" rx="16" fill="${color}" opacity="0.85"/>`,
  (color) => `<rect x="10" y="20" width="80" height="30" rx="15" fill="${color}" opacity="0.85"/>`,
];
const CONTRAST_SHAPES = [
  (color) => `<circle cx="50" cy="50" r="32" fill="none" stroke="${color}" stroke-width="12"/>`,
  (color) => `<circle cx="65" cy="65" r="28" fill="${color}" opacity="0.9"/>`,
  (color) => `<polygon points="50,15 85,80 15,80" fill="${color}" opacity="0.9"/>`,
  (color) => `<rect x="42" y="10" width="16" height="80" rx="8" fill="${color}"/>`,
];
const ACCENT_SHAPES = [
  (color) => `<circle cx="35" cy="35" r="12" fill="${color}"/>`,
  (color) => `<circle cx="50" cy="50" r="14" fill="${color}"/>`,
  (color) => `<rect x="58" y="20" width="20" height="20" rx="4" fill="${color}"/>`,
  (color) => `<circle cx="70" cy="30" r="10" fill="${color}"/>`,
];

/** Returns whether the shared UI is running with the browser-only demo adapter. */
function isDemoMode() {
  return document.body.dataset.demo === "true"
    || window.location.pathname.startsWith("/demo/")
    || new URLSearchParams(window.location.search).has("demo");
}

/** Renders a deterministic geometric primitive from a bounded set. */
function renderShape(variants, selector, color) {
  return variants[selector](color);
}

/**
 * Generates an SVG string representation of a deterministic geometric avatar.
 *
 * @param {string} seed - The seed string (e.g. company name or avatar_seed).
 * @param {number} size - Output viewBox dimensions (default 100).
 * @returns {string} Clean SVG markup.
 */
export function generateAvatarSvg(seed = "default", size = 100) {
  const hash = hashString(seed || "default");
  const palette = PALETTES[hash % PALETTES.length];

  const bg = palette[3];
  const primary = palette[0];
  const secondary = palette[1];
  const accent = palette[2];

  const shapesMarkup = [
    renderShape(BACKDROP_SHAPES, hash % BACKDROP_SHAPES.length, primary),
    renderShape(CONTRAST_SHAPES, (hash >> 3) % CONTRAST_SHAPES.length, secondary),
    renderShape(ACCENT_SHAPES, (hash >> 6) % ACCENT_SHAPES.length, accent),
  ].join("");

  return `
    <svg viewBox="0 0 ${size} ${size}" width="100%" height="100%" xmlns="http://www.w3.org/2000/svg">
      <defs>
        <clipPath id="avatar-clip-${hash}">
          <rect width="${size}" height="${size}" rx="16"/>
        </clipPath>
      </defs>
      <rect width="${size}" height="${size}" fill="${bg}"/>
      <g clip-path="url(#avatar-clip-${hash})">
        ${shapesMarkup}
      </g>
    </svg>
  `.trim();
}

/**
 * Renders an avatar element that automatically displays the company's real logo if available,
 * seamlessly falling back to the procedural geometric SVG avatar if unavailable or undisclosed.
 *
 * @param {string} companyName - Name of the company.
 * @param {string} seed - Seed for the procedural SVG fallback.
 * @param {number} size - Output pixel dimension (default 64).
 * @param {string} companyDomain - Optional explicit website domain.
 * @param {string} jobId - Optional job process ID.
 * @returns {string} Clean HTML markup string.
 */
export function renderCompanyAvatar(companyName = "Unknown", seed = "default", size = 64, companyDomain = "", jobId = "") {
  const isUnknown = !companyName || companyName.toLowerCase().trim() === "unknown";
  const fallbackSvg = generateAvatarSvg(seed || companyName || "Unknown", size);

  if (isUnknown) {
    return `<div class="avatar-svg-wrapper" style="width: 100%; height: 100%; border-radius: inherit;">${fallbackSvg}</div>`;
  }

  if (isDemoMode()) {
    const logo = DEMO_COMPANY_LOGOS.get(companyName.toLowerCase().trim());
    if (logo) {
      return `<div class="company-avatar-box" style="width: 100%; height: 100%; position: relative; border-radius: inherit; display: flex; align-items: center; justify-content: center; overflow: hidden; background: #fff;">
        <img src="/demo/assets/logos/${logo}" alt="${escapeAttr(companyName)} logo" class="company-logo-img" style="width: 100%; height: 100%; object-fit: contain; padding: 6px; border-radius: inherit;" loading="lazy" />
      </div>`;
    }
    return `<div class="avatar-svg-wrapper" style="width: 100%; height: 100%; border-radius: inherit;">${fallbackSvg}</div>`;
  }
  const logoUrl = `/api/company-logo?company=${encodeURIComponent(companyName)}&domain=${encodeURIComponent(companyDomain || '')}&job_id=${encodeURIComponent(jobId || '')}`;

  return `
    <div class="company-avatar-box" style="width: 100%; height: 100%; position: relative; border-radius: inherit; display: flex; align-items: center; justify-content: center; overflow: hidden;">
      <img src="${logoUrl}"
           alt="${escapeAttr(companyName)}"
           class="company-logo-img"
           style="width: 100%; height: 100%; object-fit: contain; padding: 6px; border-radius: inherit; background: rgba(255, 255, 255, 0.05);"
           loading="lazy"
           data-avatar-fallback="true" />
      <div class="company-avatar-fallback" style="display: none; width: 100%; height: 100%; border-radius: inherit;">
        ${fallbackSvg}
      </div>
    </div>
  `.trim();
}
