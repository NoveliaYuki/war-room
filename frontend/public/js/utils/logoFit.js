/**
 * @fileoverview Fits company logos to their avatar tile.
 *
 * Logos arrive as favicons, wordmarks, and app icons with no agreed framing, so
 * each one is read once and classified by its own backdrop:
 * - a tile (solid artwork, even when inset in a margin) fills the avatar;
 * - a mark (shape on transparent or white) is trimmed and stays padded, so
 *   `object-fit: contain` keeps it inside the avatar whatever its proportions.
 */

const SAMPLE_SIZE = 64;
const OUTPUT_SIZE = 256;
const FAINT_ALPHA = 24;
const VISIBLE_ALPHA = 128;
const SOLID_ALPHA = 200;
const NEAR_WHITE = 235;
const DOMINANT_SHARE = 0.5;
const EDGE_GAP_SHARE = 0.05;
const SOLID_EDGE_SHARE = 0.9;
const SOLID_BODY_SHARE = 0.5;
const BODY_INSET_SHARE = 0.08;
const SOLID_AREA_SHARE = 0.6;
const UNBROKEN_RIM_SHARE = 0.97;
const LIGHT_MARK_SHARE = 0.85;
const MAX_SQUARE_SKEW = 1.15;
const MIN_TRIM_SHARE = 0.03;
const MAX_REMEMBERED_LOGOS = 200;
const DARK_BACKING = "#111827";

const rememberedFits = new Map();

/** Reads a logo as a small RGBA grid that keeps its proportions, or null when unreadable. */
function readLogoGrid(image) {
  const ratio = SAMPLE_SIZE / Math.max(image.naturalWidth, image.naturalHeight);
  const width = Math.max(1, Math.round(image.naturalWidth * ratio));
  const height = Math.max(1, Math.round(image.naturalHeight * ratio));
  const canvas = document.createElement("canvas");
  canvas.width = width;
  canvas.height = height;
  const context = canvas.getContext("2d", { willReadFrequently: true });
  if (!context) return null;
  try {
    context.drawImage(image, 0, 0, width, height);
    return { width, height, data: context.getImageData(0, 0, width, height).data };
  } catch {
    return null;
  }
}

function pixelAt({ width, data }, x, y) {
  const offset = (y * width + x) * 4;
  return data.subarray(offset, offset + 4);
}

const isTransparent = (pixel) => pixel[3] < FAINT_ALPHA;
const isNearWhite = ([red, green, blue]) => Math.min(red, green, blue) >= NEAR_WHITE;
const isSolidArtwork = (pixel) => pixel[3] > SOLID_ALPHA && !isNearWhite(pixel);

function perimeterPixels(grid) {
  const pixels = [];
  for (let x = 0; x < grid.width; x += 1) pixels.push(pixelAt(grid, x, 0), pixelAt(grid, x, grid.height - 1));
  for (let y = 1; y < grid.height - 1; y += 1) pixels.push(pixelAt(grid, 0, y), pixelAt(grid, grid.width - 1, y));
  return pixels;
}

function averageColor(pixels) {
  const totals = [0, 0, 0];
  pixels.forEach((pixel) => totals.forEach((_, channel) => { totals[channel] += pixel[channel]; }));
  const [red, green, blue] = totals.map((total) => Math.round(total / (pixels.length || 1)));
  return `rgb(${red}, ${green}, ${blue})`;
}

/**
 * Names what surrounds the artwork: "transparent", "white", or "own" when the
 * logo's edges are its own painted background, which must never be trimmed.
 * Any gap in the edge rules out "own": rounded tiles and letters touching the
 * image bounds both leave gaps, and only shape analysis can tell them apart.
 */
function classifyBackdrop(grid) {
  const perimeter = perimeterPixels(grid);
  const painted = perimeter.filter((pixel) => !isTransparent(pixel));
  if (1 - painted.length / perimeter.length >= EDGE_GAP_SHARE) return { type: "transparent" };
  const isWhite = painted.filter(isNearWhite).length / perimeter.length >= DOMINANT_SHARE;
  return isWhite ? { type: "white" } : { type: "own", backingColor: averageColor(painted) };
}

/** Finds the box around pixels accepted by a predicate. */
function findBounds(grid, isContent) {
  const bounds = { left: grid.width, right: -1, top: grid.height, bottom: -1 };
  for (let y = 0; y < grid.height; y += 1) {
    for (let x = 0; x < grid.width; x += 1) {
      if (!isContent(pixelAt(grid, x, y))) continue;
      bounds.left = Math.min(bounds.left, x);
      bounds.right = Math.max(bounds.right, x);
      bounds.top = Math.min(bounds.top, y);
      bounds.bottom = Math.max(bounds.bottom, y);
    }
  }
  return bounds.right < 0 ? null : bounds;
}

function boundsSize({ left, right, top, bottom }) {
  return { width: right - left + 1, height: bottom - top + 1 };
}

function isNearlySquare({ width, height }) {
  return Math.max(width, height) / Math.min(width, height) <= MAX_SQUARE_SKEW;
}

/** Samples the middle of each side at an inset, so rounded corners do not count against a tile. */
function edgeSamples(grid, { left, right, top, bottom }, inset) {
  const quarterWidth = Math.floor((right - left) / 4);
  const quarterHeight = Math.floor((bottom - top) / 4);
  const insetTop = Math.min(top + inset, bottom);
  const insetBottom = Math.max(bottom - inset, top);
  const insetLeft = Math.min(left + inset, right);
  const insetRight = Math.max(right - inset, left);
  const samples = [];
  for (let x = left + quarterWidth; x <= right - quarterWidth; x += 1) {
    samples.push(pixelAt(grid, x, insetTop), pixelAt(grid, x, insetBottom));
  }
  for (let y = top + quarterHeight; y <= bottom - quarterHeight; y += 1) {
    samples.push(pixelAt(grid, insetLeft, y), pixelAt(grid, insetRight, y));
  }
  return samples;
}

const solidShare = (samples) => samples.filter(isSolidArtwork).length / samples.length;

/** Requires solid artwork past the rim, so an outlined frame is not mistaken for a tile. */
function isSolidTile(grid, bounds) {
  const { width, height } = boundsSize(bounds);
  const bodyInset = Math.max(1, Math.round(Math.min(width, height) * BODY_INSET_SHARE));
  return solidShare(edgeSamples(grid, bounds, 1)) >= SOLID_EDGE_SHARE
    && solidShare(edgeSamples(grid, bounds, bodyInset)) >= SOLID_BODY_SHARE;
}

/**
 * Separates a filled tile from a bold mark whose strokes merely reach its
 * bounds: the backdrop shows through a mark's gaps and cuts across its rim.
 */
function isFilledShape(grid, bounds) {
  if (solidShare(edgeSamples(grid, bounds, 1)) < UNBROKEN_RIM_SHARE) return false;
  const { left, right, top, bottom } = bounds;
  let solid = 0;
  for (let y = top; y <= bottom; y += 1) {
    for (let x = left; x <= right; x += 1) {
      if (isSolidArtwork(pixelAt(grid, x, y))) solid += 1;
    }
  }
  const { width, height } = boundsSize(bounds);
  return solid / (width * height) >= SOLID_AREA_SHARE;
}

/** Picks a backing that keeps a mark visible: white marks vanish on the default white tile. */
function markBacking(grid, bounds, backdrop) {
  if (backdrop.type === "white") return "";
  const visible = [];
  for (let y = bounds.top; y <= bounds.bottom; y += 1) {
    for (let x = bounds.left; x <= bounds.right; x += 1) {
      const pixel = pixelAt(grid, x, y);
      if (pixel[3] >= VISIBLE_ALPHA) visible.push(pixel);
    }
  }
  return visible.filter(isNearWhite).length / (visible.length || 1) >= LIGHT_MARK_SHARE ? DARK_BACKING : "";
}

function ownBackgroundFit(grid, backdrop) {
  const whole = { left: 0, right: grid.width - 1, top: 0, bottom: grid.height - 1 };
  if (!isSolidTile(grid, whole)) return { kind: "mark", fills: false, backing: "", bounds: null };
  const fills = isNearlySquare(grid);
  return { kind: "tile", fills, backing: fills ? "" : backdrop.backingColor, bounds: null };
}

/** Classifies artwork on a removable backdrop as a solid tile or a free-standing mark. */
function backdropFit(grid, backdrop) {
  const isFaintContent = backdrop.type === "white"
    ? (pixel) => pixel[3] >= FAINT_ALPHA && !isNearWhite(pixel)
    : (pixel) => pixel[3] >= FAINT_ALPHA;
  const isSolidContent = backdrop.type === "white"
    ? isFaintContent
    : (pixel) => pixel[3] >= VISIBLE_ALPHA;
  const solidBounds = findBounds(grid, isSolidContent);
  if (!solidBounds) return { kind: "mark", fills: false, backing: "", bounds: null };
  if (isSolidTile(grid, solidBounds) && isFilledShape(grid, solidBounds)) {
    const fills = isNearlySquare(boundsSize(solidBounds));
    const backing = fills ? "" : averageColor(edgeSamples(grid, solidBounds, 1));
    return { kind: "tile", fills, backing, bounds: solidBounds };
  }
  const bounds = findBounds(grid, isFaintContent);
  return { kind: "mark", fills: false, backing: markBacking(grid, solidBounds, backdrop), bounds };
}

/**
 * Measures how a loaded logo should sit in its avatar.
 *
 * @param {HTMLImageElement} image - A loaded logo.
 * @returns {{kind: string, fills: boolean, backing: string, crop: ?object}|null}
 *   The fit, with `crop` in fractions of the image, or null when unreadable.
 */
export function measureLogoFit(image) {
  if (!image.naturalWidth || !image.naturalHeight) return null;
  const grid = readLogoGrid(image);
  if (!grid) return null;
  const backdrop = classifyBackdrop(grid);
  const { bounds, ...fit } = backdrop.type === "own" ? ownBackgroundFit(grid, backdrop) : backdropFit(grid, backdrop);
  return { ...fit, crop: bounds ? cropFractions(grid, bounds) : null };
}

/** Converts sample bounds to image fractions, dropping trims too small to matter. */
function cropFractions(grid, bounds) {
  const left = bounds.left / grid.width;
  const top = bounds.top / grid.height;
  const right = (bounds.right + 1) / grid.width;
  const bottom = (bounds.bottom + 1) / grid.height;
  const trimmed = Math.max(left, top, 1 - right, 1 - bottom);
  return trimmed < MIN_TRIM_SHARE ? null : { left, top, width: right - left, height: bottom - top };
}

/**
 * Redraws a logo without its margin, or returns null when the browser refuses.
 * The whole image is drawn at an offset because SVG source rectangles are unreliable.
 */
function cropLogo(image, crop) {
  const ratio = OUTPUT_SIZE / Math.max(crop.width * image.naturalWidth, crop.height * image.naturalHeight);
  const drawnWidth = image.naturalWidth * ratio;
  const drawnHeight = image.naturalHeight * ratio;
  const canvas = document.createElement("canvas");
  canvas.width = Math.max(1, Math.round(crop.width * drawnWidth));
  canvas.height = Math.max(1, Math.round(crop.height * drawnHeight));
  try {
    canvas.getContext("2d").drawImage(image, -crop.left * drawnWidth, -crop.top * drawnHeight, drawnWidth, drawnHeight);
    return canvas.toDataURL("image/png");
  } catch {
    return null;
  }
}

function rememberFit(source, fit) {
  if (rememberedFits.size >= MAX_REMEMBERED_LOGOS) rememberedFits.delete(rememberedFits.keys().next().value);
  rememberedFits.set(source, fit);
}

/** Returns a logo's fit, measuring each source once across avatar re-renders. */
function resolveLogoFit(image) {
  const source = image.currentSrc || image.src;
  if (rememberedFits.has(source)) return rememberedFits.get(source);
  const fit = measureLogoFit(image);
  if (fit) rememberFit(source, { ...fit, croppedSrc: fit.crop ? cropLogo(image, fit.crop) : null });
  return rememberedFits.get(source) || null;
}

/**
 * Applies a logo's fit once; the cropped redraw fires `load` again and is ignored.
 *
 * @param {HTMLImageElement} image - A loaded logo.
 */
export function applyLogoFit(image) {
  if (image.dataset.logoFitted) return;
  image.dataset.logoFitted = "true";
  const fit = resolveLogoFit(image);
  if (!fit) return;
  image.classList.add(fit.kind === "tile" ? "company-logo-tile" : "company-logo-mark");
  image.classList.toggle("company-logo-fills", fit.fills);
  if (fit.backing) image.style.setProperty("--logo-backing", fit.backing);
  if (!fit.croppedSrc) return;
  image.dataset.logoSource = image.getAttribute("src");
  image.src = fit.croppedSrc;
}

/** Forgets measured logos. */
export function resetLogoFits() {
  rememberedFits.clear();
}
