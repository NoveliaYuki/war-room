
import { describe, it, expect } from 'vitest';
import { generateAvatarSvg, renderCompanyAvatar } from '../../public/js/avatar.js';

describe('avatar', () => {
  it('renders normal company name', () => {
    const avatar = renderCompanyAvatar('Example Company');
    expect(avatar).toContain('<svg');
    expect(avatar).toContain('E');
  });
  it('renders unknown company', () => {
    const avatar = renderCompanyAvatar('');
    expect(avatar).toContain('<svg');
    expect(avatar).toContain('viewBox="0 0 64 64"');
  });
  it('supports different sizes', () => {
    const avatar = renderCompanyAvatar('Example Company', 'example.com', 48);
    expect(avatar).toContain('width="48"');
    expect(avatar).toContain('height="48"');
  });
  it('handles company domain provided vs not', () => {
    const withDomain = renderCompanyAvatar('Example Company', 'seed', 64, 'example.com');
    const withoutDomain = renderCompanyAvatar('Example Company');
    expect(withDomain).toContain('<img');
    expect(withoutDomain).toContain('<img');
  });

  it('covers every deterministic geometric shape branch', () => {
    for (let seed = 0; seed < 128; seed += 1) {
      const svg = generateAvatarSvg(String(seed), 32);
      expect(svg).toContain('viewBox="0 0 32 32"');
      expect(svg).toMatch(/<(?:circle|rect|polygon)/);
    }
    expect(generateAvatarSvg('', 24)).toContain('viewBox="0 0 24 24"');
  });

  it('encodes logo query data and HTML attributes', () => {
    const markup = renderCompanyAvatar('Acme & Sons', 'seed', 64, 'acme.test/path?a=1&b=2', 'id&x');
    expect(markup).toContain('company=Acme%20%26%20Sons');
    expect(markup).toContain('domain=acme.test%2Fpath%3Fa%3D1%26b%3D2');
    expect(markup).toContain('job_id=id%26x');
    expect(markup).toContain('alt="Acme &amp; Sons"');
    expect(renderCompanyAvatar(' unknown ', '', 48)).toContain('viewBox="0 0 48 48"');
  });
});
