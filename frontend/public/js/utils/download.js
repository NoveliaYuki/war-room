/**
 * @fileoverview Saves generated files through the browser's download flow.
 */

/**
 * Saves a blob through a temporary download link.
 *
 * @param {Blob} blob - File contents.
 * @param {string} filename - Suggested download filename.
 */
export function saveBlob(blob, filename) {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 1000);
}
