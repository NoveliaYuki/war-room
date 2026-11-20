/**
 * @fileoverview Synthetic board data for spreadsheet import tests: two processes with stages, questions,
 * a search period, technologies, and CV versions, and an API source that serves them.
 */

import { vi } from 'vitest';

const clone = (value) => structuredClone(value);
/** Returns Unix seconds for a local calendar date at noon, so tests do not depend on the time zone. */
export const noon = (year, month, day) => Math.floor(new Date(year, month - 1, day, 12).getTime() / 1000);

/**
 * Returns a fresh copy of the synthetic board.
 *
 * @returns {{jobs: object[], stagesByJob: Map<string, object[]>, periods: object[], technologies: object[], cvVersions: object[]}} The data.
 */
export function fixture() {
  const jobs = [
    {
      id: 'job-1', company_name: 'Acme, Inc.', position_title: 'Senior "Platform" Engineer',
      company_overview: '## About\nWe build **fast** services.\n\n- Go\n- [Docs](https://example.com/docs)',
      status: 'ongoing', current_stage_title: 'Recruiter Screen', current_stage_title_is_type: false,
      work_arrangement: 'on_site', employment_type: 'permanent_b2b', salary_type: 'limited', salary_min: 60000, salary_max: 75000,
      salary_currency: 'EUR', expected_salary: '70k + equity', is_referral: true, recruiter_name: 'Núria Puig', recruiter_agency: null,
      recruiter_contact: 'nuria@example.com', technologies: [{ id: 'tech-go', name: 'Go' }, { id: 'tech-pg', name: 'PostgreSQL' }],
      selected_cv_version: { id: 'cv-3', version_number: 3, original_name: 'cv.pdf' }, application_sent_date: '2026-11-02',
      recruiter_first_contact_date: 'someday', search_period_id: 'period-1', job_post_url: 'https://example.com/jobs/1', application_url: null,
      created_at: noon(2026, 11, 1), status_changed_at: noon(2026, 11, 5), updated_at: noon(2026, 11, 6),
    },
    {
      id: 'job-2', company_name: 'Unknown', position_title: 'Analyst', company_overview: 'x'.repeat(40000), status: 'waiting',
      work_arrangement: 'unknown', employment_type: 'unknown', salary_type: 'unknown', salary_min: null, salary_max: null, salary_currency: 'EUR',
      technologies: [], selected_cv_version: null, is_referral: false, current_stage_title: 'HR', current_stage_title_is_type: true,
      created_at: noon(2026, 11, 1), status_changed_at: noon(2026, 11, 1), updated_at: noon(2026, 11, 1),
    },
  ];
  const stagesByJob = new Map([
    ['job-1', [
      { id: 'stage-a', order_index: 0, stage_type: 'HR', custom_title: 'Recruiter Screen', questions: [
        { id: 'q-1', order_index: 0, question: 'Why us?', answer_notes: 'Mission\nand team', is_asked: false },
        { id: 'q-2', order_index: 1, question: 'Salary?', answer_notes: '', is_asked: true },
      ] },
      { id: 'stage-b', order_index: 1, stage_type: 'Technical', custom_title: null, questions: [{ id: 'q-3', order_index: 0, question: 'Design a cache', answer_notes: '', is_asked: false }] },
    ]],
    ['job-2', [{ id: 'stage-c', order_index: 0, stage_type: 'HR', custom_title: null, questions: [] }]],
  ]);
  return {
    jobs, stagesByJob,
    periods: [{ id: 'period-1', name: 'Autumn 2026' }, { id: 'period-2', name: 'Spring 2027' }],
    technologies: [{ id: 'tech-go', name: 'Go', aliases: ['Golang'] }, { id: 'tech-pg', name: 'PostgreSQL', aliases: ['Postgres'] }, { id: 'tech-k8s', name: 'Kubernetes', aliases: ['K8s'] }],
    cvVersions: [{ id: 'cv-4', version_number: 4 }, { id: 'cv-3', version_number: 3 }],
  };
}

/**
 * Serves the data like the API, recording each read.
 *
 * @param {ReturnType<typeof fixture>} data - Board data.
 * @returns {object} getJobs, getJob, getSearchPeriods, getTechnologies, and getCvVersions.
 */
export function sourceFor(data) {
  return {
    getJobs: vi.fn(async () => clone(data.jobs)),
    getJob: vi.fn(async (id) => ({ id, stages: clone(data.stagesByJob.get(id) || []) })),
    getSearchPeriods: vi.fn(async () => clone(data.periods)),
    getTechnologies: async () => clone(data.technologies),
    getCvVersions: async () => ({ versions: clone(data.cvVersions) }),
  };
}
