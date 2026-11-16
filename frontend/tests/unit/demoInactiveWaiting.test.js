import { describe, expect, it } from 'vitest';
import { initialJobs } from '../../demo/data/jobs.js';
import { applyInactiveWaitingPolicy, touchDemoJob } from '../../demo/inactiveWaiting.js';

const DAY_SECONDS = 24 * 60 * 60;

describe('demo inactive waiting policy', () => {
  it('rejects only waiting jobs with at least 30 days without activity', () => {
    const nowMs = Date.UTC(2026, 9, 13);
    const now = Math.floor(nowMs / 1000);
    const jobs = [
      { status: 'waiting', last_activity_at: now - 30 * DAY_SECONDS },
      { status: 'waiting', last_activity_at: now - 29 * DAY_SECONDS },
      { status: 'ongoing', last_activity_at: now - 60 * DAY_SECONDS },
    ];

    expect(applyInactiveWaitingPolicy(jobs, nowMs)).toBe(true);
    expect(jobs.map((job) => job.status)).toEqual(['rejected', 'waiting', 'ongoing']);
    expect(jobs[0].automatic_rejection_reason).toContain('30 days');
    expect(jobs[0].status_changed_at).toBe(now);
  });

  it('touches a saved change and clears the auto-rejection note', () => {
    const job = { last_activity_at: 1, automatic_rejection_reason: 'old reason' };
    touchDemoJob(job, Date.UTC(2026, 9, 13));
    expect(job.last_activity_at).toBe(Math.floor(Date.UTC(2026, 9, 13) / 1000));
    expect(job.automatic_rejection_reason).toBe('');
  });

  it('seeds a clear automatic-rejection example and keeps Waiting samples recent', () => {
    const now = Math.floor(Date.now() / 1000);
    const automaticExample = initialJobs.find((job) => job.automatic_rejection_reason);
    expect(automaticExample).toMatchObject({ status: 'rejected' });
    expect(automaticExample.automatic_rejection_reason).toContain('30 days');
    expect(now - automaticExample.last_activity_at).toBeGreaterThanOrEqual(30 * DAY_SECONDS);
    for (const job of initialJobs.filter((item) => item.status === 'waiting')) {
      expect(now - job.last_activity_at).toBeLessThan(30 * DAY_SECONDS);
    }
  });
});
