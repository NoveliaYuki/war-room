import { beforeEach, describe, expect, it, vi } from 'vitest';

const { api, calls } = vi.hoisted(() => {
  const calls = {};
  const api = new Proxy({}, { get: (_, key) => (calls[key] ||= vi.fn().mockResolvedValue({})) });
  return { api, calls };
});
const toast = vi.hoisted(() => vi.fn());
vi.mock('../../public/js/api.js', () => ({ api }));
vi.mock('../../public/js/flip.js', () => ({ closeWithFlip: vi.fn((modal, backdrop, done) => done?.()), cancelPendingFlipClose: vi.fn() }));
vi.mock('../../public/js/components/questionList.js', () => ({ enableQuestionReordering: vi.fn() }));
vi.mock('../../public/js/utils/toast.js', () => ({ showToast: toast }));

import { openDetailModal } from '../../public/js/components/detailModal.js';

const tick = async () => {
  await new Promise((resolve) => setTimeout(resolve, 0));
  await new Promise((resolve) => setTimeout(resolve, 0));
};

async function editInline(modal, selector, value) {
  const target = modal.querySelector(selector);
  const eventName = selector === '.editable-salary' ? 'click' : 'dblclick';
  target.dispatchEvent(new MouseEvent(eventName, { bubbles: true, cancelable: true }));
  const editor = target.parentElement.querySelector('.inline-edit-input, .inline-edit-textarea');
  editor.value = value;
  editor.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
  await tick();
}

const job = (stages = []) => ({
  id: 'job-1', company_name: 'Acme', position_title: 'Engineer', status: 'ongoing', salary_type: 'limited',
  salary_min: 80000, salary_max: 100000, salary_currency: 'EUR', is_referral: false,
  recruiter_type: 'external', recruiter_name: 'Recruiter', recruiter_agency: 'Agency', recruiter_contact: 'r@example.com',
  keyword_note: 'remote', company_overview: 'Company', reasons_to_change: 'Growth', experience_notes: 'Platform work',
  expected_salary: '€85k base, flexible', interview_notes: 'Notes',
  work_arrangement: 'unknown', employment_type: 'unknown',
  job_post_url: 'https://example.com/job', description: 'Original description', avatar_seed: 'seed', company_domain: 'acme.test',
  attachments: [], interviewers: [], stages,
});

beforeEach(() => {
  document.body.innerHTML = '<div id="detail-modal"></div><div id="modal-backdrop"></div>';
  delete document.body.dataset.demo;
  window.localStorage.removeItem('war-room-detail-split-ratio');
  toast.mockClear();
  Object.values(calls).forEach((fn) => fn.mockReset().mockResolvedValue({}));
});

describe('detail modal', () => {
  it('explains that file attachments are unavailable in demo mode', async () => {
    document.body.dataset.demo = 'true';
    api.getJob.mockResolvedValue(job([{ id: 's1', stage_type: 'HR', status: 'current', questions: [], interviewers: [] }]));
    await openDetailModal('job-1');

    const attachmentSections = [...document.querySelectorAll('.attachments-grid')];
    expect(attachmentSections).toHaveLength(2);
    expect(attachmentSections.every((section) => section.textContent.includes('Attachments are unavailable in the static demo.'))).toBe(true);
  });

  it('supports keyboard and pointer resizing and remembers the pane ratio', async () => {
    api.getJob.mockResolvedValue(job());
    const modal = document.querySelector('#detail-modal');
    await openDetailModal('job-1');

    const handle = modal.querySelector('.split-pane-resizer');
    const layout = modal.querySelector('.modal-split-layout');
    expect(handle.getAttribute('role')).toBe('separator');
    expect(handle.getAttribute('aria-valuenow')).toBe('47');
    expect(handle.getAttribute('aria-valuemin')).toBe('20');
    expect(handle.getAttribute('aria-valuemax')).toBe('80');
    expect(handle.querySelectorAll('svg')).toHaveLength(2);

    handle.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    expect(handle.getAttribute('aria-valuenow')).toBe('49');
    expect(window.localStorage.getItem('war-room-detail-split-ratio')).toBe('49');
    handle.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true }));
    expect(handle.getAttribute('aria-valuenow')).toBe('47');
    handle.dispatchEvent(new KeyboardEvent('keydown', { key: 'Home', bubbles: true }));
    expect(handle.getAttribute('aria-valuenow')).toBe('20');
    handle.dispatchEvent(new KeyboardEvent('keydown', { key: 'End', bubbles: true }));
    expect(handle.getAttribute('aria-valuenow')).toBe('80');

    layout.getBoundingClientRect = () => ({ left: 0, width: 1000 });
    const secondaryPointerDown = new Event('pointerdown', { bubbles: true, cancelable: true });
    Object.defineProperty(secondaryPointerDown, 'button', { value: 2 });
    handle.dispatchEvent(secondaryPointerDown);
    expect(document.body.classList.contains('is-resizing-split')).toBe(false);

    const pointerDown = new Event('pointerdown', { bubbles: true, cancelable: true });
    Object.defineProperty(pointerDown, 'button', { value: 0 });
    handle.dispatchEvent(pointerDown);
    const frames = [];
    const requestFrame = vi.spyOn(window, 'requestAnimationFrame').mockImplementation((callback) => {
      frames.push(callback);
      return frames.length;
    });
    const cancelFrame = vi.spyOn(window, 'cancelAnimationFrame').mockImplementation(() => {});
    const pointerMove = (clientX) => {
      const moveEvent = new Event('pointermove');
      Object.defineProperty(moveEvent, 'clientX', { value: clientX });
      window.dispatchEvent(moveEvent);
    };
    pointerMove(100);
    pointerMove(150);
    expect(requestFrame).toHaveBeenCalledTimes(1);
    expect(window.localStorage.getItem('war-room-detail-split-ratio')).toBe('80');
    frames[0]();
    expect(handle.getAttribute('aria-valuemin')).toBe('35');
    expect(handle.getAttribute('aria-valuemax')).toBe('59');
    expect(handle.getAttribute('aria-valuenow')).toBe('35');
    expect(window.localStorage.getItem('war-room-detail-split-ratio')).toBe('80');
    window.dispatchEvent(new Event('pointerup'));
    expect(handle.getAttribute('aria-valuenow')).toBe('35');
    expect(window.localStorage.getItem('war-room-detail-split-ratio')).toBe('35');
    requestFrame.mockRestore();
    cancelFrame.mockRestore();

    const tinyLayoutPointerDown = new Event('pointerdown', { bubbles: true, cancelable: true });
    Object.defineProperty(tinyLayoutPointerDown, 'button', { value: 0 });
    layout.getBoundingClientRect = () => ({ left: 0, width: 10 });
    handle.dispatchEvent(tinyLayoutPointerDown);
    const tinyPointerMove = new Event('pointermove');
    Object.defineProperty(tinyPointerMove, 'clientX', { value: 300 });
    window.dispatchEvent(tinyPointerMove);
    window.dispatchEvent(new Event('pointerup'));
    expect(handle.getAttribute('aria-valuenow')).toBe('35');

    await openDetailModal('job-1');
    expect(modal.querySelector('.split-pane-resizer').getAttribute('aria-valuenow')).toBe('35');
  });

  it('uses the default pane ratio for invalid or unavailable browser storage', async () => {
    api.getJob.mockResolvedValue(job());
    window.localStorage.setItem('war-room-detail-split-ratio', 'not-a-ratio');
    const modal = document.querySelector('#detail-modal');
    await openDetailModal('job-1');
    expect(modal.querySelector('.split-pane-resizer').getAttribute('aria-valuenow')).toBe('47');

    const getItem = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('storage unavailable');
    });
    await openDetailModal('job-1');
    expect(modal.querySelector('.split-pane-resizer').getAttribute('aria-valuenow')).toBe('47');
    getItem.mockRestore();

    const setItem = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('storage unavailable');
    });
    modal.querySelector('.split-pane-resizer').dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    expect(modal.querySelector('.split-pane-resizer').getAttribute('aria-valuenow')).toBe('49');
    setItem.mockRestore();
  });

  it('renders stages when the API returns null question and interviewer lists', async () => {
    api.getJob.mockResolvedValue(job([{
      id: 'stage-1',
      stage_type: 'HR',
      status: 'current',
      custom_title: 'Initial conversation',
      meeting_type: 'onsite',
      description: '',
      questions: null,
      interviewers: null,
    }]));

    const modal = document.querySelector('#detail-modal');
    await openDetailModal('job-1');

    expect(modal.querySelector('.modal-split-layout')).not.toBeNull();
    expect(modal.querySelector('.questions-workspace').textContent).toContain('(0)');
    expect(modal.querySelector('.interviewers-list').textContent).toContain('No interviewers listed');
    expect(modal.querySelector('.interview-mode-tag').textContent).toBe('In person');
    modal.querySelector('.interview-mode-tag').click();
    await tick();
    expect(api.updateStage).toHaveBeenCalledWith('stage-1', { meeting_type: 'video' });
    expect(api.scheduleMeeting).not.toHaveBeenCalled();
  });

  it('loads and renders general job details with no stages', async () => {
    api.getJob.mockResolvedValue(job());
    const modal = document.querySelector('#detail-modal');
    const backdrop = document.querySelector('#modal-backdrop');
    await openDetailModal('job-1', vi.fn());
    expect(backdrop.classList.contains('active')).toBe(true);
    expect(modal.querySelector('.modal-title').textContent).toContain('Engineer');
    expect(modal.querySelector('.job-status-picker')).not.toBeNull();
    expect(modal.querySelector('#work-arrangement-select')).toBeNull();
    expect(modal.querySelector('#employment-type-select')).toBeNull();
    expect(modal.querySelector('.work-arrangement-tag').textContent).toBe('Not specified');
    expect(modal.querySelector('.employment-type-tag').textContent).toBe('Not specified');
    expect(modal.querySelector('.stage-empty-state')).not.toBeNull();
    const addFirstStageButton = modal.querySelector('#btn-init-first-stage');
    expect(addFirstStageButton).not.toBeNull();
    expect(addFirstStageButton.innerHTML).toContain('<svg');
    expect(addFirstStageButton.textContent).toContain('Add First Stage');
    expect(addFirstStageButton.textContent).not.toContain('${icon');
    expect(modal.querySelector('#company-overview-display').textContent).toBe('Company');
    expect(modal.querySelector('.general-process-group').textContent).toContain('General Selection Process');
    expect(modal.querySelector('.general-process-group').textContent).toContain('My Notes / Anything Else');
    expect(modal.querySelector('#interview-notes-display').textContent).toContain('Notes');
    expect(modal.querySelector('.job-details-group').compareDocumentPosition(modal.querySelector('.my-notes-group')) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(modal.querySelector('.editable-experience-notes').textContent).toBe('Platform work');
    expect(modal.querySelector('.editable-reasons-to-change').textContent).toBe('Growth');
    expect(modal.querySelector('.editable-expected-salary').textContent).toBe('€85k base, flexible');
    expect(modal.querySelector('.editable-interview-notes').textContent).toBe('Notes');

    modal.querySelector('.work-arrangement-tag').click();
    await tick();
    expect(api.updateJob).toHaveBeenCalledWith('job-1', { work_arrangement: 'remote' });

    modal.querySelector('.employment-type-tag').click();
    await tick();
    expect(api.updateJob).toHaveBeenCalledWith('job-1', { employment_type: 'permanent' });
  });

  it('renders and cycles the stored employment type and work arrangement', async () => {
    const fixture = { ...job(), employment_type: 'permanent_b2b', work_arrangement: 'remote' };
    api.getJob.mockImplementation(async () => fixture);
    api.updateJob.mockImplementation(async (_id, updates) => Object.assign(fixture, updates));
    await openDetailModal('job-1');
    expect(document.querySelector('.employment-type-tag').textContent).toBe('Permanent / B2B');
    expect(document.querySelector('.work-arrangement-tag').textContent).toBe('Remote');
    document.querySelector('.employment-type-tag').click();
    await tick();
    expect(api.updateJob).toHaveBeenCalledWith('job-1', { employment_type: 'permanent' });

    for (const arrangement of ['hybrid', 'on_site', 'remote']) {
      document.querySelector('.work-arrangement-tag').click();
      await tick();
      expect(fixture.work_arrangement).toBe(arrangement);
    }
    expect(api.updateJob).toHaveBeenCalledWith('job-1', { work_arrangement: 'hybrid' });
  });

  it('renders active stage questions, changes stage, toggles description, and submits a question', async () => {
    const stages = [
      { id: 's1', stage_type: 'HR', custom_title: 'Screen', status: 'completed', description: 'Intro', questions: [{ id: 'q1', question: 'What is the team?', is_asked: 0, answer_notes: '' }] },
      { id: 's2', stage_type: 'Technical', status: 'current', questions: [] },
    ];
    api.getJob.mockResolvedValue(job(stages));
    const modal = document.querySelector('#detail-modal');
    await openDetailModal(modal, document.querySelector('#modal-backdrop'), 'job-1', vi.fn());
    expect(modal.querySelector('.stage-hero-title').textContent).toBe('Technical');
    modal.querySelector('.stage-tab-item[data-idx="0"]').click();
    expect(modal.querySelector('.stage-hero-title').textContent).toBe('Screen');
    modal.querySelector('.btn-toggle-reachout')?.click();
    const questionInput = modal.querySelector('#active-add-question-form .add-question-input');
    questionInput.value = '  Ask about on-call  ';
    modal.querySelector('#active-add-question-form').dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(api.createQuestion).toHaveBeenCalledWith({ stage_id: 's1', question: 'Ask about on-call', answer_notes: '' });
  });

  it('opens and cancels the edit details form', async () => {
    api.getJob.mockResolvedValue(job());
    const modal = document.querySelector('#detail-modal');
    await openDetailModal(modal, document.querySelector('#modal-backdrop'), 'job-1');
    modal.querySelector('.btn-edit-details').click();
    expect(modal.querySelector('.edit-process-form')).not.toBeNull();
    modal.querySelector('.btn-cancel-edit').click();
    expect(api.getJob).toHaveBeenCalledWith('job-1');
  });

  it('logs a missing job and handles request failure', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => {});
    api.getJob.mockResolvedValueOnce(null);
    await openDetailModal('missing', vi.fn());
    api.getJob.mockRejectedValueOnce(new Error('network'));
    await openDetailModal('missing', vi.fn());
    expect(error).toHaveBeenCalledTimes(2);
    error.mockRestore();
  });

  it('handles job status, deletion confirmation, and all general inline edits', async () => {
    const fixture = job();
    api.getJob.mockResolvedValue(fixture);
    vi.stubGlobal('confirm', vi.fn().mockReturnValueOnce(false).mockReturnValueOnce(true));
    const modal = document.querySelector('#detail-modal');
    const refreshed = vi.fn();
    await openDetailModal(modal, document.querySelector('#modal-backdrop'), fixture.id, refreshed);

    const status = modal.querySelector('.job-status-picker');
    status.value = 'accepted';
    status.dispatchEvent(new Event('change'));
    await tick();
    expect(api.updateJob).toHaveBeenCalledWith(fixture.id, { status: 'accepted' });
    modal.querySelector('.btn-delete-job').click();
    await tick();
    expect(api.deleteJob).not.toHaveBeenCalled();
    modal.querySelector('.btn-delete-job').click();
    await tick();
    expect(api.deleteJob).toHaveBeenCalledWith(fixture.id);
    expect(refreshed).toHaveBeenCalled();

    await openDetailModal(modal, document.querySelector('#modal-backdrop'), fixture.id, refreshed);
    const cases = [
      ['.editable-position-title', 'Principal Engineer', { position_title: 'Principal Engineer' }],
      ['.editable-company-name', '', { company_name: 'Unknown' }],
      ['.editable-salary', 'from 95k', { salary_type: 'no_max', salary_min: 95000, salary_max: null }],
      ['.editable-keywords', 'platform, remote', { keyword_note: 'platform, remote' }],
      ['.editable-overview', 'New overview', { company_overview: 'New overview' }],
      ['.editable-reasons-to-change', 'More scope', { reasons_to_change: 'More scope' }],
      ['.editable-experience-notes', 'Platform migration', { experience_notes: 'Platform migration' }],
      ['.editable-expected-salary', '€90k plus equity', { expected_salary: '€90k plus equity' }],
      ['.editable-interview-notes', 'Hiring team is 8', { interview_notes: 'Hiring team is 8' }],
      ['.editable-description', 'Updated role', { description: 'Updated role' }],
      ['.editable-job-post-url', '', { job_post_url: null }],
    ];
    for (const [selector, value, payload] of cases) {
      await editInline(modal, selector, value);
      expect(api.updateJob).toHaveBeenCalledWith(fixture.id, payload);
    }
    modal.querySelector('.btn-toggle-reachout').click();
    expect(modal.querySelector('#reachout-text-container').style.display).toBe('block');
    modal.querySelector('.btn-toggle-reachout').click();
    expect(modal.querySelector('#reachout-text-container').style.display).toBe('none');
  });

  it('covers active stage recruiter, interviewer, question, and stage action handlers', async () => {
    const stages = [
      { id: 's1', stage_type: 'HR', custom_title: 'Screen', status: 'completed', questions: [] },
      { id: 's2', stage_type: 'Technical', custom_title: 'Architecture', status: 'current', notes: 'Discuss platform ownership', questions: [{ id: 'q1', question: 'Scale?', is_asked: false, answer_notes: '' }] },
    ];
    const fixture = { ...job(stages), interviewers: [{ name: 'Alex', role: 'Engineer', notes: 'Staff' }] };
    api.getJob.mockResolvedValue(fixture);
    vi.stubGlobal('confirm', vi.fn(() => true));
    vi.stubGlobal('prompt', vi.fn().mockReturnValueOnce('Sam').mockReturnValueOnce('Lead').mockReturnValueOnce('Panelist'));
    const modal = document.querySelector('#detail-modal');
    await openDetailModal(modal, document.querySelector('#modal-backdrop'), fixture.id);
    expect(modal.querySelector('.editable-stage-notes').textContent).toBe('Discuss platform ownership');

    const recruiterSelect = modal.querySelector('.stage-recruiter-source-select');
    recruiterSelect.value = 'internal';
    recruiterSelect.dispatchEvent(new Event('change'));
    await tick();
    expect(api.updateStage).toHaveBeenCalledWith('s2', { recruiter_type: 'internal' });
    await editInline(modal, '.editable-stage-recruiter-name', 'Morgan');
    expect(api.updateStage).toHaveBeenCalledWith('s2', { recruiter_name: 'Morgan' });
    await editInline(modal, '.editable-stage-recruiter-agency', 'Product');
    expect(api.updateStage).toHaveBeenCalledWith('s2', { recruiter_agency: 'Product' });
    await editInline(modal, '.editable-stage-recruiter-contact', 'https://example.com/profile');
    expect(api.updateStage).toHaveBeenCalledWith('s2', { recruiter_contact: 'https://example.com/profile' });

    await editInline(modal, '.editable-stage-interviewer-name', 'Taylor');
    expect(api.updateStage).toHaveBeenCalledWith('s2', { interviewers: [{ name: 'Taylor', role: 'Engineer', notes: 'Staff' }] });
    await editInline(modal, '.editable-stage-interviewer-role', 'Director');
    expect(api.updateStage).toHaveBeenCalledWith('s2', { interviewers: [{ name: 'Alex', role: 'Director', notes: 'Staff' }] });
    await editInline(modal, '.editable-stage-interviewer-note', 'Hiring manager');
    expect(api.updateStage).toHaveBeenCalledWith('s2', { interviewers: [{ name: 'Alex', role: 'Engineer', notes: 'Hiring manager' }] });
    modal.querySelector('.btn-add-stage-interviewer').click();
    await tick();
    expect(api.updateStage).toHaveBeenCalledWith('s2', { interviewers: [{ name: 'Alex', role: 'Engineer', notes: 'Staff' }, { name: 'Sam', role: 'Lead', notes: 'Panelist' }] });
    modal.querySelector('.btn-del-stage-interviewer').click();
    await tick();
    expect(api.updateStage).toHaveBeenCalledWith('s2', { interviewers: [] });

    modal.querySelector('.q-notes').value = 'Discuss failure domains';
    modal.querySelector('.q-notes').dispatchEvent(new Event('change'));
    modal.querySelector('.q-del').click();
    modal.querySelector('#active-add-question-form .add-question-input').value = '   ';
    modal.querySelector('#active-add-question-form').dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await tick();
    expect(api.updateQuestion).toHaveBeenCalledWith('q1', { answer_notes: 'Discuss failure domains' });
    expect(api.deleteQuestion).toHaveBeenCalledWith('q1');
    expect(api.createQuestion).not.toHaveBeenCalled();

    await editInline(modal, '.editable-stage-title', 'System Design');
    expect(api.updateStage).toHaveBeenCalledWith('s2', { custom_title: 'System Design' });
    await editInline(modal, '.editable-stage-notes', 'Discuss architecture and team expectations');
    expect(api.updateStage).toHaveBeenCalledWith('s2', { notes: 'Discuss architecture and team expectations' });
    await editInline(modal, '.editable-stage-desc', 'Architecture round');
    expect(api.updateStage).toHaveBeenCalledWith('s2', { description: 'Architecture round' });
    await editInline(modal, '.editable-question-text', 'How do you scale?');
    expect(api.updateQuestion).toHaveBeenCalledWith('q1', { question: 'How do you scale?' });

    modal.querySelector('#btn-toggle-current-stage').click();
    modal.querySelector('#btn-move-stage-up').click();
    modal.querySelector('#btn-delete-stage').click();
    await tick();
    expect(api.setCurrentStage).toHaveBeenCalledWith('s2');
    expect(api.reorderStages).toHaveBeenCalledWith(fixture.id, ['s2', 's1']);
    expect(api.deleteStage).toHaveBeenCalledWith('s2');
  });

  it('supports adding stages and scheduling, clearing, and canceling meetings', async () => {
    const stages = [{ id: 's1', stage_type: 'HR', status: 'current', questions: [] }];
    const fixture = job(stages);
    api.getJob.mockResolvedValue(fixture);
    vi.stubGlobal('confirm', vi.fn(() => true));
    vi.stubGlobal('prompt', vi.fn().mockReturnValueOnce('Culture').mockReturnValueOnce('Cultural').mockReturnValueOnce('Leadership values'));
    const modal = document.querySelector('#detail-modal');
    await openDetailModal(modal, document.querySelector('#modal-backdrop'), fixture.id);
    modal.querySelector('#btn-add-stage-tab').click();
    await tick();
    expect(api.createStage).toHaveBeenCalledWith({ job_id: fixture.id, stage_type: 'Cultural', custom_title: 'Culture', description: 'Leadership values', status: 'pending' });

    await openDetailModal(modal, document.querySelector('#modal-backdrop'), fixture.id);
    modal.querySelector('.btn-edit-meeting-schedule').click();
    const form = modal.querySelector('#schedule-stage-form');
    form.querySelector('[name="meeting_date"]').value = '2026-10-01';
    form.querySelector('[name="meeting_time"]').value = '11:00';
    form.querySelector('[name="meeting_type"]').value = 'onsite';
    form.querySelector('[name="meeting_url"]').value = '';
    form.querySelector('[name="notes"]').value = 'Bring portfolio';
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await tick();
    expect(api.scheduleMeeting).toHaveBeenCalledWith('s1', expect.objectContaining({ meeting_date: '2026-10-01', meeting_time: '11:00', meeting_type: 'onsite', notes: 'Bring portfolio' }));

    await openDetailModal(modal, document.querySelector('#modal-backdrop'), fixture.id);
    modal.querySelector('.btn-edit-meeting-schedule').click();
    modal.querySelector('.btn-clear-schedule').click();
    await tick();
    expect(api.scheduleMeeting).toHaveBeenCalledWith('s1', { meeting_date: null, meeting_time: null, meeting_url: null, meeting_type: 'video' });
  });

  it('renders stage-level data, inherited interviewer data, and safe meeting fallbacks', async () => {
    const stages = [
      { id: 's1', stage_type: 'HR', status: 'completed', questions: [], interviewers: [] },
      {
        id: 's2', stage_type: 'Technical', custom_title: 'Architecture', status: 'current',
        meeting_date: '2026-10-01', meeting_time: '', meeting_type: 'unexpected', meeting_url: 'javascript:alert(1)',
        questions: [], interviewers: [],
      },
    ];
    const fixture = {
      ...job(stages),
      job_post_url: 'javascript:alert(1)',
      interviewers: [{ name: 'Shared interviewer', role: '', notes: '' }],
      attachments: [
        { id: 'general-file', original_name: 'brief.pdf', file_size: 1024, stage_id: null },
        { id: 'stage-file', original_name: 'notes.pdf', file_size: 2048, stage_id: 's2' },
      ],
    };
    api.getJob.mockResolvedValue(fixture);
    await openDetailModal('job-1');
    const modal = document.querySelector('#detail-modal');
    expect(modal.querySelector('.meeting-format-pill').classList.contains('pill-video')).toBe(true);
    expect(modal.querySelector('a[href^="javascript:"]')).toBeNull();
    expect(modal.querySelector('.interviewer-name').textContent).toBe('Shared interviewer');
    expect(modal.querySelector('.attachment-name').textContent).toBe('brief.pdf');
    expect(modal.querySelectorAll('.attachment-chip')).toHaveLength(2);
    expect(modal.querySelector('.job-details-group').textContent).toContain('Files (1)');
    expect(modal.querySelector('.job-details-group').textContent).not.toContain('Additional Files');
    expect(modal.querySelector('.split-pane-left .stage-focus-group')).toBeNull();
    expect(modal.querySelector('.split-pane-right .stage-focus-group')).not.toBeNull();
    const rightPane = modal.querySelector('.split-pane-right');
    expect(rightPane.querySelector('.stage-hero-banner').compareDocumentPosition(rightPane.querySelector('.stage-focus-group')) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(rightPane.querySelector('.stage-focus-group').compareDocumentPosition(rightPane.querySelector('.questions-workspace')) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

    vi.stubGlobal('confirm', vi.fn(() => false));
    modal.querySelector('.btn-del-attachment').click();
    await tick();
    expect(api.deleteAttachment).not.toHaveBeenCalled();

    vi.stubGlobal('confirm', vi.fn(() => true));
    modal.querySelector('.btn-del-attachment').click();
    await tick();
    expect(api.deleteAttachment).toHaveBeenCalledWith('general-file');

    modal.querySelector('.stage-tab-item[data-idx="0"]').click();
    expect(modal.querySelector('.interviewer-name')).toBeNull();
    expect(modal.querySelector('.stage-recruiter-source-select').value).toBe('internal');

    api.getJob.mockResolvedValue({ ...fixture, company_name: 'Unknown', description: '', attachments: [], stages: [] });
    await openDetailModal('job-1');
    expect(modal.querySelector('.modal-company').classList.contains('is-unknown')).toBe(true);
    expect(modal.querySelector('.btn-toggle-reachout')).toBeNull();
    expect(modal.querySelector('#btn-init-first-stage')).not.toBeNull();
    expect(modal.querySelector('.attachment-chip')).toBeNull();
  });

  it('synchronizes stage-one recruiter edits and treats empty uploads as a no-op', async () => {
    const fixture = job([
      {
        id: 's1', stage_type: 'HR', status: 'current', questions: [],
        interviewers: [{ name: 'Panelist', role: 'Recruiter', notes: 'First call' }],
      },
      { id: 's2', stage_type: 'Technical', status: 'pending', questions: [], interviewers: [] },
    ]);
    api.getJob.mockResolvedValue(fixture);
    const modal = document.querySelector('#detail-modal');
    await openDetailModal('job-1');

    const recruiterType = modal.querySelector('.stage-recruiter-source-select');
    recruiterType.value = 'internal';
    recruiterType.dispatchEvent(new Event('change'));
    await tick();
    expect(api.updateStage).toHaveBeenCalledWith('s1', { recruiter_type: 'internal' });
    expect(api.updateJob).toHaveBeenCalledWith('job-1', { recruiter_type: 'internal' });

    await openDetailModal('job-1');
    await editInline(modal, '.editable-stage-recruiter-name', '');
    await editInline(modal, '.editable-stage-recruiter-agency', '');
    await editInline(modal, '.editable-stage-recruiter-contact', '');
    expect(api.updateJob).toHaveBeenCalledWith('job-1', { recruiter_name: null });
    expect(api.updateJob).toHaveBeenCalledWith('job-1', { recruiter_agency: null });
    expect(api.updateJob).toHaveBeenCalledWith('job-1', { recruiter_contact: null });

    modal.querySelector('#file-upload-general-input').dispatchEvent(new Event('change'));
    expect(modal.querySelector('#file-upload-stage-input')).toBeNull();

    const file = new File(['resume'], 'resume.pdf', { type: 'application/pdf' });
    const generalUpload = modal.querySelector('#file-upload-general-input');
    Object.defineProperty(generalUpload, 'files', { configurable: true, value: [file] });
    api.uploadAttachment.mockRejectedValueOnce(new Error('general upload failed'));
    generalUpload.dispatchEvent(new Event('change'));
    await tick();
    expect(toast).toHaveBeenCalledWith('general upload failed', 'error');

    const retryUpload = modal.querySelector('#file-upload-general-input');
    Object.defineProperty(retryUpload, 'files', { configurable: true, value: [file] });
    retryUpload.dispatchEvent(new Event('change'));
    await tick();
    expect(api.uploadAttachment).toHaveBeenCalledTimes(2);
    expect(api.uploadAttachment.mock.calls[1][0].get('job_id')).toBe('job-1');

    modal.querySelector('.stage-tab-item[data-idx="1"]').click();
    const stageUpload = modal.querySelector('#file-upload-stage-input');
    Object.defineProperty(stageUpload, 'files', { configurable: true, value: [file] });
    stageUpload.dispatchEvent(new Event('change'));
    await tick();
    expect(api.uploadAttachment.mock.calls[2][0].get('stage_id')).toBe('s2');

    modal.querySelector('.stage-tab-item[data-idx="0"]').click();
    vi.stubGlobal('prompt', vi.fn().mockReturnValueOnce(null));
    modal.querySelector('.btn-add-stage-interviewer').click();
    await tick();
    expect(api.updateStage).not.toHaveBeenCalledWith('s1', expect.objectContaining({ interviewers: expect.any(Array) }));

    vi.stubGlobal('prompt', vi.fn().mockReturnValueOnce('New panelist').mockReturnValueOnce(null).mockReturnValueOnce(''));
    modal.querySelector('.btn-add-stage-interviewer').click();
    await tick();
    expect(api.updateStage).toHaveBeenCalledWith('s1', {
      interviewers: [
        { name: 'Panelist', role: 'Recruiter', notes: 'First call' },
        { name: 'New panelist', role: '', notes: '' },
      ],
    });
    modal.querySelector('.btn-del-stage-interviewer').click();
    await tick();
    expect(api.updateStage).toHaveBeenCalledWith('s1', { interviewers: [] });
  });

  it('renders undisclosed jobs and optional stage content without unsafe links', async () => {
    const fixture = {
      ...job([{
        id: 's1', stage_type: 'Cultural', custom_title: '', status: 'current', description: '',
        meeting_date: '2026-10-01', meeting_time: '15:00', meeting_type: 'phone', meeting_url: 'https://meet.example',
        recruiter_type: 'internal', recruiter_name: 'Stage Contact', recruiter_agency: '', recruiter_contact: '',
        questions: [{ id: 'q1', question: 'Culture?', is_asked: true, answer_notes: '' }],
      }]),
      company_name: 'Unknown',
      salary_type: 'unknown', salary_min: null, salary_max: null,
      is_referral: true, job_post_url: '', description: '', keyword_note: '',
      company_overview: '', reasons_to_change: '', experience_notes: '', expected_salary: '', interview_notes: '',
      attachments: [],
    };
    api.getJob.mockResolvedValue(fixture);
    const previousToast = window.showToast;
    const inlineToast = vi.fn();
    window.showToast = inlineToast;
    const modal = document.querySelector('#detail-modal');
    await openDetailModal(null, null, 'job-1');

    expect(modal.querySelector('.modal-company').classList.contains('is-unknown')).toBe(true);
    expect(modal.querySelector('.salary-tag').textContent.toLowerCase()).toContain('undisclosed');
    expect(modal.querySelector('.referral-tag').textContent).toContain('Referral');
    expect(modal.querySelector('.modal-meta-pills .recruiter-badge')).toBeNull();
    expect(modal.querySelector('.editable-stage-recruiter-name').textContent).toContain('Stage Contact');
    expect(modal.querySelector('.editable-job-post-url').textContent).toContain('No job post link recorded');
    expect(modal.querySelector('#reachout-text-container')).toBeNull();
    expect(modal.querySelector('.editable-keywords').textContent).toContain('No keywords');
    expect(modal.querySelector('#company-overview-display').textContent).toContain('Summarize what the company does');
    expect(modal.querySelector('#experience-notes-display').textContent).toContain('Add experience');
    expect(modal.querySelector('#expected-salary-display').textContent).toContain('Add your target');
    expect(modal.querySelector('.meeting-format-pill').classList.contains('pill-phone')).toBe(true);
    expect(modal.querySelector('.btn-join-meeting-inline').getAttribute('href')).toBe('https://meet.example');
    expect(modal.querySelector('.stage-hero-title').textContent).toBe('Cultural');
    expect(modal.querySelector('.editable-stage-desc').textContent).toBe('No stage description');
    expect(modal.querySelector('.q-toggle')).toBeNull();
    expect(modal.querySelector('.editable-question-text').textContent).toBe('Culture?');
    expect(modal.querySelector('.interview-mode-tag').textContent).toBe('Remote');
    expect(modal.querySelector('.questions-workspace-title').textContent).not.toContain('asked');
    expect(modal.querySelector('.stage-focus-group').textContent).toContain('Screening Documents');
    expect(modal.querySelector('#file-upload-stage-input')).toBeNull();

    modal.querySelector('.editable-position-title').dispatchEvent(new MouseEvent('dblclick', { bubbles: true, cancelable: true }));
    const titleEditor = modal.querySelector('.editable-position-title').parentElement.querySelector('.inline-edit-input');
    titleEditor.value = '   ';
    titleEditor.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    await tick();
    expect(api.updateJob).not.toHaveBeenCalledWith('job-1', { position_title: '' });

    modal.querySelector('.editable-modal-referral').click();
    await tick();
    expect(api.updateJob).toHaveBeenCalledWith('job-1', { is_referral: 0 });
    expect(inlineToast).toHaveBeenCalledWith('Marked as Non-referral', 'success', 1200);

    api.getJob.mockResolvedValue({ ...fixture, stages: undefined, attachments: undefined });
    await openDetailModal('job-1');
    expect(modal.querySelector('#btn-init-first-stage')).not.toBeNull();
    expect(modal.querySelector('.attachment-chip')).toBeNull();
    if (previousToast === undefined) delete window.showToast;
    else window.showToast = previousToast;
  });

  it('reports failures from stage, question, meeting, and job actions', async () => {
    const stages = [
      { id: 's1', stage_type: 'HR', status: 'completed', questions: [] },
      { id: 's2', stage_type: 'Technical', status: 'current', questions: [{ id: 'q1', question: 'Scale?', is_asked: false, answer_notes: '' }] },
    ];
    api.getJob.mockResolvedValue(job(stages));
    vi.stubGlobal('confirm', vi.fn(() => true));
    const modal = document.querySelector('#detail-modal');
    await openDetailModal('job-1');

    api.updateJob.mockRejectedValueOnce(new Error('status failed'));
    modal.querySelector('.job-status-picker').dispatchEvent(new Event('change'));
    api.deleteJob.mockRejectedValueOnce(new Error('delete job failed'));
    modal.querySelector('.btn-delete-job').click();
    api.updateJob.mockRejectedValueOnce(new Error('referral failed'));
    modal.querySelector('.editable-modal-referral').click();
    api.updateJob.mockRejectedValueOnce(new Error('employment type failed'));
    modal.querySelector('.employment-type-tag').click();
    api.setCurrentStage.mockRejectedValueOnce(new Error('current stage failed'));
    modal.querySelector('#btn-toggle-current-stage').click();
    api.reorderStages.mockRejectedValueOnce(new Error('reorder failed'));
    modal.querySelector('#btn-move-stage-up').click();
    api.deleteStage.mockRejectedValueOnce(new Error('delete stage failed'));
    modal.querySelector('#btn-delete-stage').click();
    api.updateQuestion.mockRejectedValueOnce(new Error('question notes failed'));
    modal.querySelector('.q-notes').dispatchEvent(new Event('change'));
    api.deleteQuestion.mockRejectedValueOnce(new Error('question delete failed'));
    modal.querySelector('.q-del').click();
    await tick();
    expect(toast).toHaveBeenCalledWith('employment type failed', 'error');
    expect(toast).toHaveBeenCalledWith('status failed', 'error');
    expect(toast).toHaveBeenCalledWith('delete job failed', 'error');
    expect(toast).toHaveBeenCalledWith('referral failed', 'error');
    expect(toast).toHaveBeenCalledWith('current stage failed', 'error');
    expect(toast).toHaveBeenCalledWith('reorder failed', 'error');
    expect(toast).toHaveBeenCalledWith('delete stage failed', 'error');
    expect(toast).toHaveBeenCalledWith('question notes failed', 'error');
    expect(toast).toHaveBeenCalledWith('question delete failed', 'error');

    await openDetailModal('job-1');
    modal.querySelector('.btn-edit-meeting-schedule').click();
    api.scheduleMeeting.mockRejectedValueOnce(new Error('meeting save failed'));
    modal.querySelector('#schedule-stage-form').dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await tick();
    expect(toast).toHaveBeenCalledWith('meeting save failed', 'error');

    await openDetailModal('job-1');
    modal.querySelector('.btn-edit-details').click();
    api.updateJob.mockRejectedValueOnce(new Error('job update failed'));
    modal.querySelector('.edit-process-form').dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await tick();
    expect(toast).toHaveBeenCalledWith('job update failed', 'error');
  });

  it('submits edited details and derives salary fields from entered bounds', async () => {
    api.getJob.mockResolvedValue(job());
    const modal = document.querySelector('#detail-modal');
    await openDetailModal(modal, document.querySelector('#modal-backdrop'), 'job-1');
    modal.querySelector('.btn-edit-details').click();
    const form = modal.querySelector('.edit-process-form');
    form.querySelector('[name="salary_max"]').value = '';
    form.querySelector('[name="salary_max"]').dispatchEvent(new Event('input'));
    form.querySelector('[name="salary_min"]').value = '70000';
    form.querySelector('[name="salary_min"]').dispatchEvent(new Event('input'));
    expect(form.querySelector('[name="salary_type"]').value).toBe('no_max');
    form.querySelector('[name="salary_max"]').value = '90000';
    form.querySelector('[name="salary_max"]').dispatchEvent(new Event('input'));
    expect(form.querySelector('[name="salary_type"]').value).toBe('limited');
    form.querySelector('[name="position_title"]').value = 'Staff Engineer';
    form.querySelector('[name="experience_notes"]').value = 'Distributed systems';
    form.querySelector('[name="expected_salary"]').value = '€95k depending on scope';
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await tick();
    expect(api.updateJob).toHaveBeenCalledWith('job-1', expect.objectContaining({
      position_title: 'Staff Engineer', salary_type: 'limited', salary_min: 70000, salary_max: 90000,
      experience_notes: 'Distributed systems', expected_salary: '€95k depending on scope', is_referral: false,
    }));
  });
});
