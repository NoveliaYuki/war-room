import { beforeEach, describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({
  api: {
    createQuestion: vi.fn(), updateQuestion: vi.fn(), deleteQuestion: vi.fn(), reorderQuestions: vi.fn(),
    createStage: vi.fn(), updateStage: vi.fn(), deleteStage: vi.fn(), reorderStages: vi.fn(),
    getMeetings: vi.fn(), getJob: vi.fn(), updateJob: vi.fn(), deleteJob: vi.fn(), reorderJobs: vi.fn(),
  },
  openDetailModal: vi.fn(),
}));

vi.mock('../../public/js/api.js', () => ({ api: mocks.api }));
vi.mock('../../public/js/components/detailModal.js', () => ({ openDetailModal: mocks.openDetailModal }));

import { renderQuestionList, enableQuestionReordering } from '../../public/js/components/questionList.js';
import { renderStageTracker } from '../../public/js/components/stageTracker.js';
import { renderScheduleView } from '../../public/js/components/scheduleView.js';
import { renderCardGrid } from '../../public/js/components/cardGrid.js';
import { renderCompanyAvatar } from '../../public/js/avatar.js';

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

beforeEach(() => {
  document.body.innerHTML = '';
  vi.clearAllMocks();
  vi.stubGlobal('confirm', vi.fn(() => true));
  mocks.api.createQuestion.mockResolvedValue({});
  mocks.api.updateQuestion.mockResolvedValue({});
  mocks.api.deleteQuestion.mockResolvedValue({});
  mocks.api.reorderQuestions.mockResolvedValue({});
  mocks.api.createStage.mockResolvedValue({});
  mocks.api.updateStage.mockResolvedValue({});
  mocks.api.deleteStage.mockResolvedValue({});
  mocks.api.reorderStages.mockResolvedValue({});
  mocks.api.getMeetings.mockResolvedValue([]);
  mocks.api.getJob.mockResolvedValue({});
  mocks.api.updateJob.mockResolvedValue({});
  mocks.api.deleteJob.mockResolvedValue({});
  mocks.api.reorderJobs.mockResolvedValue({});
});

describe('question list', () => {
  it('renders questions as text and saves answer notes', async () => {
    const changed = vi.fn();
    const root = renderQuestionList('s1', [
      { id: 'q1', question: 'Question one', is_asked: 1, answer_notes: 'notes' },
      { id: 'q2', question: 'Question two', is_asked: 0 },
    ], changed);
    expect(root.querySelector('.questions-header').textContent).not.toContain('asked');
    expect(root.querySelectorAll('.question-item')).toHaveLength(2);
    expect(root.querySelector('.question-checkbox')).toBeNull();
    expect(root.querySelector('.question-text').textContent).toBe('Question one');
    root.querySelector('.question-answer-box').dispatchEvent(new Event('change'));
    root.querySelector('.question-delete-btn').click();
    await flush();
    expect(mocks.api.updateQuestion).toHaveBeenCalledWith('q1', { answer_notes: 'notes' });
    expect(mocks.api.deleteQuestion).toHaveBeenCalledWith('q1');
    expect(changed).toHaveBeenCalled();
  });

  it('creates non-empty questions and ignores whitespace', async () => {
    const root = renderQuestionList('stage-x', [], vi.fn());
    const input = root.querySelector('.add-question-input');
    const form = root.querySelector('form');
    input.value = '  ';
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    expect(mocks.api.createQuestion).not.toHaveBeenCalled();
    input.value = '  New question  ';
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await flush();
    expect(mocks.api.createQuestion).toHaveBeenCalledWith({ stage_id: 'stage-x', question: 'New question', answer_notes: '' });
    expect(input.value).toBe('');
  });

  it('reorders pointer dragged questions and ignores non-primary pointer presses', async () => {
    const root = renderQuestionList('stage-x', [
      { id: 'a', question: 'A' }, { id: 'b', question: 'B' },
    ]);
    const [first, second] = root.querySelectorAll('.question-item');
    first.getBoundingClientRect = () => ({ top: 0, bottom: 20, left: 0, width: 100, height: 20 });
    second.getBoundingClientRect = () => ({ top: 30, bottom: 50, left: 0, width: 100, height: 20 });
    const handle = first.querySelector('.question-drag-handle');
    handle.dispatchEvent(new PointerEvent('pointerdown', { button: 2, clientY: 5 }));
    expect(root.querySelector('.question-drop-placeholder')).toBeNull();
    handle.dispatchEvent(new PointerEvent('pointerdown', { button: 0, clientY: 5, bubbles: true }));
    window.dispatchEvent(new PointerEvent('pointermove', { clientY: 45 }));
    window.dispatchEvent(new PointerEvent('pointerup'));
    await flush();
    expect(mocks.api.reorderQuestions).toHaveBeenCalledWith('stage-x', ['b', 'a']);
  });

  it('safely handles a missing reorder list', () => {
    expect(enableQuestionReordering(null, 's1', vi.fn())).toBeUndefined();
  });
});

describe('stage tracker', () => {
  it('renders an empty state and adds a stage form', async () => {
    const refreshed = vi.fn();
    const root = renderStageTracker('job-1', [], refreshed);
    expect(root.querySelector('.empty-state')).not.toBeNull();
    root.querySelector('.btn-new-stage').click();
    root.querySelector('.stage-title-input').value = 'Design';
    root.querySelector('.stage-desc-input').value = 'Architecture discussion';
    root.querySelector('form').dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await flush();
    expect(mocks.api.createStage).toHaveBeenCalledWith({ job_id: 'job-1', stage_type: 'Technical', custom_title: 'Design', description: 'Architecture discussion', status: 'pending' });
    expect(refreshed).toHaveBeenCalled();
  });

  it('updates status, reorders and confirms stage deletion', async () => {
    const refreshed = vi.fn();
    const root = renderStageTracker('j', [
      { id: 'a', stage_type: 'HR', custom_title: 'First', status: 'current', description: 'Intro' },
      { id: 'b', stage_type: 'Offer & Decision', status: 'pending' },
    ], refreshed);
    const cards = root.querySelectorAll('.stage-card');
    cards[0].querySelector('select').value = 'completed';
    cards[0].querySelector('select').dispatchEvent(new Event('change'));
    cards[0].querySelector('[title="Move Stage Down"]').click();
    cards[1].querySelector('[title="Move Stage Up"]').click();
    cards[0].querySelector('[title="Remove Stage"]').click();
    await flush();
    expect(mocks.api.updateStage).toHaveBeenCalledWith('a', { status: 'completed' });
    expect(mocks.api.reorderStages).toHaveBeenCalledWith('j', ['b', 'a']);
    expect(mocks.api.deleteStage).toHaveBeenCalledWith('a');
    expect(refreshed).toHaveBeenCalled();
  });

  it('handles stage defaults, boundaries, canceled actions, and API failures', async () => {
    const refreshed = vi.fn();
    const root = renderStageTracker('job', [
      { id: 'first', stage_type: 'Unknown', status: 'invalid', description: '' },
      { id: 'last', stage_type: 'HR', status: 'pending', description: 'Screening' },
    ], refreshed);
    const cards = root.querySelectorAll('.stage-card');
    expect(cards[0].classList.contains('is-pending')).toBe(true);
    expect(cards[0].querySelector('.stage-type-badge').classList.contains('HR')).toBe(true);
    expect(cards[0].querySelector('.stage-description')).toBeNull();
    expect(cards[0].querySelector('[title="Move Stage Up"]')).toBeNull();
    expect(cards[0].querySelector('[title="Move Stage Down"]')).not.toBeNull();
    expect(cards[1].querySelector('[title="Move Stage Up"]')).not.toBeNull();
    expect(cards[1].querySelector('[title="Move Stage Down"]')).toBeNull();

    mocks.api.updateStage.mockRejectedValueOnce(new Error('status failed'));
    cards[0].querySelector('select').value = 'skipped';
    cards[0].querySelector('select').dispatchEvent(new Event('change'));
    mocks.api.reorderStages.mockRejectedValueOnce(new Error('reorder failed'));
    cards[0].querySelector('[title="Move Stage Down"]').click();
    vi.stubGlobal('confirm', vi.fn(() => false));
    cards[0].querySelector('[title="Remove Stage"]').click();
    await flush();
    expect(mocks.api.deleteStage).not.toHaveBeenCalled();

    vi.stubGlobal('confirm', vi.fn(() => true));
    mocks.api.deleteStage.mockRejectedValueOnce(new Error('delete failed'));
    cards[0].querySelector('[title="Remove Stage"]').click();
    await flush();

    root.querySelector('.btn-new-stage').click();
    const form = root.querySelector('.inline-stage-form');
    form.querySelector('.stage-title-input').value = '   ';
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    expect(mocks.api.createStage).not.toHaveBeenCalled();
    form.querySelector('.stage-title-input').value = 'Follow-up';
    form.querySelector('.stage-desc-input').value = '   ';
    mocks.api.createStage.mockRejectedValueOnce(new Error('create failed'));
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await flush();
    expect(mocks.api.createStage).toHaveBeenCalledWith({ job_id: 'job', stage_type: 'Technical', custom_title: 'Follow-up', description: '', status: 'pending' });
  });

  it('supports stage actions when the optional refresh callback is absent', async () => {
    const root = renderStageTracker('job', [
      { id: 'one', stage_type: 'HR', status: 'pending' },
      { id: 'two', stage_type: 'Technical', status: 'current' },
    ]);
    const cards = root.querySelectorAll('.stage-card');
    cards[0].querySelector('select').value = 'completed';
    cards[0].querySelector('select').dispatchEvent(new Event('change'));
    cards[0].querySelector('[title="Move Stage Down"]').click();
    vi.stubGlobal('confirm', vi.fn(() => true));
    cards[0].querySelector('[title="Remove Stage"]').click();
    root.querySelector('.btn-new-stage').click();
    const form = root.querySelector('.inline-stage-form');
    form.querySelector('.stage-title-input').value = 'Screen';
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await flush();
    expect(mocks.api.updateStage).toHaveBeenCalledWith('one', { status: 'completed' });
    expect(mocks.api.reorderStages).toHaveBeenCalledWith('job', ['two', 'one']);
    expect(mocks.api.deleteStage).toHaveBeenCalledWith('one');
    expect(mocks.api.createStage).toHaveBeenCalledWith({ job_id: 'job', stage_type: 'Technical', custom_title: 'Screen', description: '', status: 'pending' });
  });

  it('canceling the stage form invokes refresh', () => {
    const refreshed = vi.fn();
    const root = renderStageTracker('j', [], refreshed);
    root.querySelector('.btn-new-stage').click();
    root.querySelector('.btn-cancel-stage').click();
    expect(refreshed).toHaveBeenCalledOnce();
  });
});

describe('schedule view', () => {
  it('renders empty, current, next, future, past, phone and unsafe URL cases', async () => {
    const date = (offset) => {
      const d = new Date(); d.setDate(d.getDate() + offset);
      return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
    };
    const meeting = (id, offset, extra = {}) => ({
      job_id: id, meeting_date: offset === null ? '' : date(offset), meeting_time: '10:00', meeting_type: 'video',
      stage_type: 'Technical', position_title: 'Engineer', company_name: 'Acme', ...extra,
    });
    const root = document.createElement('div');
    mocks.api.getMeetings.mockResolvedValueOnce([]);
    await renderScheduleView(root, document.createElement('div'), document.createElement('div'), vi.fn());
    expect(root.querySelector('.empty-state')).not.toBeNull();

    mocks.api.getMeetings.mockResolvedValueOnce([
      meeting('today', 0, { recruiter_name: 'R', recruiter_contact: '123|abc', stage_notes: 'Bring notes', meeting_url: 'https://meet.example' }),
      meeting('phone', 0, { meeting_type: 'phone', meeting_url: 'javascript:alert(1)', recruiter_contact: '<img src=x onerror=alert(1)>|x' }),
      meeting('untrusted-type', 0, { meeting_type: 'video\" onmouseover=alert(1)', stage_type: '<img src=x onerror=alert(1)>' }),
      meeting('tomorrow', 1, { meeting_type: 'video" onmouseover=alert(1)', meeting_url: 'https://future.example', recruiter_name: 'Future recruiter', stage_notes: 'Confirm panel', company_domain: 'acme.example', avatar_seed: 'Acme' }),
      meeting('future', 4, { company_name: 'Unknown' }),
      meeting('future-phone', 5, { meeting_type: 'phone', meeting_url: 'javascript:alert(1)', recruiter_contact: '555-0100|mobile' }),
      meeting('undated', null),
      meeting('past', -1),
    ]);
    const modal = document.createElement('div');
    const backdrop = document.createElement('div');
    await renderScheduleView(root, modal, backdrop, vi.fn());
    expect(root.querySelector('.today-banner.has-meetings')).not.toBeNull();
    expect(root.textContent).toContain('Tomorrow');
    expect(root.textContent).toContain('Upcoming Dates');
    expect(root.textContent).toContain('Past Meetings');
    expect(root.querySelector('a[href="https://meet.example"]')).not.toBeNull();
    expect(root.querySelector('a[href="https://future.example"]')).not.toBeNull();
    expect(root.querySelector('a[href^="javascript:"]')).toBeNull();
    expect(root.querySelector('.meeting-row-right .phone-call-indicator').textContent).toContain('Phone');
    expect(root.querySelector('.today-meeting-card[data-jobid="phone"] .phone-call-indicator').textContent).toContain('<img src=x onerror=alert(1)>');
    expect(root.querySelector('.meeting-format-pill[onmouseover]')).toBeNull();
    expect(root.querySelector('.meeting-row-card .meeting-format-pill[onmouseover]')).toBeNull();
    expect(root.querySelector('.meeting-row-card .meeting-format-pill').classList.contains('pill-video')).toBe(true);
    const scheduleLogo = root.querySelector('.meeting-row-card .company-logo-img');
    const jobCardLogo = document.createElement('div');
    jobCardLogo.innerHTML = renderCompanyAvatar('Acme', 'Acme', 64, 'acme.example', 'tomorrow');
    expect(scheduleLogo.getAttribute('src')).toBe(jobCardLogo.querySelector('.company-logo-img').getAttribute('src'));
    expect(root.querySelector('.stage-type-badge img')).toBeNull();
    expect(root.querySelector('.today-meeting-card img')).toBeNull();
    root.querySelector('.btn-open-meeting').click();
    await flush();
    expect(backdrop.classList.contains('active')).toBe(true);
    expect(mocks.openDetailModal).toHaveBeenCalled();
    root.querySelector('.meeting-row-card .btn-open-meeting').click();
    await flush();
    expect(mocks.openDetailModal).toHaveBeenLastCalledWith(modal, backdrop, 'tomorrow', expect.any(Function));
  });
});

describe('card grid', () => {
  it('renders empty and populated grids and handles delete, referral, edit, click, and mouse events', async () => {
    const root = document.createElement('div');
    const launch = document.createElement('button'); launch.id = 'btn-new-process'; launch.addEventListener('click', vi.fn());
    document.body.append(launch);
    renderCardGrid(root, []);
    expect(root.querySelector('.empty-state')).not.toBeNull();
    root.querySelector('.btn-create-first-process').click();
    renderCardGrid(root, [{ id: 'j1', company_name: 'Acme', position_title: 'Engineer', status: 'ongoing', salary_type: 'unknown', is_referral: 0, recruiter_type: 'none', total_stages_count: 0 }]);
    const card = root.querySelector('.process-card');
    expect(card).not.toBeNull();
    card.getBoundingClientRect = () => ({ left: 5, top: 6, width: 100, height: 80 });
    card.dispatchEvent(new MouseEvent('mousemove', { clientX: 15, clientY: 26 }));
    expect(card.style.getPropertyValue('--mouse-x')).toBe('10px');
    card.querySelector('.editable-card-company').dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
    let edit = card.querySelector('input'); edit.value = 'New Co'; edit.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }));
    card.querySelector('.editable-card-referral').dispatchEvent(new MouseEvent('dblclick', { bubbles: true, cancelable: true }));
    card.querySelector('.btn-card-delete').click();
    root.querySelector('.quick-add-card').click();
    card.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await flush();
    expect(mocks.api.updateJob).toHaveBeenCalled();
    expect(mocks.api.deleteJob).toHaveBeenCalledWith('j1');
    expect(mocks.openDetailModal).toHaveBeenCalled();
  });
});
