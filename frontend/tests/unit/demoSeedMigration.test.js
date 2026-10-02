import { describe, expect, it } from 'vitest';
import { migrateLegacyDemoJob } from '../../public/js/utils/demoMigration.js';

const updatedSeed = {
  id: 'demo-3',
  company_name: 'NVIDIA',
  avatar_seed: 'NVIDIA',
  company_domain: 'nvidia.com',
  job_post_url: 'https://careers.nvidia.com',
  salary_min: 125000,
  salary_max: 165000,
  salary_type: 'limited',
  salary_currency: 'EUR',
  work_arrangement: 'hybrid',
  employment_type: 'permanent',
  is_referral: false,
  keyword_note: 'GPU computing platform • Go services • Reliability engineering at global scale',
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

    expect(migrateLegacyDemoJob([job], updatedSeed)).toBe(true);
    expect(job.company_name).toBe('NVIDIA');
    expect(job.company_domain).toBe('nvidia.com');
    expect(job).toMatchObject({ salary_min: 125000, salary_max: 165000, salary_currency: 'EUR' });
    expect(job.keyword_note).toBe(updatedSeed.keyword_note);
    expect(job.description).toContain("NVIDIA's hybrid team");
    expect(job.company_overview).toContain('NVIDIA is');
    expect(job.interview_notes).toContain('gpu computing platform');
    expect(job.stages[0]).toMatchObject({ status: 'completed', description: 'HR conversation at NVIDIA.', recruiter_agency: 'NVIDIA Talent Acquisition' });
  });

  it('leaves records from other companies unchanged', () => {
    const job = { id: 'demo-3', company_name: 'Contoso', company_domain: 'contoso.com' };
    expect(migrateLegacyDemoJob([job], updatedSeed)).toBe(false);
    expect(job.company_name).toBe('Contoso');
  });

  it('replaces the earlier duplicate Microsoft demo role and preserves saved progress', () => {
    const job = {
      id: 'demo-3',
      company_name: 'Microsoft',
      company_domain: 'microsoft.com',
      salary_min: 125000,
      salary_max: 165000,
      keyword_note: 'Azure developer platform • Go services • Reliability engineering at global scale',
      description: "Software Engineer II on Microsoft's hybrid team.",
      stages: [{ status: 'current', description: 'Technical conversation at Microsoft.', recruiter_agency: 'Microsoft Talent Acquisition' }],
    };

    expect(migrateLegacyDemoJob([job], updatedSeed)).toBe(true);
    expect(job).toMatchObject({ company_name: 'NVIDIA', company_domain: 'nvidia.com', salary_min: 125000, salary_max: 165000 });
    expect(job.keyword_note).toBe(updatedSeed.keyword_note);
    expect(job.description).toContain("NVIDIA's hybrid team");
    expect(job.stages[0]).toMatchObject({ status: 'current', description: 'Technical conversation at NVIDIA.', recruiter_agency: 'NVIDIA Talent Acquisition' });
  });
});
