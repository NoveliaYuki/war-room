import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({
  api: {
    updateJob: vi.fn(),
    deleteJob: vi.fn(),
    reorderJobs: vi.fn(),
  },
  openWithFlip: vi.fn(),
  openDetailModal: vi.fn(),
  inlineEditors: [],
  toast: vi.fn(),
}));

vi.mock('../../public/js/api.js', () => ({ api: mocks.api }));
vi.mock('../../public/js/flip.js', () => ({ openWithFlip: mocks.openWithFlip }));
vi.mock('../../public/js/components/detailModal.js', () => ({ openDetailModal: mocks.openDetailModal }));
vi.mock('../../public/js/utils/toast.js', () => ({ showToast: mocks.toast }));
vi.mock('../../public/js/inlineEdit.js', () => ({
  makeInlineEditable: (element, options) => mocks.inlineEditors.push({ element, options }),
  parseSalaryInput: (value) => ({ salary_input: value }),
}));

import { formatSalaryShort, renderCardGrid } from '../../public/js/components/cardGrid.js';

const makeJob = (overrides = {}) => ({
  id: 'job-1',
  company_name: 'Acme',
  avatar_seed: 'acme',
  company_domain: 'acme.test',
  position_title: 'Engineer',
  status: 'active',
  salary_type: 'limited',
  salary_min: 37000,
  salary_max: 90000,
  salary_currency: 'EUR',
  is_referral: false,
  recruiter_type: 'none',
  keyword_note: 'Platform',
  current_stage_title: '',
  current_stage_index: 0,
  total_stages_count: 0,
  ...overrides,
});

function setup(jobs = [makeJob()], options = {}) {
  const container = document.createElement('main');
  const modal = document.createElement('section');
  const backdrop = document.createElement('div');
  modal.id = 'detail-modal';
  backdrop.id = 'modal-backdrop';
  document.body.append(container, modal, backdrop);
  renderCardGrid(
    container,
    jobs,
    Object.hasOwn(options, 'modal') ? options.modal : modal,
    Object.hasOwn(options, 'backdrop') ? options.backdrop : backdrop,
    options.refresh ?? null,
  );
  return { container, card: container.querySelector('.process-card'), modal, backdrop };
}

function pointerEvent(type, { x = 10, y = 10, button = 0, target } = {}) {
  const event = new Event(type, { bubbles: true, cancelable: true });
  Object.defineProperties(event, {
    clientX: { value: x },
    clientY: { value: y },
    button: { value: button },
  });
  if (target) target.dispatchEvent(event);
  return event;
}

describe('cardGrid', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
    mocks.api.updateJob.mockReset();
    mocks.api.deleteJob.mockReset();
    mocks.api.reorderJobs.mockReset();
    mocks.openWithFlip.mockReset();
    mocks.openDetailModal.mockReset();
    mocks.toast.mockReset();
    mocks.inlineEditors.length = 0;
    vi.useRealTimers();
    window.showToast = undefined;
    vi.stubGlobal('confirm', vi.fn(() => true));
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    delete window.showToast;
  });

  it("renders employment type from each job record", () => {
    const container = document.createElement("div");
    const jobs = [
      makeJob({ id: "job-1", company_name: "Example Company One", employment_type: "permanent_b2b" }),
      makeJob({ id: "job-2", company_name: "Example Company Two", employment_type: "permanent" }),
      makeJob({ id: "job-3", company_name: "Example Company Three", employment_type: "b2b" }),
    ];
    renderCardGrid(container, jobs);

    expect([...container.querySelectorAll(".employment-type-tag")].map((tag) => tag.textContent)).toEqual([
      "Permanent / B2B", "Permanent", "B2B",
    ]);
  });
  it("does not add an employment type tag when the type is unknown", () => {
    const { card } = setup([makeJob({ employment_type: "unknown" })]);
    expect(card.querySelector(".employment-type-tag")).toBeNull();
  });
  it('shows known work arrangements on preview cards and omits unknown values', () => {
    const container = document.createElement('main');
    renderCardGrid(container, [
      makeJob({ id: 'remote', work_arrangement: 'remote', keyword_note: 'Remote' }),
      makeJob({ id: 'hybrid', work_arrangement: 'hybrid' }),
      makeJob({ id: 'onsite', work_arrangement: 'on_site' }),
      makeJob({ id: 'unknown', work_arrangement: 'unknown' }),
    ]);

    expect([...container.querySelectorAll('.work-arrangement-tag')].map((tag) => tag.textContent)).toEqual([
      'Remote', 'Hybrid', 'On-site',
    ]);
    expect(container.querySelector('[data-id="unknown"] .work-arrangement-tag')).toBeNull();
    expect(container.querySelector('[data-id="remote"] .card-summary .keyword-note').textContent).toBe('No role highlights added');
  });

  describe("salary labels", () => {
    it("formats ranges and bounds", () => {
      expect(formatSalaryShort("limited", 37000, 83000, "EUR")).toBe("€37k-83k");
      expect(formatSalaryShort("limited", 37000, 83000, "USD")).toBe("$37k-83k");
      expect(formatSalaryShort("limited", 37000, 83000, "GBP")).toBe("£37k-83k");
      expect(formatSalaryShort("no_min", null, 83000, "USD")).toBe("Up to $83k");
      expect(formatSalaryShort("no_max", 37000, null, "USD")).toBe("From $37k");
    });
    it("handles undisclosed and equal amounts", () => {
      expect(formatSalaryShort("unknown", null, null, "USD")).toBe("Salary undisclosed");
      expect(formatSalaryShort("limited", 37000, 37000, "USD")).toBe("$37k");
      expect(formatSalaryShort("limited", 900, 900, "USD")).toBe("$900");
      expect(formatSalaryShort("limited", 1000, 1000, "USD")).toBe("$1k");
      expect(formatSalaryShort("limited", 1500, 1500, "USD")).toBe("$1.5k");
    });
  });

  it('renders the empty state and quick create action', () => {
    const { container } = setup([]);
    expect(container.querySelector('.empty-state').textContent).toContain('No selection processes found');
    expect(container.querySelector('.btn-create-first-process').getAttribute('onclick')).toBeNull();
  });

  it("removes duplicated metadata from role highlights", () => {
    const { card } = setup([makeJob({
      company_name: "Example Company",
      recruiter_name: "Casey Example",
      keyword_note: "Example Company • Casey Example • Platform security • Spain • Permanent",
    })]);

    expect(card.querySelector(".card-summary").textContent).toContain("Platform security • Spain");
    expect(card.querySelector(".card-summary").textContent).not.toContain("Example Company");
    expect(card.querySelector(".card-summary").textContent).not.toContain("Casey Example");
  });
  it('does not repeat referral in the card summary', () => {
    const { card } = setup([makeJob({ is_referral: true, keyword_note: 'Referral' })]);
    expect(card.querySelector('.referral-tag').textContent).toContain('Referral');
    expect(card.querySelector('.keyword-note').textContent).toContain('No role highlights added');
  });

  it('renders the count stage label without showing a job-level recruiter', () => {
    const { card } = setup([makeJob({ total_stages_count: 2, recruiter_type: 'internal' })]);
    expect(card.querySelector('.card-stage-indicator').textContent).toContain('2 interview stages');
    expect(card.querySelector('.recruiter-badge')).toBeNull();
  });

  it('uses an icon-only details button with an accessible label', () => {
    const { card } = setup();
    const openButton = card.querySelector('.btn-card-open-details');

    expect(openButton.textContent.trim()).toBe('');
    expect(openButton.getAttribute('aria-label')).toBe('Open details for Engineer');
    openButton.click();
    expect(mocks.openDetailModal).toHaveBeenCalledOnce();
  });

  it('keeps role highlights at the top and stage navigation in the bottom footer', () => {
    const { card } = setup();
    const content = card.querySelector('.card-content');
    const footer = card.querySelector('.card-footer');

    expect(content.querySelector('.card-summary')).not.toBeNull();
    expect(content.querySelector('.card-stage-indicator')).toBeNull();
    expect(footer.querySelector('.card-stage-indicator')).not.toBeNull();
    expect(footer.querySelector('.keyword-note')).toBeNull();
  });

  it('skips optional editors when their editable elements are absent', () => {
    const originalQuery = Element.prototype.querySelector;
    for (const missingSelector of [
      '.editable-card-company', '.editable-card-position', '.editable-card-salary',
      '.editable-card-keyword', '.editable-card-referral',
    ]) {
      const query = vi.spyOn(Element.prototype, 'querySelector').mockImplementation(function (selector) {
        if (this.classList?.contains('process-card') && selector === missingSelector) return null;
        return originalQuery.call(this, selector);
      });
      const container = document.createElement('main');
      renderCardGrid(container, [makeJob()]);
      expect(container.querySelector('.process-card')).not.toBeNull();
      query.mockRestore();
    }
  });

  it('wires all inline saves and refreshes only when appropriate', async () => {
    const refresh = vi.fn();
    setup([makeJob()], { refresh });
    const [company, position, salary, keyword] = mocks.inlineEditors;
    await company.options.onSave('');
    await position.options.onSave('');
    await position.options.onSave('Architect');
    await salary.options.onSave('75k');
    await keyword.options.onSave('Go');
    expect(mocks.api.updateJob.mock.calls).toEqual([
      ['job-1', { company_name: 'Unknown' }],
      ['job-1', { position_title: 'Architect' }],
      ['job-1', { salary_input: '75k' }],
      ['job-1', { keyword_note: 'Go' }],
    ]);
    expect(refresh).toHaveBeenCalledTimes(4);
  });

  it('runs inline saves without a refresh callback', async () => {
    setup([makeJob()]);
    for (const { options } of mocks.inlineEditors) await options.onSave('value');
    expect(mocks.api.updateJob).toHaveBeenCalledTimes(4);
  });

  it('handles referral toggles, delete confirmation, and their error paths', async () => {
    const refresh = vi.fn();
    const { card } = setup([makeJob()], { refresh });
    const referral = card.querySelector('.editable-card-referral');
    const event = new Event('dblclick', { bubbles: true, cancelable: true });
    const stop = vi.spyOn(event, 'stopPropagation');
    referral.dispatchEvent(event);
    await Promise.resolve();
    expect(event.defaultPrevented).toBe(true);
    expect(stop).toHaveBeenCalledOnce();
    expect(mocks.api.updateJob).toHaveBeenCalledWith('job-1', { is_referral: 1 });
    expect(refresh).toHaveBeenCalledOnce();

    const showToast = vi.fn();
    window.showToast = showToast;
    mocks.api.updateJob.mockRejectedValueOnce(new Error('toggle failed'));
    referral.dispatchEvent(new Event('dblclick', { bubbles: true, cancelable: true }));
    await Promise.resolve();
    await Promise.resolve();
    expect(mocks.toast).toHaveBeenCalledWith('toggle failed', 'error');
    expect(showToast).not.toHaveBeenCalled();

    mocks.api.deleteJob.mockResolvedValueOnce();
    card.querySelector('.btn-card-delete').click();
    await Promise.resolve();
    expect(mocks.api.deleteJob).toHaveBeenCalledWith('job-1');
    expect(refresh).toHaveBeenCalledTimes(2);
    vi.stubGlobal('confirm', vi.fn(() => false));
    card.querySelector('.btn-card-delete').click();
    expect(mocks.api.deleteJob).toHaveBeenCalledOnce();
    mocks.api.deleteJob.mockRejectedValueOnce(new Error('delete failed'));
    vi.stubGlobal('confirm', vi.fn(() => true));
    card.querySelector('.btn-card-delete').click();
    await Promise.resolve();
    await Promise.resolve();
    expect(mocks.toast).toHaveBeenCalledWith('delete failed', 'error');
  });

  it('shows referral success feedback and tolerates missing refresh callbacks', async () => {
    const { card } = setup([makeJob({ is_referral: true })]);
    const showToast = vi.fn();
    window.showToast = showToast;
    card.querySelector('.editable-card-referral').dispatchEvent(new Event('dblclick', { bubbles: true }));
    await Promise.resolve();
    expect(mocks.api.updateJob).toHaveBeenCalledWith('job-1', { is_referral: 0 });
    expect(showToast).toHaveBeenCalledWith('Marked as Non-referral', 'success', 1200);
    mocks.api.deleteJob.mockResolvedValueOnce();
    card.querySelector('.btn-card-delete').click();
    await Promise.resolve();
    expect(mocks.api.deleteJob).toHaveBeenCalledWith('job-1');
  });

  it('shows the referral success message when a referral is added', async () => {
    const { card } = setup([makeJob()]);
    const showToast = vi.fn();
    window.showToast = showToast;
    card.querySelector('.editable-card-referral').dispatchEvent(new Event('dblclick', { bubbles: true }));
    await Promise.resolve();
    expect(showToast).toHaveBeenCalledWith('Marked as Referral', 'success', 1200);
  });

  it('opens details from cards, delays editable clicks, and suppresses control clicks', async () => {
    vi.useFakeTimers();
    const createButton = document.createElement('button');
    createButton.id = 'btn-new-process';
    const created = vi.fn();
    createButton.addEventListener('click', created);
    document.body.append(createButton);
    const { container, card, modal, backdrop } = setup([makeJob()]);
    card.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await Promise.resolve();
    expect(mocks.openWithFlip).toHaveBeenCalledWith(card, modal, backdrop);
    expect(mocks.openDetailModal).toHaveBeenCalledWith(modal, backdrop, 'job-1', null);
    const count = mocks.openDetailModal.mock.calls.length;
    card.querySelector('.btn-card-delete').dispatchEvent(new MouseEvent('click', { bubbles: true }));
    const buttonCalls = mocks.openDetailModal.mock.calls.length;
    expect(buttonCalls).toBe(count);
    card.querySelector('.editable-card-company').classList.add('editable-text');
    card.querySelector('.editable-card-company').dispatchEvent(new MouseEvent('click', { bubbles: true }));
    card.querySelector('.editable-card-company').dispatchEvent(new MouseEvent('click', { bubbles: true }));
    vi.advanceTimersByTime(219);
    expect(mocks.openDetailModal).toHaveBeenCalledTimes(buttonCalls);
    card.querySelector('.editable-card-company').dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
    vi.advanceTimersByTime(2);
    expect(mocks.openDetailModal).toHaveBeenCalledTimes(buttonCalls);
    card.querySelector('.editable-card-company').dispatchEvent(new MouseEvent('click', { bubbles: true }));
    vi.advanceTimersByTime(220);
    await Promise.resolve();
    expect(mocks.openDetailModal).toHaveBeenCalledTimes(buttonCalls + 1);
    card.querySelector('.editable-card-company').dispatchEvent(new MouseEvent('click', { bubbles: true }));
    card.dispatchEvent(pointerEvent('pointerdown'));
    vi.advanceTimersByTime(180);
    vi.advanceTimersByTime(40);
    expect(mocks.openDetailModal).toHaveBeenCalledTimes(buttonCalls + 1);
    window.dispatchEvent(pointerEvent('pointerup'));
    card.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
    container.querySelector('.quick-add-card').click();
    expect(created).toHaveBeenCalledOnce();
  });

  it('ignores clicks on buttons, links, inputs, and textareas inside cards', () => {
    const { card } = setup();
    const controls = ['button', 'a', 'input', 'textarea'].map((tag) => {
      const control = document.createElement(tag);
      card.append(control);
      control.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      return control;
    });
    expect(controls).toHaveLength(4);
    expect(mocks.openDetailModal).not.toHaveBeenCalled();
  });

  it('does not open details when no modal pair exists and supports document fallbacks', async () => {
    const { container, card } = setup([makeJob()], { modal: null, backdrop: null });
    document.querySelector('#detail-modal').remove();
    document.querySelector('#modal-backdrop').remove();
    card.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await Promise.resolve();
    expect(mocks.openWithFlip).not.toHaveBeenCalled();
    expect(mocks.openDetailModal).toHaveBeenCalledWith(null, null, 'job-1', null);
    container.querySelector('.quick-add-card').click();
  });

  it('cancels a pending hold when the pointer moves too far and ignores non-left or control presses', () => {
    vi.useFakeTimers();
    const { card } = setup();
    card.dispatchEvent(pointerEvent('pointerdown', { button: 2 }));
    card.querySelector('.btn-card-delete').dispatchEvent(pointerEvent('pointerdown'));
    card.dispatchEvent(pointerEvent('pointerdown'));
    window.dispatchEvent(pointerEvent('pointermove', { x: 12, y: 13 }));
    window.dispatchEvent(pointerEvent('pointermove', { x: 30, y: 30 }));
    vi.advanceTimersByTime(200);
    expect(card.classList.contains('is-holding')).toBe(false);
    expect(card.classList.contains('is-dragging')).toBe(false);
    window.dispatchEvent(pointerEvent('pointerup'));
    window.dispatchEvent(pointerEvent('pointercancel'));
  });

  it('reorders cards by dragging before and after targets and onto quick add', async () => {
    vi.useFakeTimers();
    const { container, card } = setup([makeJob({ id: 'a' }), makeJob({ id: 'b' }), makeJob({ id: 'c' })]);
    const cards = [...container.querySelectorAll('.process-card')];
    cards.forEach((item, index) => vi.spyOn(item, 'getBoundingClientRect').mockReturnValue({
      left: index * 110, top: 0, width: 100, height: 100,
    }));
    const second = cards[1];
    const third = cards[2];
    vi.spyOn(document, 'elementFromPoint').mockImplementation((x) => (x < 100 ? card : x < 220 ? second : third));
    card.dispatchEvent(pointerEvent('pointerdown', { x: 10, y: 10 }));
    vi.advanceTimersByTime(180);
    expect(card.classList.contains('is-dragging')).toBe(true);
    expect(document.body.classList.contains('is-reordering-cards')).toBe(true);
    window.dispatchEvent(pointerEvent('pointermove', { x: 150, y: 10 }));
    window.dispatchEvent(pointerEvent('pointermove', { x: 190, y: 40 }));
    window.dispatchEvent(pointerEvent('pointermove', { x: 180, y: 40 }));
    window.dispatchEvent(pointerEvent('pointermove', { x: 210, y: 90 }));
    window.dispatchEvent(pointerEvent('pointermove', { x: 310, y: 90 }));
    card.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    expect(mocks.openDetailModal).not.toHaveBeenCalled();
    window.dispatchEvent(pointerEvent('pointerup'));
    await Promise.resolve();
    expect(card.classList.contains('is-dragging')).toBe(false);
    expect(document.body.classList.contains('is-reordering-cards')).toBe(false);
    expect(mocks.api.reorderJobs).toHaveBeenCalledOnce();
    expect(mocks.api.reorderJobs.mock.calls[0][0]).toHaveLength(3);
  });

  it('does not persist an unchanged drag and suppresses the click after a drag', async () => {
    vi.useFakeTimers();
    const { container, card } = setup([makeJob()]);
    vi.spyOn(card, 'getBoundingClientRect').mockReturnValue({ left: 0, top: 0, width: 100, height: 80 });
    vi.spyOn(document, 'elementFromPoint').mockReturnValue(card);
    card.dispatchEvent(pointerEvent('pointerdown'));
    vi.advanceTimersByTime(180);
    container.querySelector('.card-drop-placeholder').remove();
    window.dispatchEvent(pointerEvent('pointerup'));
    card.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    expect(mocks.api.reorderJobs).not.toHaveBeenCalled();
    expect(mocks.openDetailModal).not.toHaveBeenCalled();
    expect(container.querySelectorAll('.card-drop-placeholder')).toHaveLength(0);
  });

  it('handles empty drop targets, detached targets, and drops on the quick-add card', async () => {
    vi.useFakeTimers();
    const { container, card } = setup([makeJob({ id: 'a' }), makeJob({ id: 'b' })]);
    const other = container.querySelectorAll('.process-card')[1];
    const detachedQuickAdd = document.createElement('div');
    detachedQuickAdd.className = 'quick-add-card';
    const detachedCard = document.createElement('div');
    detachedCard.className = 'process-card';
    detachedCard.setAttribute('data-id', 'detached');
    const targets = [null, detachedQuickAdd, detachedCard, card, container.querySelector('.quick-add-card')];
    vi.spyOn(card, 'getBoundingClientRect').mockReturnValue({ left: 0, top: 0, width: 100, height: 80 });
    vi.spyOn(other, 'getBoundingClientRect').mockReturnValue({ left: 110, top: 0, width: 100, height: 80 });
    vi.spyOn(document, 'elementFromPoint').mockImplementation(() => targets.shift());
    card.dispatchEvent(pointerEvent('pointerdown'));
    vi.advanceTimersByTime(180);
    for (let i = 0; i < 5; i += 1) window.dispatchEvent(pointerEvent('pointermove', { x: 120, y: 70 }));
    window.dispatchEvent(pointerEvent('pointerup'));
    await Promise.resolve();
    expect(mocks.api.reorderJobs).toHaveBeenCalledWith(['b', 'a']);
    expect(container.lastElementChild.classList.contains('quick-add-card')).toBe(true);
  });
});
