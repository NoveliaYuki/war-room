
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { api } from '../../public/js/api.js';

describe('api', () => {
  beforeEach(() => {
    global.fetch = vi.fn();
  });

  afterEach(() => {
    vi.resetAllMocks();
  });

  const mockResponse = (data, status = 200, ok = true) => {
    global.fetch.mockResolvedValueOnce({
      ok,
      status,
      json: async () => data
    });
  };

  const methods = ['getJobs', 'getJobCounts', 'getJob', 'createJob', 'updateJob', 'deleteJob', 'reorderJobs', 'createStage', 'updateStage', 'setCurrentStage', 'getMeetings', 'scheduleMeeting', 'deleteStage', 'reorderStages', 'createQuestion', 'reorderQuestions', 'updateQuestion', 'deleteQuestion', 'uploadAttachment', 'deleteAttachment'];

  methods.forEach(method => {
    it(`${method} success`, async () => {
      mockResponse({ success: true });
      let res;
      if (['getJobs', 'getJobCounts', 'getMeetings'].includes(method)) {
         res = await api[method]();
      } else if (['getJob', 'deleteJob', 'deleteStage', 'deleteQuestion', 'deleteAttachment', 'setCurrentStage'].includes(method)) {
         res = await api[method]('id123');
      } else if (['createJob', 'createStage', 'createQuestion', 'uploadAttachment'].includes(method)) {
         res = await api[method]({});
      } else if (['reorderJobs'].includes(method)) {
         res = await api[method]([]);
      } else if (['reorderStages', 'reorderQuestions'].includes(method)) {
         res = await api[method]('id123', []);
      } else {
         res = await api[method]('id123', {});
      }
      expect(res).toEqual({ success: true });
    });

    it(`${method} error response`, async () => {
      global.fetch.mockResolvedValueOnce({
        ok: false,
        status: 400,
        json: async () => ({ error: 'Bad Request' })
      });
      let p;
      if (['getJobs', 'getJobCounts', 'getMeetings'].includes(method)) {
         p = api[method]();
      } else if (['getJob', 'deleteJob', 'deleteStage', 'deleteQuestion', 'deleteAttachment', 'setCurrentStage'].includes(method)) {
         p = api[method]('id123');
      } else if (['createJob', 'createStage', 'createQuestion', 'uploadAttachment'].includes(method)) {
         p = api[method]({});
      } else if (['reorderJobs'].includes(method)) {
         p = api[method]([]);
      } else if (['reorderStages', 'reorderQuestions'].includes(method)) {
         p = api[method]('id123', []);
      } else {
         p = api[method]('id123', {});
      }
      await expect(p).rejects.toThrow();
    });
  });

  it('encodes only meaningful job filters and optional meeting dates', async () => {
    mockResponse([]);
    await api.getJobs('ongoing', '  data science  ');
    expect(global.fetch).toHaveBeenLastCalledWith('/api/jobs?status=ongoing&search=data+science');
    mockResponse([]);
    await api.getJobs('all', '   ');
    expect(global.fetch).toHaveBeenLastCalledWith('/api/jobs?');
    mockResponse([]);
    await api.getMeetings('2001-01-01 & next');
    expect(global.fetch).toHaveBeenLastCalledWith('/api/meetings?date=2001-01-01%20%26%20next');
  });

  it('encodes identifiers as a single path segment', async () => {
    mockResponse({ id: 'a/b ?' });
    await api.getJob('a/b ?');
    expect(global.fetch).toHaveBeenCalledWith('/api/jobs/a%2Fb%20%3F');
  });

  it('reports the create endpoint fallback for unreadable and empty errors', async () => {
    global.fetch.mockResolvedValueOnce({ ok: false, status: 500, json: () => Promise.reject(new Error('invalid json')) });
    await expect(api.createJob({})).rejects.toThrow('Failed to create');
    global.fetch.mockResolvedValueOnce({ ok: false, status: 500, json: async () => ({}) });
    await expect(api.createJob({})).rejects.toThrow('Failed to create job process');
  });

  if (api.getCompanyLogo) {
    it('getCompanyLogo test', async () => {
      mockResponse({ url: '...' });
      const res = await api.getCompanyLogo('example.com');
      expect(res).toBeTruthy();
    });
  }
});
