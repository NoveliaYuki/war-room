
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

  it('exports a ZIP and reports an export error', async () => {
    const archive = new Blob(['zip'], { type: 'application/zip' });
    global.fetch.mockResolvedValueOnce({ ok: true, blob: async () => archive });
    expect(await api.exportBackup()).toBe(archive);
    expect(global.fetch).toHaveBeenLastCalledWith('/api/backup/export');
    global.fetch.mockResolvedValueOnce({ ok: false, json: async () => ({ error: 'export failed' }) });
    await expect(api.exportBackup()).rejects.toThrow('export failed');
    global.fetch.mockResolvedValueOnce({ ok: false, json: async () => { throw new Error('unreadable'); } });
    await expect(api.exportBackup()).rejects.toThrow('Failed to create backup');
    global.fetch.mockResolvedValueOnce({ ok: false, json: async () => ({}) });
    await expect(api.exportBackup()).rejects.toThrow('Failed to create backup');
  });

  it('sends the selected ZIP and surfaces empty-backup confirmation', async () => {
    const archive = new File(['zip'], 'backup.zip', { type: 'application/zip' });
    global.fetch.mockResolvedValueOnce({ ok: true, json: async () => ({ success: true }) });
    expect(await api.importBackup(archive)).toEqual({ success: true });
    const request = global.fetch.mock.lastCall[1];
    expect(request.method).toBe('POST');
    expect(request.body.get('backup')).toBe(archive);
    expect(request.body.get('allow_empty')).toBe('false');
    global.fetch.mockResolvedValueOnce({ ok: false, json: async () => ({ error: 'empty backup', requires_confirmation: true }) });
    await expect(api.importBackup(archive, true)).rejects.toMatchObject({ message: 'empty backup', requiresEmptyConfirmation: true });
    expect(global.fetch.mock.lastCall[1].body.get('allow_empty')).toBe('true');
    global.fetch.mockResolvedValueOnce({ ok: false, json: async () => { throw new Error('unreadable'); } });
    await expect(api.importBackup(archive)).rejects.toMatchObject({ message: 'Failed to import backup', requiresEmptyConfirmation: false });
    global.fetch.mockResolvedValueOnce({ ok: false, json: async () => ({}) });
    await expect(api.importBackup(archive)).rejects.toMatchObject({ message: 'Failed to import backup', requiresEmptyConfirmation: false });
  });

  it('uses the CV library API contract and encodes version and job identifiers', async () => {
    mockResponse([{ id: 'v1', version_number: 1 }]);
    expect(await api.getCvVersions()).toEqual([{ id: 'v1', version_number: 1 }]);
    expect(global.fetch).toHaveBeenLastCalledWith('/api/cv/versions');

    const file = new File(['cv'], 'cv.pdf', { type: 'application/pdf' });
    mockResponse({ id: 'v2' });
    expect(await api.uploadCvVersion(file)).toEqual({ id: 'v2' });
    expect(global.fetch.mock.lastCall[0]).toBe('/api/cv/versions');
    expect(global.fetch.mock.lastCall[1].body.get('file')).toBe(file);

    const cvBlob = new Blob(['cv']);
    global.fetch.mockResolvedValueOnce({ ok: true, blob: async () => cvBlob });
    expect(await api.downloadCvVersion('v/2')).toBe(cvBlob);
    expect(global.fetch).toHaveBeenLastCalledWith('/api/cv/versions/v%2F2/download');

    mockResponse({ success: true });
    await api.deleteCvVersion('v/2');
    expect(global.fetch.mock.lastCall).toEqual(['/api/cv/versions/v%2F2', { method: 'DELETE' }]);

    mockResponse({ success: true });
    await api.setJobCvVersion('job/1', null);
    expect(global.fetch.mock.lastCall[0]).toBe('/api/jobs/job%2F1/cv-version');
    expect(JSON.parse(global.fetch.mock.lastCall[1].body)).toEqual({ version_id: null });
  });

  it('uses a shared logo URL contract for the avatar component', () => {
    expect(api.getCompanyLogoUrl('Acme & Sons', 'acme.test', 'job/1')).toBe('/api/company-logo?company=Acme%20%26%20Sons&domain=acme.test&job_id=job%2F1');
  });

  it('gives attachment rows a download URL from the selected data source', async () => {
    mockResponse({ id: 'job', attachments: [{ id: 'file/1' }] });
    const job = await api.getJob('job');
    expect(job.attachments[0].download_url).toBe('/api/attachments/file%2F1/download');
  });
});
