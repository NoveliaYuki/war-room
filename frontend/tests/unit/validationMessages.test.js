import { afterEach, describe, expect, it, vi } from 'vitest';

/** Imports a fresh validation module with the given interface language saved. */
async function loadValidation(language) {
  vi.resetModules();
  window.localStorage.clear();
  window.localStorage.setItem('war-room.language', language);
  return import('../../public/js/utils/validationMessages.js');
}

/** Gives a field a fixed validity state; happy-dom does not evaluate constraints. */
function withValidity(field, state) {
  Object.defineProperty(field, 'validity', { configurable: true, get: () => ({ valid: false, ...state }) });
  return field;
}

afterEach(() => {
  document.body.innerHTML = '';
  window.localStorage.clear();
  vi.restoreAllMocks();
});

describe('validation messages', () => {
  it('words each validity problem in the selected language', async () => {
    const { validationMessage } = await loadValidation('es');
    const field = (html) => { document.body.innerHTML = html; return document.body.firstElementChild; };
    expect(validationMessage(withValidity(field('<input required>'), { valueMissing: true }))).toBe('Rellena este campo.');
    expect(validationMessage(withValidity(field('<select required></select>'), { valueMissing: true }))).toBe('Selecciona una opción.');
    expect(validationMessage(withValidity(field('<input type="url">'), { typeMismatch: true }))).toBe('Introduce una dirección web, como https://example.com.');
    expect(validationMessage(withValidity(field('<input type="email">'), { typeMismatch: true }))).toBe('Introduce una dirección de correo electrónico.');
    expect(validationMessage(withValidity(field('<input type="number">'), { badInput: true }))).toBe('Introduce un número.');
    expect(validationMessage(withValidity(field('<input type="date">'), { badInput: true }))).toBe('Introduce una fecha válida.');
    expect(validationMessage(withValidity(field('<input type="text">'), { badInput: true }))).toBe('Introduce un valor válido.');
    expect(validationMessage(withValidity(field('<input type="number" min="0">'), { rangeUnderflow: true }))).toBe('Introduce un valor igual o superior a 0.');
    expect(validationMessage(withValidity(field('<input type="number" max="200000">'), { rangeOverflow: true }))).toBe('Introduce un valor igual o inferior a 200.000.');
    expect(validationMessage(withValidity(field('<input type="number" step="1000">'), { stepMismatch: true }))).toBe('Introduce un múltiplo de 1000.');
    expect(validationMessage(withValidity(field('<input minlength="3">'), { tooShort: true }))).toBe('Usa al menos 3 caracteres.');
    expect(validationMessage(withValidity(field('<input maxlength="80">'), { tooLong: true }))).toBe('Usa 80 caracteres como máximo.');
    expect(validationMessage(withValidity(field('<input pattern="a+">'), { patternMismatch: true }))).toBe('Usa el formato indicado.');
    expect(validationMessage(withValidity(field('<input>'), {}))).toBe('Introduce un valor válido.');
    expect(validationMessage(withValidity(field('<input>'), { valid: true }))).toBe('');
  });

  it('sets the message when a field is reported and clears it before the next edit or submit', async () => {
    const { bindValidationMessages } = await loadValidation('ca');
    document.body.innerHTML = '<div id="root"><form><input name="title" required><button type="submit">Desa</button></form></div>';
    const input = withValidity(document.querySelector('input'), { valueMissing: true });
    const setCustomValidity = vi.spyOn(input, 'setCustomValidity');
    bindValidationMessages(document.querySelector('#root'));

    input.dispatchEvent(new Event('invalid'));
    expect(setCustomValidity).toHaveBeenLastCalledWith('Omple aquest camp.');
    input.dispatchEvent(new Event('input', { bubbles: true }));
    expect(setCustomValidity).toHaveBeenLastCalledWith('');

    setCustomValidity.mockClear();
    document.querySelector('button').dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
    // The click clears the old message first, so the browser validates the field afresh.
    expect(setCustomValidity.mock.calls[0]).toEqual(['']);

    setCustomValidity.mockClear();
    document.querySelector('form').dispatchEvent(new MouseEvent('click', { bubbles: true }));
    expect(setCustomValidity).not.toHaveBeenCalled();
  });

  it('leaves a valid field without a message', async () => {
    const { bindValidationMessages } = await loadValidation('en');
    document.body.innerHTML = '<div id="root"><input required></div>';
    const input = withValidity(document.querySelector('input'), { valid: true });
    const setCustomValidity = vi.spyOn(input, 'setCustomValidity');
    bindValidationMessages(document.querySelector('#root'));
    input.dispatchEvent(new Event('invalid'));
    input.dispatchEvent(new Event('change', { bubbles: true }));
    expect(setCustomValidity.mock.calls).toEqual([[''], ['']]);
  });
});
