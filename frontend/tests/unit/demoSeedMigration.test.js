import { describe, expect, it } from 'vitest';
import { migrateFactorialDemoJob } from '../../public/js/utils/demoMigration.js';

const updatedSeed = {
  id: 'demo-3',
  company_name: 'Microsoft',
  avatar_seed: 'Microsoft',
  company_domain: 'microsoft.com',
  job_post_url: 'https://careers.microsoft.com',
  salary_min: 125000,
  salary_max: 165000,
  salary_type: 'limited',
  salary_currency: 'EUR',
  work_arrangement: 'hybrid',
  employment_type: 'permanent',
  is_referral: false,
  keyword_note: 'Azure developer platform • Go services • Reliability engineering at global scale',
};

describe('demo seed migration', () => {
  it('replaces the old Factorial sample while preserving interview progress and saved notes', () => {
    const job = {
      id: 'demo-3',
      company_name: 'Factorial',
      avatar_seed: 'Factorial',
      company_domain: 'factorialhr.com',
      job_post_url: 'https://careers.factorialhr.com',
      salary_min: 65000,
      salary_max: 85000,
      salary_type: 'limited',
      salary_currency: 'EUR',
      work_arrangement: 'hybrid',
      employment_type: 'permanent',
      is_referral: false,
      keyword_note: 'Multi-tenant HR workflows • Go services • Platform reliability and collaborative delivery',
      description: "Software Engineer II on Factorial's hybrid team.",
      company_overview: 'Factorial is a sample employer.',
      interview_notes: 'Keep this saved note about multi-tenant hr workflows.',
      stages: [{ status: 'completed', description: 'HR conversation at Factorial.', recruiter_agency: 'Factorial Talent Acquisition' }],
    };

    expect(migrateFactorialDemoJob([job], updatedSeed)).toBe(true);
    expect(job.company_name).toBe('Microsoft');
    expect(job.company_domain).toBe('microsoft.com');
    expect(job).toMatchObject({ salary_min: 125000, salary_max: 165000, salary_currency: 'EUR' });
    expect(job.keyword_note).toBe(updatedSeed.keyword_note);
    expect(job.description).toContain("Microsoft's hybrid team");
    expect(job.company_overview).toContain('Microsoft is');
    expect(job.interview_notes).toContain('azure developer platform');
    expect(job.stages[0]).toMatchObject({ status: 'completed', description: 'HR conversation at Microsoft.', recruiter_agency: 'Microsoft Talent Acquisition' });
  });

  it('leaves records from other companies unchanged', () => {
    const job = { id: 'demo-3', company_name: 'Contoso', company_domain: 'contoso.com' };
    expect(migrateFactorialDemoJob([job], updatedSeed)).toBe(false);
    expect(job.company_name).toBe('Contoso');
  });

  it('updates the stale salary on a demo already renamed by the earlier migration', () => {
    const job = {
      id: 'demo-3',
      company_name: 'Microsoft',
      company_domain: 'microsoft.com',
      salary_min: 65000,
      salary_max: 85000,
    };

    expect(migrateFactorialDemoJob([job], updatedSeed)).toBe(true);
    expect(job).toMatchObject({ company_name: 'Microsoft', salary_min: 125000, salary_max: 165000 });
  });
});
