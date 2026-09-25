import { describe, it, expect, vi, beforeEach } from "vitest";
import { parseSalaryInput, makeInlineEditable } from "../../public/js/inlineEdit.js";

describe("parseSalaryInput", () => {
  it("parses range formats and normalizes the bounds", () => {
    const expected = { salary_type: "limited", salary_min: 37000, salary_max: 83000 };
    expect(parseSalaryInput("€37k - €83k")).toEqual(expected);
    expect(parseSalaryInput("83,000 - 37,000")).toEqual(expected);
    expect(parseSalaryInput("$37,000–$83,000")).toEqual(expected);
  });
  it("parses single salary bounds", () => {
    expect(parseSalaryInput("up to $83k")).toEqual({ salary_type: "no_min", salary_min: null, salary_max: 83000 });
    expect(parseSalaryInput("from £37k")).toEqual({ salary_type: "no_max", salary_min: 37000, salary_max: null });
  });
  it("handles unknown salary values", () => {
    const unknown = { salary_type: "unknown", salary_min: null, salary_max: null };
    expect(parseSalaryInput("unknown")).toEqual(unknown);
    expect(parseSalaryInput("undisclosed")).toEqual(unknown);
    expect(parseSalaryInput("")).toEqual(unknown);
  });
});

  describe('makeInlineEditable', () => {
    let element, onSave;
    beforeEach(() => {
      element = document.createElement('div');
      element.textContent = 'text';
      document.body.appendChild(element);
      onSave = vi.fn();
    });

    it('double click triggers edit', () => {
      makeInlineEditable(element, { onSave });
      element.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
      const input = element.parentElement.querySelector('input');
      expect(input).not.toBeNull();
    });

    it('Enter saves', () => {
      makeInlineEditable(element, { onSave });
      element.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
      const input = element.parentElement.querySelector('input');
      input.value = 'new';
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }));
      expect(onSave).toHaveBeenCalledWith('new');
    });

    it('Escape cancels', () => {
      makeInlineEditable(element, { onSave });
      element.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
      const input = element.parentElement.querySelector('input');
      input.value = 'new';
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
      expect(onSave).not.toHaveBeenCalled();
      expect(element.parentElement.querySelector('input')).toBeNull();
    });

    it('blur saves', () => {
      makeInlineEditable(element, { onSave });
      element.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
      const input = element.parentElement.querySelector('input');
      input.value = 'new';
      input.dispatchEvent(new Event('blur'));
      expect(onSave).toHaveBeenCalledWith('new');
    });

    it('ignores missing targets and uses configured multiline values', () => {
      expect(makeInlineEditable(null, { onSave })).toBeUndefined();
      makeInlineEditable(element, {
        getValue: () => 'first\nsecond', multiline: true, rows: 5, maxLength: 50,
        placeholder: 'Enter notes', onSave,
      });
      element.dispatchEvent(new MouseEvent('dblclick', { bubbles: true, cancelable: true }));
      const textarea = element.parentElement.querySelector('textarea');
      expect(textarea.value).toBe('first\nsecond');
      expect(textarea.rows).toBe('5');
      expect(textarea.maxLength).toBe(50);
      expect(textarea.placeholder).toBe('Enter notes');
      textarea.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', shiftKey: true, bubbles: true }));
      expect(textarea.isConnected).toBe(true);
      textarea.value = 'first\nsecond edited';
      textarea.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
      expect(onSave).toHaveBeenCalledWith('first\nsecond edited');
    });

    it('applies formatDisplay and respects disabled optimistic updates', () => {
      const formatTarget = document.createElement('div');
      formatTarget.textContent = 'old'; document.body.append(formatTarget);
      makeInlineEditable(formatTarget, { onSave, formatDisplay: (value) => `<strong>${value}</strong>` });
      formatTarget.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
      const input = formatTarget.parentElement.querySelector('input');
      input.value = 'new'; input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }));
      expect(formatTarget.textContent).toBe('<strong>new</strong>');
      expect(formatTarget.querySelector('strong')).toBeNull();

      const disabled = document.createElement('div'); disabled.textContent = 'old'; document.body.append(disabled);
      makeInlineEditable(disabled, { onSave, updateText: false });
      disabled.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
      const secondInput = disabled.parentElement.querySelector('input');
      secondInput.value = 'new'; secondInput.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }));
      expect(disabled.textContent).toBe('old');
    });

    it('handles unchanged values, custom cancellation, and save failures', () => {
      const onCancel = vi.fn();
      makeInlineEditable(element, { onSave, onCancel });
      element.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
      element.parentElement.querySelector('input').dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }));
      expect(onSave).not.toHaveBeenCalled();
      expect(onCancel).toHaveBeenCalledOnce();

      const badTarget = document.createElement('div'); badTarget.textContent = 'old'; document.body.append(badTarget);
      const error = vi.spyOn(console, 'error').mockImplementation(() => {});
      makeInlineEditable(badTarget, { onSave: vi.fn(() => { throw new Error('save failed'); }) });
      badTarget.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
      const input = badTarget.parentElement.querySelector('input'); input.value = 'new';
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }));
      expect(error).toHaveBeenCalledOnce();
      error.mockRestore();
    });
  });
