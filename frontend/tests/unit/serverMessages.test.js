import { afterEach, describe, expect, it, vi } from 'vitest';

/** Imports a fresh message module with the given interface language saved. */
async function loadMessages(language) {
  vi.resetModules();
  window.localStorage.clear();
  window.localStorage.setItem('war-room.language', language);
  return import('../../public/js/utils/serverMessages.js');
}

afterEach(() => {
  window.localStorage.clear();
  vi.restoreAllMocks();
});

describe('server messages', () => {
  it('translates known messages into the selected language', async () => {
    const { serverMessage } = await loadMessages('es');
    expect(serverMessage('Stage not found', 'fallback')).toBe('Esta fase ya no existe.');
    expect(serverMessage('Technology is assigned to one or more processes; remove its assignments first', 'fallback'))
      .toBe('Esta tecnología se usa en uno o más procesos. Quítala de ellos primero.');
    expect(serverMessage('That site answered with status 503.', 'fallback')).toBe('Ese sitio ha respondido con el estado 503.');
    expect(serverMessage("This posting's closing date has passed.", 'fallback')).toBe('La fecha de cierre de esta oferta ya ha pasado.');
  });

  it('translates invalid-field details, with a general message for unlisted ones', async () => {
    const { serverMessage } = await loadMessages('ca');
    expect(serverMessage('request contains an invalid field value: technology name or alias already exists', 'fallback'))
      .toBe('Ja existeix una tecnologia amb aquest nom o àlies.');
    expect(serverMessage('request contains an invalid field value: applied_date must use YYYY-MM-DD', 'fallback'))
      .toBe('Introdueix una data vàlida.');
    expect(serverMessage('request contains an invalid field value: invalid status "x"', 'fallback'))
      .toBe("Un dels valors no és vàlid. Revisa el formulari i torna-ho a provar.");
  });

  it('shows the fallback for unknown messages outside English', async () => {
    const { serverMessage } = await loadMessages('es');
    expect(serverMessage('Some new server message', 'No se ha podido guardar')).toBe('No se ha podido guardar');
    expect(serverMessage('', 'No se ha podido guardar')).toBe('No se ha podido guardar');
    expect(serverMessage(undefined, 'No se ha podido guardar')).toBe('No se ha podido guardar');
  });

  it('keeps unknown English messages as sent', async () => {
    const { serverMessage } = await loadMessages('en');
    expect(serverMessage('Some new server message', 'Failed to save')).toBe('Some new server message');
    expect(serverMessage('sign in required', 'Failed to save')).toBe('Sign in to continue.');
  });
});
