/** Filters saved processes using every selected filter category. */
export function filterJobs(jobs, filters) {
  return jobs.filter((job) => matchesArrangement(job, filters)
    && matchesReferral(job, filters.referral)
    && matchesExpectedSalary(job, filters.expectedSalaryQuery)
    && matchesSalaryRange(job.salary_min, job.salary_max, filters.postedSalaryMin, filters.postedSalaryMax)
    && (!filters.currency || job.salary_currency === filters.currency)
    && matchesTechnologies(job, filters.technologies));
}

function matchesArrangement(job, filters) {
  return !filters.arrangements.length || filters.arrangements.includes(job.work_arrangement);
}

function matchesReferral(job, referral) {
  return !referral || job.is_referral === (referral === "yes");
}

function matchesExpectedSalary(job, query) {
  return !query || (job.expected_salary || "").toLocaleLowerCase().includes(query.toLocaleLowerCase());
}

function matchesTechnologies(job, selectedIDs) {
  const jobIDs = new Set((job.technologies || []).map((technology) => technology.id));
  return selectedIDs.every((id) => jobIDs.has(id));
}

function matchesSalaryRange(minimum, maximum, requestedMin, requestedMax) {
  if (requestedMin === null && requestedMax === null) return true;
  if (minimum === null && maximum === null) return false;
  const lowest = minimum ?? maximum;
  const highest = maximum ?? minimum;
  return (requestedMin === null || highest >= requestedMin) && (requestedMax === null || lowest <= requestedMax);
}
