import { beforeEach, describe, expect, it, vi } from 'vitest';
import { buildDemoJobs, buildHistoricalDemoJobs, initialJobs } from '../../demo/data/jobs.js';
import {
  canonicalDemoJobPayload, canonicalDemoPeriodName, canonicalDemoQuestionPayload, canonicalDemoStagePayload,
  localizeDemoJob, localizeDemoPeriodName, localizeDemoQuestion, localizeDemoStage,
} from '../../demo/data/localize.js';
import { demoText } from '../../demo/data/text.js';

vi.mock('../../demo/fileStore.js', () => ({
  saveFile: vi.fn(async () => {}),
  readFile: vi.fn(async () => null),
  deleteFile: vi.fn(async () => {}),
  replaceFiles: vi.fn(async () => {}),
}));

const clone = (value) => JSON.parse(JSON.stringify(value));
const TEXT_FIELDS = ['position_title', 'keyword_note', 'description', 'company_overview', 'interview_notes', 'reasons_to_change', 'experience_notes', 'expected_salary', 'recruiter_agency'];

/** Collects every string in a value, for checks that no English sample text is left. */
function strings(value) {
  if (typeof value === 'string') return [value];
  if (Array.isArray(value)) return value.flatMap(strings);
  if (value && typeof value === 'object') return Object.values(value).flatMap(strings);
  return [];
}

describe('demo sample text', () => {
  it('defines the same wording shape in every language', () => {
    const shape = (text) => Object.keys(text).filter((key) => !['roles', 'historicalRoles', 'stageKinds', 'arrangements'].includes(key)).sort();
    expect(shape(demoText.es)).toEqual(shape(demoText.en));
    expect(shape(demoText.ca)).toEqual(shape(demoText.en));
    for (const language of ['es', 'ca']) {
      expect(demoText[language].roles).toHaveLength(12);
      expect(demoText[language].historicalRoles).toHaveLength(10);
      expect(Object.keys(demoText[language].questions)).toEqual(Object.keys(demoText.en.questions));
    }
  });

  it('builds the same records in every language with only their text changed', () => {
    const english = buildDemoJobs('en');
    expect(english).toEqual(initialJobs);
    for (const language of ['es', 'ca']) {
      const local = buildDemoJobs(language);
      expect(local.map((job) => job.id)).toEqual(english.map((job) => job.id));
      local.forEach((job, index) => {
        expect(job.stages.map((stage) => stage.id)).toEqual(english[index].stages.map((stage) => stage.id));
        expect(job.status).toBe(english[index].status);
        for (const field of TEXT_FIELDS) expect(job[field], `${job.id} ${field}`).not.toBe(english[index][field]);
      });
    }
    expect(buildDemoJobs('es')[3].expected_salary).toMatch(/^130\.000\s€–170\.000\s€$/);
    expect(buildDemoJobs('ca')[0].stages[1].description).toBe("Entrevista tècnica per al lloc d'Enginyer/a frontend sènior a Google.");
    expect(buildDemoJobs('ca')[3].recruiter_agency).toBe("Selecció de talent d'Apple");
  });

  it('shows saved English sample text in the selected language', () => {
    const local = buildDemoJobs('es');
    const shown = localizeDemoJob(clone(initialJobs[1]), 'es');
    for (const field of TEXT_FIELDS) expect(shown[field]).toBe(local[1][field]);
    expect(shown.current_stage_title).toBe('Cultural');
    expect(shown.stages[1].custom_title).toBe('Ronda técnica 1');
    expect(shown.stages[1].interviewers[0]).toEqual(local[1].stages[1].interviewers[0]);
    expect(shown.stages[1].questions.map((question) => question.question)).toEqual(local[1].stages[1].questions.map((question) => question.question));
    expect(localizeDemoJob(clone(initialJobs[1]), 'en')).toEqual(initialJobs[1]);
  });

  it('leaves edited fields exactly as the visitor wrote them', () => {
    const saved = clone(initialJobs[0]);
    saved.keyword_note = 'My own note';
    saved.stages[0].notes = 'Typed in English on purpose';
    saved.stages[0].questions[0].question = 'My question';
    const shown = localizeDemoJob(saved, 'ca');
    expect(shown.keyword_note).toBe('My own note');
    expect(shown.stages[0].notes).toBe('Typed in English on purpose');
    expect(shown.stages[0].questions[0].question).toBe('My question');
    expect(shown.description).toBe(buildDemoJobs('ca')[0].description);
  });

  it('translates historical jobs, stages, questions, and period names', () => {
    const historical = buildHistoricalDemoJobs();
    expect(localizeDemoJob(clone(historical[0]), 'es').position_title).toBe('Diseñador/a UX');
    expect(localizeDemoJob(clone(historical[0]), 'ca').keyword_note).toBe("Dissenyador/a UX • Lloc d'exemple del juny del 2021");
    const stage = localizeDemoStage(clone(initialJobs[0].stages[0]), 'es');
    expect(stage.description).toBe('Entrevista de RR. HH. para el puesto de Ingeniero/a frontend sénior en Google.');
    expect(localizeDemoStage(clone(initialJobs[0].stages[0]), 'en')).toEqual(initialJobs[0].stages[0]);
    const question = localizeDemoQuestion(clone(initialJobs[0].stages[0].questions[0]), 'ca');
    expect(question.question).toBe('Quin tipus de feina et dona més energia?');
    expect(localizeDemoQuestion(clone(initialJobs[0].stages[0].questions[0]), 'en').question).toBe('What kind of work gives you the most energy?');
    expect(localizeDemoPeriodName({ id: 'demo-period-current', name: 'Current' }, 'es')).toBe('Actual');
    expect(localizeDemoPeriodName({ id: 'demo-period-june-2021', name: 'June 2021' }, 'ca')).toBe('Juny del 2021');
    expect(localizeDemoPeriodName({ id: 'demo-period-current', name: 'Renamed' }, 'es')).toBe('Renamed');
    expect(localizeDemoPeriodName({ id: 'demo-period-other', name: 'Current' }, 'es')).toBe('Current');
  });

  it('saves untouched translated sample text back in English', () => {
    const local = buildDemoJobs('es')[0];
    const payload = canonicalDemoJobPayload('demo-1', { keyword_note: local.keyword_note, description: 'Edited', salary_min: 1 }, 'es');
    expect(payload).toEqual({ keyword_note: initialJobs[0].keyword_note, description: 'Edited', salary_min: 1 });
    expect(canonicalDemoJobPayload('demo-1', { keyword_note: 'x' }, 'en')).toEqual({ keyword_note: 'x' });

    const stage = local.stages[0];
    const stagePayload = canonicalDemoStagePayload(stage.id, { notes: stage.notes, interviewers: [{ ...stage.interviewers[0] }, { name: 'New', role: 'Mine' }] }, 'es');
    expect(stagePayload).toEqual({ notes: initialJobs[0].stages[0].notes, interviewers: [initialJobs[0].stages[0].interviewers[0], { name: 'New', role: 'Mine' }] });
    expect(canonicalDemoStagePayload(stage.id, { notes: 'x' }, 'en')).toEqual({ notes: 'x' });

    const question = stage.questions[0];
    expect(canonicalDemoQuestionPayload(question.id, { question: question.question, is_asked: true }, 'es'))
      .toEqual({ question: initialJobs[0].stages[0].questions[0].question, is_asked: true });
    expect(canonicalDemoQuestionPayload(question.id, { question: 'x' }, 'en')).toEqual({ question: 'x' });

    expect(canonicalDemoPeriodName('demo-period-current', 'Actual', 'es')).toBe('Current');
    expect(canonicalDemoPeriodName('demo-period-current', 'Mi búsqueda', 'es')).toBe('Mi búsqueda');
  });
});

describe('demo store language', () => {
  beforeEach(() => {
    vi.resetModules();
    window.localStorage.clear();
    vi.stubGlobal('fetch', vi.fn(async () => new Response(new Blob(['<svg xmlns="http://www.w3.org/2000/svg"/>'], { type: 'image/svg+xml' }), { status: 200 })));
  });

  /** Loads the demo store with the interface language set to `language`. */
  async function loadStore(language) {
    window.localStorage.setItem('war-room.language', language);
    const i18n = await import('../../public/js/i18n.js');
    const store = await import('../../demo/demoStore.js');
    return { ...store, i18n };
  }

  it('serves every sample record in the selected language and follows a language switch', async () => {
    const { demoApi, i18n } = await loadStore('es');
    const englishSample = new Set(strings(buildDemoJobs('en')).filter((text) => !strings(buildDemoJobs('es')).includes(text)));
    const leaked = (records) => strings(records).filter((text) => englishSample.has(text));

    const jobs = await demoApi.getJobs();
    expect(jobs.find((job) => job.id === 'demo-1').position_title).toBe('Ingeniero/a frontend sénior');
    expect(leaked(jobs)).toEqual([]);
    expect(leaked(await demoApi.getJob('demo-2'))).toEqual([]);
    expect(leaked(await demoApi.getMeetings())).toEqual([]);
    expect((await demoApi.getSearchPeriods()).map((period) => period.name)).toEqual(['Actual', 'Junio de 2021']);
    const technologies = await demoApi.getTechnologies();
    expect(technologies.find((item) => item.name === 'Swift').jobs[0].position_title).toBe('Ingeniero/a macOS sénior');
    expect((await demoApi.getJobs('all', 'telemetría')).map((job) => job.id)).toEqual(['demo-8']);

    i18n.setLanguage('ca');
    expect((await demoApi.getJob('demo-1')).position_title).toBe('Enginyer/a frontend sènior');
    i18n.setLanguage('en');
    expect((await demoApi.getJob('demo-1')).position_title).toBe('Senior Frontend Engineer');
  });

  it('keeps saved sample text in English so it can still be translated later', async () => {
    const { demoApi, i18n } = await loadStore('es');
    const job = await demoApi.getJob('demo-1');
    const updated = await demoApi.updateJob('demo-1', { keyword_note: job.keyword_note, description: job.description, expected_salary: 'Mi rango' });
    expect(updated.keyword_note).toBe(job.keyword_note);
    expect(updated.history.filter((entry) => entry.action === 'changed role highlights')).toEqual([]);

    const stage = job.stages[0];
    const savedStage = await demoApi.updateStage(stage.id, { interviewers: stage.interviewers });
    expect(savedStage.interviewers).toEqual(stage.interviewers);
    await demoApi.scheduleMeeting(stage.id, { notes: stage.notes });
    const question = stage.questions[0];
    expect((await demoApi.updateQuestion(question.id, { question: question.question, is_asked: true })).question).toBe(question.question);
    const current = (await demoApi.getSearchPeriods()).find((period) => period.id === 'demo-period-current');
    expect((await demoApi.updateSearchPeriod(current.id, { name: current.name, start_date: current.start_date, end_date: '' })).name).toBe('Actual');

    i18n.setLanguage('en');
    const english = await demoApi.getJob('demo-1');
    expect(english.keyword_note).toBe(initialJobs[0].keyword_note);
    expect(english.expected_salary).toBe('Mi rango');
    expect(english.stages[0].interviewers).toEqual(initialJobs[0].stages[0].interviewers);
    expect(english.stages[0].notes).toBe(initialJobs[0].stages[0].notes);
    expect(english.stages[0].questions[0].question).toBe(initialJobs[0].stages[0].questions[0].question);
    expect((await demoApi.getSearchPeriods()).find((period) => period.id === 'demo-period-current').name).toBe('Current');
  });

  it('saves an empty company as Unknown without a company history entry', async () => {
    const { demoApi } = await loadStore('es');
    const created = await demoApi.createJob({ company_name: '', position_title: 'Ingeniera', status: 'waiting' });
    const updated = await demoApi.updateJob(created.id, { company_name: '', position_title: 'Ingeniera' });
    expect(updated.company_name).toBe('Unknown');
    expect((updated.history || []).map((entry) => entry.action)).not.toContain('changed company');
  });

  it('fills the sample import draft in the selected language', async () => {
    const { demoApi } = await loadStore('ca');
    const preview = await demoApi.previewJobImport({ url: 'https://jobs.example.com/1' });
    expect(preview.draft.position_title).toBe('Enginyer/a de plataforma sènior');
    expect(preview.draft.company_overview).toContain('## Requisits');
    expect(preview.technologies.map((item) => item.name)).toEqual(expect.arrayContaining(['Python', 'AWS', 'Terraform']));
  });
});
