import { afterEach, describe, expect, it, vi } from 'vitest';
import { activateModal, focusModalContents, restoreModalFocus, trapModalTab } from '../../public/js/modalA11y.js';

describe('modal accessibility helpers', () => {
  afterEach(() => {
    document.body.innerHTML = '';
    vi.restoreAllMocks();
  });

  it('names the dialog, focuses the requested control, traps tab, and restores focus', () => {
    document.body.innerHTML = `
      <main class="app-container" aria-hidden="false"><button id="trigger">Open</button></main>
      <div id="modal" role="dialog" tabindex="-1">
        <h2 class="modal-title">Details</h2>
        <button id="first">First</button><button id="last">Last</button>
      </div>`;
    vi.spyOn(HTMLElement.prototype, 'getClientRects').mockReturnValue([{}]);
    const modal = document.querySelector('#modal');
    const trigger = document.querySelector('#trigger');
    trigger.focus();

    activateModal(modal, '#first');
    expect(modal.getAttribute('aria-labelledby')).toBe('war-room-modal-title');
    expect(document.activeElement).toBe(document.querySelector('#first'));
    expect(document.querySelector('.app-container').inert).toBe(true);
    expect(document.querySelector('.app-container').getAttribute('aria-hidden')).toBe('true');

    const shiftTab = new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, cancelable: true });
    expect(trapModalTab(shiftTab, modal)).toBe(true);
    expect(shiftTab.defaultPrevented).toBe(true);
    expect(document.activeElement).toBe(document.querySelector('#last'));

    const innerShiftTab = new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, cancelable: true });
    trapModalTab(innerShiftTab, modal);
    expect(innerShiftTab.defaultPrevented).toBe(false);

    const tab = new KeyboardEvent('keydown', { key: 'Tab', cancelable: true });
    trapModalTab(tab, modal);
    expect(document.activeElement).toBe(document.querySelector('#first'));
    const innerTab = new KeyboardEvent('keydown', { key: 'Tab', cancelable: true });
    trapModalTab(innerTab, modal);
    expect(innerTab.defaultPrevented).toBe(false);
    expect(trapModalTab(new KeyboardEvent('keydown', { key: 'Escape' }), modal)).toBe(false);

    restoreModalFocus();
    expect(document.activeElement).toBe(trigger);
    expect(document.querySelector('.app-container').inert).toBe(false);
    expect(document.querySelector('.app-container').getAttribute('aria-hidden')).toBe('false');
  });

  it('uses a fallback name and focus target for dialogs without a title', () => {
    document.body.innerHTML = '<div id="modal" role="dialog" tabindex="-1"><button id="first">Continue</button></div>';
    vi.spyOn(HTMLElement.prototype, 'getClientRects').mockReturnValue([{}]);
    const modal = document.querySelector('#modal');
    focusModalContents(modal);
    expect(modal.getAttribute('aria-label')).toBe('Dialog');
    expect(document.activeElement).toBe(document.querySelector('#first'));
  });

  it('keeps focus in an empty dialog and ignores a removed return target', () => {
    document.body.innerHTML = '<main class="app-container"><button id="trigger">Open</button></main><div id="modal" tabindex="-1"></div>';
    vi.spyOn(HTMLElement.prototype, 'getClientRects').mockReturnValue([]);
    const modal = document.querySelector('#modal');
    const trigger = document.querySelector('#trigger');
    trigger.focus();
    activateModal(modal, '#missing');
    expect(document.activeElement).toBe(modal);
    const tab = new KeyboardEvent('keydown', { key: 'Tab', cancelable: true });
    expect(trapModalTab(tab, modal)).toBe(true);
    expect(tab.defaultPrevented).toBe(true);
    expect(document.activeElement).toBe(modal);
    trigger.remove();
    restoreModalFocus();
  });

  it('preserves the original page state when a dialog opens again before closing', () => {
    document.body.innerHTML = `
      <main class="app-container"><button id="trigger">Open</button></main>
      <div id="modal" role="dialog" tabindex="-1"><h2 class="modal-title">Dialog</h2>
        <button id="first">First</button><button id="last">Last</button>
      </div>`;
    const modal = document.querySelector('#modal');
    const background = document.querySelector('.app-container');
    const trigger = document.querySelector('#trigger');
    trigger.focus();

    activateModal(modal, '#first');
    activateModal(modal, '#last');
    expect(background.inert).toBe(true);
    expect(document.activeElement).toBe(document.querySelector('#last'));

    restoreModalFocus();
    expect(background.inert).toBe(false);
    expect(background.hasAttribute('aria-hidden')).toBe(false);
    expect(document.activeElement).toBe(trigger);
  });

});
