import { beforeEach, describe, expect, it, vi } from 'vitest';

const { api, toast } = vi.hoisted(() => ({
  api: { createQuestion: vi.fn(), updateQuestion: vi.fn(), deleteQuestion: vi.fn(), reorderQuestions: vi.fn() },
  toast: vi.fn(),
}));
vi.mock('../../public/js/api.js', () => ({ api }));
vi.mock('../../public/js/utils/toast.js', () => ({ showToast: toast }));

import { enableQuestionReordering, renderQuestionList } from '../../public/js/components/questionList.js';

const tick = () => new Promise((resolve) => setTimeout(resolve, 0));

function createList(ids = ['q1', 'q2']) {
  const list = document.createElement('div');
  list.className = 'questions-list';
  const handles = [];
  for (const id of ids) {
    const item = document.createElement('div');
    item.className = 'question-item';
    item.dataset.qid = id;
    const handle = document.createElement('button');
    handle.className = 'question-drag-handle';
    item.appendChild(handle);
    list.appendChild(item);
    handles.push(handle);
  }
  return { list, handles };
}

function rect(top, height = 20, left = 0, width = 100) {
  return { top, bottom: top + height, left, right: left + width, width, height };
}

function startDrag(handle, clientY) {
  handle.dispatchEvent(new PointerEvent('pointerdown', { button: 0, clientY, bubbles: true, cancelable: true }));
}

async function finishDrag(clientY) {
  window.dispatchEvent(new PointerEvent('pointermove', { clientY }));
  window.dispatchEvent(new PointerEvent('pointerup'));
  await tick();
}

beforeEach(() => {
  document.body.innerHTML = '';
  vi.clearAllMocks();
  api.createQuestion.mockResolvedValue({});
  api.updateQuestion.mockResolvedValue({});
  api.deleteQuestion.mockResolvedValue({});
  api.reorderQuestions.mockResolvedValue({});
});

describe('renderQuestionList', () => {
  it('renders text questions, saves notes, deletes, and creates trimmed questions', async () => {
    const changed = vi.fn();
    const root = renderQuestionList('stage-9', [
      { id: 'q1', question: '<unsafe>', is_asked: true, answer_notes: 'Initial note' },
      { id: 'q2', question: 'Second', is_asked: false },
    ], changed);
    expect(root.querySelector('.question-text').textContent).toBe('<unsafe>');
    expect(root.querySelector('.questions-header').textContent).not.toContain('asked');
    expect(root.querySelectorAll('.question-item')).toHaveLength(2);
    expect(root.querySelector('.question-checkbox')).toBeNull();
    expect(root.querySelector('.question-text').textContent).toBe('<unsafe>');
    root.querySelector('.question-answer-box').dispatchEvent(new Event('change'));
    root.querySelector('.question-delete-btn').click();
    const input = root.querySelector('.add-question-input');
    const form = root.querySelector('.add-question-form');
    input.value = '  ';
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    expect(api.createQuestion).not.toHaveBeenCalled();
    input.value = '  New question  ';
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await tick();
    expect(api.updateQuestion).toHaveBeenCalledWith('q1', { answer_notes: 'Initial note' });
    expect(api.deleteQuestion).toHaveBeenCalledWith('q1');
    expect(api.createQuestion).toHaveBeenCalledWith({ stage_id: 'stage-9', question: 'New question', answer_notes: '' });
    expect(input.value).toBe('');
    expect(changed).toHaveBeenCalledTimes(2);
  });

  it('shows errors from question update, delete, answer save, and create', async () => {
    api.updateQuestion.mockRejectedValue(new Error('update failed'));
    api.deleteQuestion.mockRejectedValue(new Error('delete failed'));
    api.createQuestion.mockRejectedValue(new Error('create failed'));
    const root = renderQuestionList('s', [{ id: 'q', question: 'Question' }]);
    root.querySelector('.question-answer-box').dispatchEvent(new Event('change'));
    root.querySelector('.question-delete-btn').click();
    root.querySelector('.add-question-input').value = 'Question two';
    root.querySelector('.add-question-form').dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await tick();
    expect(toast).toHaveBeenCalledWith('update failed', 'error');
    expect(toast).toHaveBeenCalledWith('delete failed', 'error');
    expect(toast).toHaveBeenCalledWith('create failed', 'error');
  });
});

describe('enableQuestionReordering', () => {
  it('ignores absent lists, non-primary pointers, and detached handles', () => {
    expect(enableQuestionReordering(null, 's', vi.fn())).toBeUndefined();
    const { list, handles } = createList();
    enableQuestionReordering(list, 's', vi.fn());
    handles[0].dispatchEvent(new PointerEvent('pointerdown', { button: 2, clientY: 5 }));
    expect(list.querySelector('.question-drop-placeholder')).toBeNull();
    handles[0].closest = () => null;
    handles[0].dispatchEvent(new PointerEvent('pointerdown', { button: 0, clientY: 5 }));
    expect(list.querySelector('.question-drop-placeholder')).toBeNull();
  });

  it('handles dragging the only question when there are no remaining targets', async () => {
    const { list, handles } = createList(['solo']);
    list.querySelector('.question-item').getBoundingClientRect = () => rect(10);
    enableQuestionReordering(list, 'stage', vi.fn());
    startDrag(handles[0], 15);
    await finishDrag(30);
    expect(api.reorderQuestions).not.toHaveBeenCalled();
  });

  it('moves a question below the last item, restores styles, and calls reorder callback', async () => {
    const { list, handles } = createList();
    const [first, second] = list.querySelectorAll('.question-item');
    first.getBoundingClientRect = () => rect(0);
    second.getBoundingClientRect = () => rect(30);
    const reordered = vi.fn();
    enableQuestionReordering(list, 'stage', reordered);
    startDrag(handles[0], 5);
    expect(first.classList.contains('is-dragging')).toBe(true);
    expect(first.style.position).toBe('fixed');
    await finishDrag(70);
    expect(Array.from(list.querySelectorAll('.question-item')).map((item) => item.dataset.qid)).toEqual(['q2', 'q1']);
    expect(api.reorderQuestions).toHaveBeenCalledWith('stage', ['q2', 'q1']);
    expect(reordered).toHaveBeenCalledWith(['q2', 'q1']);
    expect(first.style.position).toBe('');
    expect(list.querySelector('.question-drop-placeholder')).toBeNull();
  });

  it('supports moving above and between items while retaining unchanged ordering', async () => {
    const { list, handles } = createList(['q1', 'q2', 'q3']);
    const [first, second, third] = list.querySelectorAll('.question-item');
    first.getBoundingClientRect = () => rect(0);
    second.getBoundingClientRect = () => rect(30);
    third.getBoundingClientRect = () => rect(60);
    enableQuestionReordering(list, 'stage', vi.fn());
    startDrag(handles[1], 35);
    await finishDrag(-20);
    expect(Array.from(list.querySelectorAll('.question-item')).map((item) => item.dataset.qid)).toEqual(['q2', 'q1', 'q3']);
    expect(api.reorderQuestions).toHaveBeenCalledWith('stage', ['q2', 'q1', 'q3']);

    api.reorderQuestions.mockClear();
    // A separate list lets the next drag begin in its original order.
    const next = createList(['a', 'b', 'c']);
    const [a, b, c] = next.list.querySelectorAll('.question-item');
    a.getBoundingClientRect = () => rect(0);
    b.getBoundingClientRect = () => rect(30);
    c.getBoundingClientRect = () => rect(60);
    enableQuestionReordering(next.list, 'stage', vi.fn());
    startDrag(next.handles[0], 5);
    await finishDrag(55);
    expect(Array.from(next.list.querySelectorAll('.question-item')).map((item) => item.dataset.qid)).toEqual(['b', 'a', 'c']);
    expect(api.reorderQuestions).toHaveBeenCalledWith('stage', ['b', 'a', 'c']);
  });

  it('places the placeholder after all targets when no midpoint qualifies', async () => {
    const { list, handles } = createList(['q1', 'q2', 'q3']);
    const [first, middle, last] = list.querySelectorAll('.question-item');
    first.getBoundingClientRect = () => rect(0, 10);
    middle.getBoundingClientRect = () => rect(30, 20);
    last.getBoundingClientRect = () => rect(60, 100);
    enableQuestionReordering(list, 'stage');
    startDrag(handles[0], 5);
    await finishDrag(110); // The dragged and last target midpoints match, so no insertion target is selected.
    expect(Array.from(list.querySelectorAll('.question-item')).map((item) => item.dataset.qid)).toEqual(['q2', 'q3', 'q1']);
    expect(api.reorderQuestions).toHaveBeenCalledWith('stage', ['q2', 'q3', 'q1']);
  });

  it('uses top and bottom auto-scroll zones and cancels animation on pointer release', async () => {
    const runEdgeDrag = async (direction) => {
      const { list, handles } = createList();
      const [first, second] = list.querySelectorAll('.question-item');
      first.getBoundingClientRect = () => rect(0);
      second.getBoundingClientRect = () => rect(30);
      Object.defineProperty(list, 'scrollHeight', { configurable: true, value: 200 });
      Object.defineProperty(list, 'clientHeight', { configurable: true, value: 100 });
      list.getBoundingClientRect = () => rect(0, 100);
      const callbacks = [];
      const raf = vi.fn((callback) => { callbacks.push(callback); return callbacks.length; });
      const cancel = vi.fn();
      vi.stubGlobal('requestAnimationFrame', raf);
      vi.stubGlobal('cancelAnimationFrame', cancel);
      enableQuestionReordering(list, 'stage', vi.fn());
      startDrag(handles[0], 5);
      const edgeY = direction === 'top' ? 1 : 99;
      window.dispatchEvent(new PointerEvent('pointermove', { clientY: edgeY }));
      expect(raf).toHaveBeenCalledOnce();
      const priorScroll = list.scrollTop;
      callbacks[0]();
      expect(direction === 'top' ? list.scrollTop < priorScroll : list.scrollTop > priorScroll).toBe(true);
      const rafCount = raf.mock.calls.length;
      window.dispatchEvent(new PointerEvent('pointermove', { clientY: direction === 'top' ? 99 : 1 }));
      expect(raf).toHaveBeenCalledTimes(rafCount);
      window.dispatchEvent(new PointerEvent('pointermove', { clientY: 50 }));
      callbacks[1](); // A scheduled frame with auto-scroll disabled does not change scrollTop.
      window.dispatchEvent(new PointerEvent('pointerup'));
      await tick();
      expect(cancel).toHaveBeenCalled();
    };
    await runEdgeDrag('top');
    await runEdgeDrag('bottom');
    vi.unstubAllGlobals();
  });

  it('does not call reorder for unchanged orders and logs reorder failures', async () => {
    const { list, handles } = createList();
    const [first, second] = list.querySelectorAll('.question-item');
    first.getBoundingClientRect = () => rect(0);
    second.getBoundingClientRect = () => rect(30);
    enableQuestionReordering(list, 'stage', vi.fn());
    startDrag(handles[0], 5);
    await finishDrag(5);
    expect(api.reorderQuestions).not.toHaveBeenCalled();

    api.reorderQuestions.mockRejectedValueOnce(new Error('reorder failed'));
    const error = vi.spyOn(console, 'error').mockImplementation(() => {});
    startDrag(handles[0], 5);
    await finishDrag(70);
    expect(error).toHaveBeenCalledWith('Failed to reorder questions:', expect.any(Error));
    error.mockRestore();
  });
});
