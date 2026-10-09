import { describe, expect, it } from "vitest";
import { filterJobs } from "../../public/js/utils/processFilters.js";

const jobs = [
  {
    id: "match", work_arrangement: "remote", is_referral: true, expected_salary: "€95k target",
    search_period_id: "march",
    salary_min: 90000, salary_max: 120000, salary_currency: "EUR",
    technologies: [{ id: "react" }, { id: "go" }],
  },
  {
    id: "other-arrangement", work_arrangement: "hybrid", is_referral: false, expected_salary: "€95k target",
    search_period_id: "september",
    salary_min: 90000, salary_max: 120000, salary_currency: "EUR",
    technologies: [{ id: "react" }, { id: "go" }],
  },
  {
    id: "missing-tech", work_arrangement: "remote", is_referral: true, expected_salary: "€95k target",
    search_period_id: null,
    salary_min: 90000, salary_max: 120000, salary_currency: "EUR",
    technologies: [{ id: "react" }],
  },
];

const emptyFilters = {
  arrangements: [], searchPeriod: "all", expectedSalaryQuery: "", postedSalaryMin: null, postedSalaryMax: null,
  currency: "", referral: "", technologies: [],
};

describe("process filters", () => {
  it("combines arrangement alternatives with referral, target salary, posting salary and all selected technologies", () => {
    const filtered = filterJobs(jobs, {
      ...emptyFilters,
      arrangements: ["remote", "hybrid"],
      referral: "yes",
      expectedSalaryQuery: "95K",
      postedSalaryMin: 100000,
      postedSalaryMax: 130000,
      currency: "EUR",
      technologies: ["react", "go"],
    });

    expect(filtered.map((job) => job.id)).toEqual(["match"]);
  });

  it("matches a single available salary bound and rejects undisclosed salary", () => {
    const candidates = [jobs[0], { id: "unknown", salary_min: null, salary_max: null }];
    expect(filterJobs(candidates, { ...emptyFilters, postedSalaryMin: 110000 }).map((job) => job.id)).toEqual(["match"]);
  });

  it("handles empty target salary, unavailable technologies, and currency mismatches", () => {
    const candidates = [
      { id: "empty-target", expected_salary: "", salary_min: null, salary_max: 100000, salary_currency: "USD" },
      { id: "no-technologies", salary_min: 100000, salary_max: null, salary_currency: "EUR" },
    ];
    expect(filterJobs(candidates, { ...emptyFilters, expectedSalaryQuery: "95k" })).toEqual([]);
    expect(filterJobs(candidates, { ...emptyFilters, technologies: ["react"] })).toEqual([]);
    expect(filterJobs(candidates, { ...emptyFilters, currency: "EUR" }).map((job) => job.id)).toEqual(["no-technologies"]);
    expect(filterJobs(candidates, { ...emptyFilters, postedSalaryMax: 100000 }).map((job) => job.id)).toEqual(["empty-target", "no-technologies"]);
    expect(filterJobs(candidates, { ...emptyFilters, postedSalaryMin: 100001, postedSalaryMax: 100001 })).toEqual([]);
  });

  it("filters jobs by a search period or the unassigned group", () => {
    expect(filterJobs(jobs, { ...emptyFilters, searchPeriod: "march" }).map((job) => job.id)).toEqual(["match"]);
    expect(filterJobs(jobs, { ...emptyFilters, searchPeriod: "unassigned" }).map((job) => job.id)).toEqual(["missing-tech"]);
    expect(filterJobs(jobs, { ...emptyFilters, searchPeriod: "all" })).toEqual(jobs);
  });
});
