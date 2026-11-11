/**
 * @fileoverview Option groups that stack into one column when a label no longer fits its column.
 * Translations differ in length, so the fit is measured rather than set per language.
 */

import { LANGUAGE_CHANGE_EVENT } from "../i18n.js";

const STACKED_CLASS = "is-stacked";

/** Lays the options out in columns, then stacks them if any label overflows its column. */
export function fitOptionGrid(grid) {
  grid.classList.remove(STACKED_CLASS);
  const overflowing = [...grid.children].some((option) => option.scrollWidth > option.clientWidth + 1);
  grid.classList.toggle(STACKED_CLASS, overflowing);
}

/** Fits every `.filter-option-grid` now, when its width or text changes, and after a language change. */
export function bindOptionGrids(root = document) {
  const grids = [...root.querySelectorAll(".filter-option-grid")];
  const fitAll = () => grids.forEach(fitOptionGrid);
  if (typeof ResizeObserver !== "undefined") {
    const widths = new WeakMap();
    // Refitting changes the grid's height, so it waits a frame to stay outside this observer's delivery loop.
    const observer = new ResizeObserver((entries) => {
      entries.forEach(({ target, contentRect }) => {
        if (widths.get(target) === contentRect.width) return;
        widths.set(target, contentRect.width);
        requestAnimationFrame(() => fitOptionGrid(target));
      });
    });
    grids.forEach((grid) => observer.observe(grid));
  }
  if (typeof MutationObserver !== "undefined") {
    const observer = new MutationObserver(fitAll);
    grids.forEach((grid) => observer.observe(grid, { childList: true, characterData: true, subtree: true }));
  }
  window.addEventListener(LANGUAGE_CHANGE_EVENT, fitAll);
  fitAll();
}
