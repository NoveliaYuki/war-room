import { afterEach, describe, expect, it } from 'vitest';
import { setLanguage } from '../../public/js/i18n.js';
import {
  arrangementLabel, companyText, editableCompanyName, employmentTypeLabel, isUndisclosedCompany, meetingFormatLabel, stageDescriptionText, stageName, stageTitleLabel, stageTypeLabel, statusLabel,
} from '../../public/js/utils/labels.js';

afterEach(() => setLanguage('en', { persist: false }));

describe('stored value labels', () => {
  it('translates known values and keeps stored ones untouched', () => {
    setLanguage('es', { persist: false });
    expect(arrangementLabel('on_site')).toBe('Presencial');
    expect(employmentTypeLabel('permanent_b2b')).toBe('Indefinido / B2B');
    expect(statusLabel('ongoing')).toBe('En curso');
    expect(stageTypeLabel('Offer & Decision')).toBe('Oferta y decisión');
    expect(meetingFormatLabel('phone')).toBe('Llamada telefónica');
  });

  it('falls back for unset or unknown values', () => {
    expect(arrangementLabel(null)).toBe('');
    expect(employmentTypeLabel('contract')).toBe('');
    expect(statusLabel('archived')).toBe('archived');
    expect(statusLabel(undefined)).toBe('');
    expect(stageTypeLabel('Panel')).toBe('Panel');
    expect(stageTypeLabel(null)).toBe('');
    expect(meetingFormatLabel('carrier pigeon')).toBe('Video Call');
  });

  it('prefers a custom stage title over its translated type', () => {
    setLanguage('ca', { persist: false });
    expect(stageName({ custom_title: 'System design', stage_type: 'Technical' })).toBe('System design');
    expect(stageName({ custom_title: '', stage_type: 'HR' })).toBe('RH');
    expect(stageName({ custom_title: 'Technical', stage_type: 'Technical' })).toBe('Tècnica');
  });

  it('shows the undisclosed company placeholder in the selected language', () => {
    setLanguage('es', { persist: false });
    expect(isUndisclosedCompany(' Unknown ')).toBe(true);
    expect(isUndisclosedCompany(undefined)).toBe(false);
    expect(companyText('Unknown')).toBe('Empresa desconocida');
    expect(companyText('Acme')).toBe('Acme');
    expect(editableCompanyName('unknown')).toBe('');
    expect(editableCompanyName('Acme')).toBe('Acme');
  });

  it('translates the titles and descriptions of default stages and keeps edited ones', () => {
    setLanguage('ca', { persist: false });
    expect(stageName({ custom_title: 'Recruiter Screen', stage_type: 'HR' })).toBe('Primer contacte');
    expect(stageTitleLabel('Technical Evaluation')).toBe('Avaluació tècnica');
    expect(stageTitleLabel('Offer & Decision')).toBe('Oferta i decisió');
    expect(stageTitleLabel('Panel final')).toBe('Panel final');
    expect(stageDescriptionText('Compensation proposal, equity review, and final agreement.')).toBe("Proposta de compensació, revisió de la participació accionarial i acord final.");
    expect(stageDescriptionText('Entrevista con Noa.')).toBe('Entrevista con Noa.');
  });
});
