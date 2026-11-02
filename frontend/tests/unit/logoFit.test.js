import { afterEach, beforeEach, describe, it, expect, vi } from 'vitest';
import { applyLogoFit, measureLogoFit, resetLogoFits } from '../../public/js/utils/logoFit.js';

const SIZE = 64;
const BLUE = [37, 99, 235, 255];
const WHITE = [255, 255, 255, 255];
const CLEAR = [0, 0, 0, 0];

/** Paints a box of one color over a background. */
function grid(box, color, background = CLEAR, width = SIZE, height = SIZE) {
  const [left, top, right, bottom] = box;
  const data = new Uint8ClampedArray(width * height * 4);
  for (let y = 0; y < height; y += 1) {
    for (let x = 0; x < width; x += 1) {
      const inside = x >= left && x <= right && y >= top && y <= bottom;
      data.set(inside ? color : background, (y * width + x) * 4);
    }
  }
  return data;
}

/** Paints a diamond, a shape no rectangle check can mistake for a tile. */
function diamond(color, background = CLEAR) {
  const data = new Uint8ClampedArray(SIZE * SIZE * 4);
  for (let y = 0; y < SIZE; y += 1) {
    for (let x = 0; x < SIZE; x += 1) {
      data.set(Math.abs(x - 32) + Math.abs(y - 32) <= 14 ? color : background, (y * SIZE + x) * 4);
    }
  }
  return data;
}

function stubCanvas(context, toDataURL = () => 'data:image/png;base64,trimmed') {
  const real = document.createElement.bind(document);
  vi.spyOn(document, 'createElement').mockImplementation((name) => (
    name === 'canvas' ? { getContext: () => context, toDataURL } : real(name)
  ));
}

const stubPixels = (data) => stubCanvas({ drawImage: vi.fn(), getImageData: () => ({ data }) });

const logo = (naturalWidth = 128, naturalHeight = 128, src = 'https://logo.example/a.png') => {
  const image = document.createElement('img');
  Object.defineProperties(image, { naturalWidth: { value: naturalWidth }, naturalHeight: { value: naturalHeight } });
  image.src = src;
  return image;
};

describe('logo fit', () => {
  beforeEach(() => resetLogoFits());
  afterEach(() => vi.restoreAllMocks());

  it('fills the avatar with a painted background', () => {
    stubPixels(grid([0, 0, 63, 63], BLUE));
    expect(measureLogoFit(logo())).toMatchObject({ kind: 'tile', fills: true, backing: '', crop: null });
  });

  it('trims the margin around a tile', () => {
    stubPixels(grid([10, 10, 53, 53], BLUE));
    const fit = measureLogoFit(logo());
    expect(fit).toMatchObject({ kind: 'tile', fills: true });
    expect(fit.crop.width).toBeCloseTo(44 / 64);
  });

  it('trims a white margin around a tile', () => {
    stubPixels(grid([10, 10, 53, 53], BLUE, WHITE));
    expect(measureLogoFit(logo())).toMatchObject({ kind: 'tile', fills: true });
  });

  it('keeps a non-square painted logo whole on its own color', () => {
    stubPixels(grid([0, 0, 63, 31], BLUE, CLEAR, 64, 32));
    expect(measureLogoFit(logo(600, 300))).toMatchObject({ kind: 'tile', fills: false, backing: 'rgb(37, 99, 235)' });
  });

  it('keeps a banner inset in a margin whole on its own color', () => {
    stubPixels(grid([0, 20, 63, 40], BLUE));
    expect(measureLogoFit(logo())).toMatchObject({ kind: 'tile', fills: false, backing: 'rgb(37, 99, 235)' });
  });

  it('keeps marks padded on white', () => {
    stubPixels(diamond(BLUE));
    expect(measureLogoFit(logo())).toMatchObject({ kind: 'mark', fills: false, backing: '' });
  });

  it('puts white marks on a dark backing', () => {
    stubPixels(diamond(WHITE));
    expect(measureLogoFit(logo())).toMatchObject({ kind: 'mark', backing: '#111827' });
  });

  it('keeps marks on a white image on white', () => {
    stubPixels(diamond(BLUE, WHITE));
    expect(measureLogoFit(logo())).toMatchObject({ kind: 'mark', backing: '' });
  });

  it.each([
    ['blank white', grid([1, 1, 0, 0], BLUE, WHITE)],
    ['blank transparent', grid([1, 1, 0, 0], BLUE)],
  ])('leaves %s logos uncropped', (_, data) => {
    stubPixels(data);
    expect(measureLogoFit(logo())).toMatchObject({ kind: 'mark', crop: null });
  });

  it('treats letters touching the image edge as a mark', () => {
    const data = grid([1, 1, 0, 0], BLUE);
    for (let x = 0; x < SIZE; x += 1) {
      for (let y = 0; y < SIZE; y += 1) {
        if (x % 8 < 5) data.set(BLUE, (y * SIZE + x) * 4);
      }
    }
    stubPixels(data);
    expect(measureLogoFit(logo())).toMatchObject({ kind: 'mark', fills: false });
  });

  it('treats a bold mark with gaps through its rim as a mark', () => {
    const data = grid([0, 0, 63, 63], BLUE, WHITE);
    for (let y = 0; y < SIZE; y += 1) {
      for (let x = 0; x < SIZE; x += 1) {
        if (x < 4 || x > 59 || y < 4 || y > 59) data.set(WHITE, (y * SIZE + x) * 4);
      }
    }
    for (let x = 0; x < 50; x += 1) {
      for (let y = 30; y < 34; y += 1) data.set(WHITE, (y * SIZE + x) * 4);
    }
    stubPixels(data);
    expect(measureLogoFit(logo())).toMatchObject({ kind: 'mark', fills: false });
  });

  it('rejects a mostly hollow shape with an unbroken rim', () => {
    const data = grid([4, 4, 59, 59], BLUE, WHITE);
    for (let y = 10; y < 54; y += 1) {
      for (let x = 10; x < 54; x += 1) {
        if (y % 8 < 7) data.set(WHITE, (y * SIZE + x) * 4);
      }
    }
    stubPixels(data);
    expect(measureLogoFit(logo())).toMatchObject({ kind: 'mark', fills: false });
  });

  it('treats a thin outline around white as a mark', () => {
    stubPixels(grid([3, 3, 60, 60], WHITE, BLUE));
    expect(measureLogoFit(logo())).toMatchObject({ kind: 'mark', crop: null });
  });

  it('applies the fit, swaps in the trimmed image, and measures each logo once', () => {
    const getImageData = vi.fn(() => ({ data: grid([10, 10, 53, 53], BLUE) }));
    stubCanvas({ drawImage: vi.fn(), getImageData });
    const image = logo();
    applyLogoFit(image);
    applyLogoFit(image);
    expect(image.classList.contains('company-logo-tile')).toBe(true);
    expect(image.classList.contains('company-logo-fills')).toBe(true);
    expect(image.getAttribute('src')).toContain('trimmed');
    expect(image.dataset.logoSource).toBe('https://logo.example/a.png');
    const again = logo();
    applyLogoFit(again);
    expect(again.getAttribute('src')).toContain('trimmed');
    expect(getImageData).toHaveBeenCalledTimes(1);
  });

  it('forgets the oldest logo once the memory is full', () => {
    const getImageData = vi.fn(() => ({ data: grid([0, 0, 63, 63], BLUE) }));
    stubCanvas({ drawImage: vi.fn(), getImageData });
    for (let index = 0; index <= 200; index += 1) applyLogoFit(logo(128, 128, `https://logo.example/${index}.png`));
    applyLogoFit(logo(128, 128, 'https://logo.example/0.png'));
    expect(getImageData).toHaveBeenCalledTimes(202);
  });

  it('applies a mark backing to the image', () => {
    stubPixels(diamond(WHITE));
    const image = logo();
    applyLogoFit(image);
    expect(image.classList.contains('company-logo-mark')).toBe(true);
    expect(image.style.getPropertyValue('--logo-backing')).toBe('#111827');
  });

  it('keeps the original image when it cannot be cropped', () => {
    stubCanvas({ drawImage: vi.fn(), getImageData: () => ({ data: grid([10, 10, 53, 53], BLUE) }) }, () => { throw new Error('tainted'); });
    const image = logo();
    applyLogoFit(image);
    expect(image.getAttribute('src')).toBe('https://logo.example/a.png');
    expect(image.classList.contains('company-logo-tile')).toBe(true);
  });

  it('leaves undecoded and unreadable logos in the default frame', () => {
    stubCanvas(null);
    const image = logo();
    applyLogoFit(image);
    expect(image.classList.contains('company-logo-tile')).toBe(false);
    expect(image.classList.contains('company-logo-mark')).toBe(false);
    expect(measureLogoFit(logo(0, 0))).toBeNull();
    vi.restoreAllMocks();
    stubCanvas({ drawImage: () => { throw new Error('tainted'); } });
    expect(measureLogoFit(logo())).toBeNull();
  });
});
