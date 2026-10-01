function updateSeedFields(job, updatedSeed) {
  for (const field of [
    "company_name", "avatar_seed", "company_domain", "job_post_url",
    "salary_min", "salary_max", "salary_type", "salary_currency",
    "work_arrangement", "employment_type", "is_referral",
  ]) {
    job[field] = updatedSeed[field];
  }
  if (job.keyword_note === "Multi-tenant HR workflows • Go services • Platform reliability and collaborative delivery") {
    job.keyword_note = updatedSeed.keyword_note;
  }
}

function updateJobText(job, updatedSeed) {
  for (const field of ["description", "company_overview"]) {
    if (typeof job[field] === "string") job[field] = job[field].replaceAll("Factorial", "Microsoft");
  }
  const oldKeyword = "multi-tenant hr workflows";
  const newKeyword = updatedSeed.keyword_note.split(" • ")[0].toLowerCase();
  for (const field of ["interview_notes", "reasons_to_change", "experience_notes"]) {
    if (typeof job[field] === "string") job[field] = job[field].replaceAll(oldKeyword, newKeyword);
  }
}

function updateStageText(job) {
  for (const stage of job.stages || []) {
    if (typeof stage.description === "string") stage.description = stage.description.replaceAll("Factorial", "Microsoft");
    if (stage.recruiter_agency === "Factorial Talent Acquisition") stage.recruiter_agency = "Microsoft Talent Acquisition";
  }
}

/** Updates the saved demo's retired Factorial sample while preserving interview progress. */
export function migrateFactorialDemoJob(records, updatedSeed) {
  if (!Array.isArray(records) || !updatedSeed) return false;
  const job = records.find((record) => {
    if (record.id !== "demo-3") return false;
    const wasFactorial = record.company_name === "Factorial" && record.company_domain === "factorialhr.com";
    const hasStaleMicrosoftSalary = record.company_name === "Microsoft"
      && record.company_domain === "microsoft.com" && record.salary_min === 65000 && record.salary_max === 85000;
    return wasFactorial || hasStaleMicrosoftSalary;
  });
  if (!job) return false;

  updateSeedFields(job, updatedSeed);
  updateJobText(job, updatedSeed);
  updateStageText(job);
  return true;
}
