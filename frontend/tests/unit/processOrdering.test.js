import { describe, expect, it, vi } from 'vitest';
import { readSavedSortMode, sortJobsForDisplay } from '../../public/js/utils/processOrdering.js';

describe('process ordering', () => {
  it('restores supported and legacy modes and safely falls back', () => {
    const storage = { getItem: vi.fn(() => 'oldest') };
    expect(readSavedSortMode(storage, 'waiting')).toBe('added-oldest');
    storage.getItem.mockReturnValue('newest');
    expect(readSavedSortMode(storage, 'waiting')).toBe('added-newest');
    storage.getItem.mockReturnValue('advanced');
    expect(readSavedSortMode(storage, 'waiting')).toBe('advanced');
    storage.getItem.mockReturnValue('unknown');
    expect(readSavedSortMode(storage, 'waiting')).toBe('added-newest');
    storage.getItem.mockImplementation(() => { throw new Error('storage blocked'); });
    expect(readSavedSortMode(storage, 'waiting')).toBe('added-newest');
  });

  it('sorts each date, progress, and manual mode while keeping ties deterministic', () => {
    const jobs = [
      { id: 'old', order_index: 3, created_at: 1, status_changed_at: 2, current_stage_index: 1, total_stages_count: 4 },
      { id: 'new', order_index: 2, created_at: 3, status_changed_at: 1, current_stage_index: 3, total_stages_count: 4 },
      { id: 'middle', order_index: 1, created_at: 2, status_changed_at: 3, current_stage_index: 2, total_stages_count: 4 },
    ];
    const ids = (mode, status = 'waiting') => sortJobsForDisplay(jobs, status, mode).map(({ id }) => id);
    expect(ids('added-newest')).toEqual(['new', 'middle', 'old']);
    expect(ids('added-oldest')).toEqual(['old', 'middle', 'new']);
    expect(ids('status-newest')).toEqual(['middle', 'old', 'new']);
    expect(ids('status-oldest')).toEqual(['new', 'old', 'middle']);
    expect(ids('advanced')).toEqual(['new', 'middle', 'old']);
    expect(ids('early')).toEqual(['old', 'middle', 'new']);
    expect(ids('manual')).toEqual(['middle', 'new', 'old']);
    expect(ids('added-newest', 'all')).toEqual(['middle', 'new', 'old']);
    expect(sortJobsForDisplay(jobs, 'waiting', 'added-newest')).not.toBe(jobs);
    const stageLess = [{ id: 'one', order_index: 2, created_at: 1 }, { id: 'two', order_index: 1, created_at: 2 }];
    expect(sortJobsForDisplay(stageLess, 'ongoing', 'advanced').map(({ id }) => id)).toEqual(['two', 'one']);
    const tied = [
      { id: 'later-order', order_index: 2, created_at: 4, status_changed_at: 5, current_stage_index: 1, total_stages_count: 2 },
      { id: 'earlier-order', order_index: 1, created_at: 4, status_changed_at: 5, current_stage_index: 1, total_stages_count: 2 },
      { id: 'earlier-date', order_index: 3, created_at: 3, status_changed_at: 4, current_stage_index: 1, total_stages_count: 2 },
    ];
    expect(sortJobsForDisplay(tied, 'waiting', 'added-newest').map(({ id }) => id)).toEqual(['later-order', 'earlier-order', 'earlier-date']);
    expect(sortJobsForDisplay(tied, 'waiting', 'status-oldest').map(({ id }) => id)).toEqual(['earlier-date', 'earlier-order', 'later-order']);
    expect(sortJobsForDisplay(tied, 'ongoing', 'advanced').map(({ id }) => id)).toEqual(['later-order', 'earlier-order', 'earlier-date']);
    expect(sortJobsForDisplay(tied, 'ongoing', 'early').map(({ id }) => id)).toEqual(['later-order', 'earlier-order', 'earlier-date']);
  });
});
