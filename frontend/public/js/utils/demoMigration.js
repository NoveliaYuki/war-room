function updateSeedFields(job, updatedSeed) {
  for (const field of [
    "company_name", "avatar_seed", "company_domain", "job_post_url",
    "salary_min", "salary_max", "salary_type", "salary_currency",
    "work_arrangement", "employment_type", "is_referral",
  ]) {
    job[field] = updatedSeed[field];
  }
  if (job.keyword_note === "Multi-tenant HR workflows • Go services • Platform reliability and collaborative delivery"
    || job.keyword_note === "Azure developer platform • Go services • Reliability engineering at global scale") {
    job.keyword_note = updatedSeed.keyword_note;
  }
}

function updateJobText(job, updatedSeed, oldCompany) {
  for (const field of ["description", "company_overview"]) {
    if (typeof job[field] === "string") job[field] = job[field].replaceAll(oldCompany, updatedSeed.company_name);
  }
  const oldKeyword = oldCompany === "Factorial" ? "multi-tenant hr workflows" : "azure developer platform";
  const newKeyword = updatedSeed.keyword_note.split(" • ")[0].toLowerCase();
  for (const field of ["interview_notes", "reasons_to_change", "experience_notes"]) {
    if (typeof job[field] === "string") job[field] = job[field].replaceAll(oldKeyword, newKeyword);
  }
}

function updateStageText(job, oldCompany, newCompany) {
  for (const stage of job.stages || []) {
    if (typeof stage.description === "string") stage.description = stage.description.replaceAll(oldCompany, newCompany);
    if (stage.recruiter_agency === `${oldCompany} Talent Acquisition`) stage.recruiter_agency = `${newCompany} Talent Acquisition`;
  }
}

/** Updates the saved demo's retired sample employer while preserving interview progress. */
export function migrateLegacyDemoJob(records, updatedSeed) {
  if (!Array.isArray(records) || !updatedSeed) return false;
  const job = records.find((record) => {
    if (record.id !== "demo-3") return false;
    const wasFactorial = record.company_name === "Factorial" && record.company_domain === "factorialhr.com";
    const isDuplicateMicrosoft = record.company_name === "Microsoft" && record.company_domain === "microsoft.com";
    return wasFactorial || isDuplicateMicrosoft;
  });
  if (!job) return false;

  const oldCompany = job.company_name;
  updateSeedFields(job, updatedSeed);
  updateJobText(job, updatedSeed, oldCompany);
  updateStageText(job, oldCompany, updatedSeed.company_name);
  return true;
}
