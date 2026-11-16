import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

let observers;

const nextFrame = () => new Promise((resolve) => requestAnimationFrame(resolve));

/** Gives an option a measured width; happy-dom has no layout of its own. */
function setOptionWidth(option, scrollWidth, clientWidth = 100) {
  Object.defineProperty(option, 'scrollWidth', { configurable: true, get: () => scrollWidth });
  Object.defineProperty(option, 'clientWidth', { configurable: true, get: () => clientWidth });
}

beforeEach(() => {
  vi.resetModules();
  observers = [];
  vi.stubGlobal('ResizeObserver', class {
    constructor(callback) { this.callback = callback; this.targets = []; observers.push(this); }
    observe(target) { this.targets.push(target); }
  });
  document.body.innerHTML = `
    <div class="filter-option-grid" id="grid">
      <label id="first"><span>Any</span></label>
      <label id="second"><span>Direct referral</span><span class="tab-count">3</span></label>
    </div>`;
  setOptionWidth(document.querySelector('#first'), 60);
  setOptionWidth(document.querySelector('#second'), 90);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('option grids', () => {
  it('keeps columns while every label fits and stacks when one overflows', async () => {
    const { fitOptionGrid } = await import('../../public/js/components/optionGrid.js');
    const grid = document.querySelector('#grid');
    fitOptionGrid(grid);
    expect(grid.classList.contains('is-stacked')).toBe(false);
    setOptionWidth(document.querySelector('#second'), 140);
    fitOptionGrid(grid);
    expect(grid.classList.contains('is-stacked')).toBe(true);
    setOptionWidth(document.querySelector('#second'), 101);
    fitOptionGrid(grid);
    expect(grid.classList.contains('is-stacked')).toBe(false);
  });

  it('refits when the width changes, ignoring repeated widths', async () => {
    const { bindOptionGrids } = await import('../../public/js/components/optionGrid.js');
    const grid = document.querySelector('#grid');
    bindOptionGrids();
    expect(observers[0].targets).toEqual([grid]);
    setOptionWidth(document.querySelector('#second'), 140);
    observers[0].callback([{ target: grid, contentRect: { width: 300 } }]);
    expect(grid.classList.contains('is-stacked')).toBe(false);
    await nextFrame();
    expect(grid.classList.contains('is-stacked')).toBe(true);
    setOptionWidth(document.querySelector('#second'), 90);
    observers[0].callback([{ target: grid, contentRect: { width: 300 } }]);
    await nextFrame();
    expect(grid.classList.contains('is-stacked')).toBe(true);
    observers[0].callback([{ target: grid, contentRect: { width: 420 } }]);
    await nextFrame();
    expect(grid.classList.contains('is-stacked')).toBe(false);
  });

  it('refits after a language change and when option text changes', async () => {
    const { bindOptionGrids } = await import('../../public/js/components/optionGrid.js');
    const { LANGUAGE_CHANGE_EVENT } = await import('../../public/js/i18n.js');
    const grid = document.querySelector('#grid');
    bindOptionGrids();
    setOptionWidth(document.querySelector('#second'), 140);
    window.dispatchEvent(new CustomEvent(LANGUAGE_CHANGE_EVENT));
    expect(grid.classList.contains('is-stacked')).toBe(true);
    setOptionWidth(document.querySelector('#second'), 90);
    document.querySelector('#second .tab-count').textContent = '12';
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(grid.classList.contains('is-stacked')).toBe(false);
  });

  it('still fits once when resize observation is unavailable', async () => {
    vi.stubGlobal('ResizeObserver', undefined);
    vi.stubGlobal('MutationObserver', undefined);
    setOptionWidth(document.querySelector('#second'), 140);
    const { bindOptionGrids } = await import('../../public/js/components/optionGrid.js');
    bindOptionGrids();
    expect(document.querySelector('#grid').classList.contains('is-stacked')).toBe(true);
  });
});
