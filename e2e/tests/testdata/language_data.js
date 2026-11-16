// Runs inside the page for TestEveryLanguageRendersOnlyItsOwnText.
// Returns the supported languages, their dictionaries, and the record text no language translates.
async ({ signedIn, defaultStageTexts }) => {
  const { LANGUAGES } = await import("/public/js/i18n.js");
  const { api } = await import("/public/js/api.js");
  const dictionaries = {};
  for (const { code } of LANGUAGES) {
    const messages = (await import(`/public/js/locales/${code}.js`)).default;
    dictionaries[code] = Object.fromEntries(Object.entries(messages).flatMap(([key, value]) => (
      typeof value === "string" ? [[key, value]] : Object.entries(value).map(([form, text]) => [`${key}.${form}`, text])
    )));
  }

  // Names, contacts, links, and technologies stay as written in every language.
  const terms = new Set(["War Room", "EUR", "USD", "GBP", ...LANGUAGES.map((language) => language.name)]);
  const add = (...values) => values.forEach((value) => { if (typeof value === "string" && value.trim()) terms.add(value.trim()); });
  // Signed in, every field the user typed is theirs; the server's default stage text must still be translated.
  const addTyped = (...values) => { if (signedIn) values.filter((value) => !defaultStageTexts.includes(value)).forEach((value) => add(value)); };

  const jobs = await Promise.all((await api.getJobs("all", "")).map((job) => api.getJob(job.id)));
  for (const job of jobs) {
    add(job.company_name, job.recruiter_name, job.recruiter_contact, job.job_post_url, job.application_url, job.company_domain, job.avatar_seed);
    addTyped(job.position_title, job.keyword_note, job.description, job.company_overview, job.interview_notes, job.reasons_to_change, job.experience_notes, job.expected_salary, job.recruiter_agency);
    (job.attachments || []).forEach((file) => add(file.original_name));
    add(job.selected_cv_version?.original_name);
    for (const stage of job.stages || []) {
      add(stage.recruiter_name, stage.recruiter_contact, stage.meeting_url, stage.meeting_time);
      addTyped(stage.custom_title, stage.description, stage.notes, stage.recruiter_agency);
      for (const person of stage.interviewers || []) { add(person.name); addTyped(person.role, person.notes); }
      for (const question of stage.questions || []) addTyped(question.question, question.answer_notes);
    }
  }
  for (const technology of await api.getTechnologies()) add(technology.name, ...(technology.aliases || []));
  if (signedIn) {
    const me = await api.getAuthMe();
    const user = me?.user || me;
    add(user?.email, user?.name);
    for (const period of await api.getSearchPeriods()) addTyped(period.name);
    const cvs = await api.getCvVersions().catch(() => ({}));
    for (const version of Array.isArray(cvs) ? cvs : cvs.versions || []) add(version.original_name);
  }
  return { languages: LANGUAGES.map((language) => language.code), dictionaries, terms: [...terms] };
}
