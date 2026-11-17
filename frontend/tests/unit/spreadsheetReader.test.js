import { describe, expect, it } from 'vitest';
import { buildBoardWorkbook } from '../../public/js/utils/boardWorkbook.js';
import {
  decodeText, detectDelimiter, MAX_SHEET_ROWS, MAX_SPREADSHEET_BYTES, parseDelimited, parseXml, readSpreadsheet, readWorkbook,
} from '../../public/js/utils/spreadsheetReader.js';
import { buildZip } from './zipBuilder.js';

const MAIN = 'http://schemas.openxmlformats.org/spreadsheetml/2006/main';
const REL = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships';
const PACKAGE_REL = 'http://schemas.openxmlformats.org/package/2006/relationships';

/** Builds a workbook the way another app might save it: prefixed namespaces, shared strings, and a 1904 date system. */
function appWorkbook(dataSheet, { date1904 = '1', rootRels = true } = {}) {
  const files = [
    { name: 'xl/workbook.xml', data: `<x:workbook xmlns:x="${MAIN}" xmlns:rel="${REL}"><x:workbookPr date1904="${date1904}"/><x:sheets>`
      + '<x:sheet name="Notes" sheetId="1" rel:id="rId2"/><x:sheet name="Data" sheetId="2" rel:id="rId1"/><x:sheet name="Chart" sheetId="3" rel:id="rId9"/></x:sheets></x:workbook>' },
    { name: 'xl/_rels/workbook.xml.rels', data: `<Relationships xmlns="${PACKAGE_REL}">`
      + `<Relationship Id="rId1" Type="${REL}/worksheet" Target="/xl/worksheets/data.xml"/>`
      + `<Relationship Id="rId2" Type="${REL}/worksheet" Target="../xl/./worksheets/notes.xml"/>`
      + `<Relationship Id="rId3" Type="${REL}/sharedStrings" Target="sharedStrings.xml"/>`
      + `<Relationship Id="rId9" Type="${REL}/chartsheet" Target="charts/missing.xml"/></Relationships>` },
    { name: 'xl/sharedStrings.xml', data: `<sst xmlns="${MAIN}"><si><t>Company</t></si><si><r><t>Ac</t></r><r><t xml:space="preserve">me </t></r><rPh><t>アクメ</t></rPh></si></sst>` },
    { name: 'xl/worksheets/notes.xml', data: `<worksheet xmlns="${MAIN}"><sheetData/></worksheet>` },
    { name: 'xl/worksheets/data.xml', data: dataSheet },
  ];
  if (rootRels) files.unshift({ name: '_rels/.rels', data: `<Relationships xmlns="${PACKAGE_REL}"><Relationship Id="rId1" Type="${REL}/officeDocument" Target="xl/workbook.xml"/></Relationships>` });
  return buildZip(files);
}

const DATA_SHEET = `<worksheet xmlns="${MAIN}"><sheetData>`
  + '<row r="1"><c r="A1" t="s"><v>0</v></c><c r="C1" t="inlineStr"><is><t>Role</t></is></c></row>'
  + '<row r="3"><c r="A3" t="s"><v>1</v></c><c t="b"><v>1</v></c><c t="str"><f>A1</f><v>Company</v></c><c t="e"><v>#N/A</v></c>'
  + '<c r="E3" t="d"><v>2026-11-02T00:00:00</v></c><c r="F3"><v>45000.5</v></c><c r="G3"><v>  </v></c><c r="H3" t="inlineStr"><is><t></t></is></c><c r="I3"/><c r="IW3"><v>9</v></c></row>'
  + '<row><c><v>7</v></c></row><row r="20000"><c r="A20000" s="1"/></row>'
  + '</sheetData></worksheet>';

describe('readWorkbook', () => {
  it('reads every cell type other apps write, in tab order', async () => {
    const workbook = await readWorkbook(appWorkbook(DATA_SHEET));
    expect(workbook.dateOffset).toBe(1462);
    expect(workbook.sheets.map((sheet) => sheet.name)).toEqual(['Notes', 'Data']);
    expect(workbook.sheets[0].rows).toEqual([]);
    expect(workbook.sheets[1].rows).toEqual([
      ['Company', null, 'Role'],
      [],
      ['Acme ', true, 'Company', null, '2026-11-02T00:00:00', 45000.5, '  '],
      [7],
    ]);
  });

  it('finds the workbook without package relationships and reads the 1900 date system', async () => {
    const workbook = await readWorkbook(appWorkbook(DATA_SHEET, { date1904: 'false', rootRels: false }));
    expect(workbook.dateOffset).toBe(0);
    expect(workbook.sheets).toHaveLength(2);
  });

  it('reads a workbook War Room exported', async () => {
    const workbook = await readWorkbook(buildBoardWorkbook([{ id: 'job-1', company_name: 'Acme', position_title: 'Engineer', status: 'waiting', salary_min: 60000 }], [], new Map()));
    expect(workbook.sheets.map((sheet) => sheet.name)).toEqual(['Board', 'Interview questions']);
    const [, headers, row] = workbook.sheets[0].rows;
    expect(headers[0]).toBe('1.1 Company');
    expect(headers[26]).toBe('Process ID');
    expect(row[0]).toBe('Acme');
    expect(row[5]).toBe(60000);
    expect(row[26]).toBe('job-1');
  });

  it('rejects a sheet with too many rows and reads shared strings only when present', async () => {
    const tooTall = `<worksheet xmlns="${MAIN}"><sheetData><row r="${MAX_SHEET_ROWS + 1}"><c t="inlineStr"><is><t>x</t></is></c></row></sheetData></worksheet>`;
    await expect(readWorkbook(appWorkbook(tooTall))).rejects.toMatchObject({ code: 'tooLarge' });
    const noStrings = buildZip([
      { name: 'xl/workbook.xml', data: `<workbook xmlns="${MAIN}" xmlns:r="${REL}"><sheets><sheet sheetId="1" r:id="rId1"/></sheets></workbook>` },
      { name: 'xl/_rels/workbook.xml.rels', data: `<Relationships xmlns="${PACKAGE_REL}"><Relationship Id="rId1" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="${REL}/sharedStrings" Target="missing.xml"/></Relationships>` },
      { name: 'xl/worksheets/sheet1.xml', data: `<worksheet xmlns="${MAIN}"><sheetData><row><c t="s"><v>4</v></c><c t="x"><v>raw</v></c><c><v>text</v></c></row></sheetData></worksheet>` },
    ]);
    expect((await readWorkbook(noStrings)).sheets).toEqual([{ name: '', rows: [[null, 'raw', 'text']] }]);
  });
});

describe('parseXml', () => {
  it('refuses document type declarations and malformed XML', () => {
    expect(() => parseXml('<!DOCTYPE x [<!ENTITY a "b">]><x>&a;</x>')).toThrow('unreadable');
    expect(() => parseXml('<a><b></a>')).toThrow('unreadable');
    expect(parseXml('<a/>').documentElement.localName).toBe('a');
  });
});

describe('delimited text', () => {
  it('detects commas, semicolons, and tabs from the first line', () => {
    expect(detectDelimiter('a,b,c\n1;2;3;4;5')).toBe(',');
    expect(detectDelimiter('"a,b";c;d')).toBe(';');
    expect(detectDelimiter('a\tb')).toBe('\t');
    expect(detectDelimiter('single')).toBe(',');
  });

  it('reads quoted separators, doubled quotes, line breaks, and empty cells', () => {
    const text = 'Company;Role;Notes\r\n"Acme; Inc.";"Senior ""Platform"" Engineer";"line 1\r\nline 2"\n;;\r"last"';
    expect(parseDelimited(text)).toEqual([
      ['Company', 'Role', 'Notes'],
      ['Acme; Inc.', 'Senior "Platform" Engineer', 'line 1\nline 2'],
      [null, null, null],
      ['last'],
    ]);
    expect(parseDelimited('a\tb\n', '\t')).toEqual([['a', 'b']]);
    expect(parseDelimited('a,b,')).toEqual([['a', 'b', null]]);
    expect(parseDelimited('ab"c,"d"e')).toEqual([['ab"c', '"d"e']]);
    expect(parseDelimited('')).toEqual([]);
  });

  it('stops at the row limit', () => {
    expect(() => parseDelimited('x\n'.repeat(MAX_SHEET_ROWS + 1))).toThrow('tooLarge');
  });

  it('decodes UTF-8, UTF-16, and Windows-1252 text', () => {
    expect(decodeText(new Uint8Array([0xef, 0xbb, 0xbf, 0x43, 0xc3, 0xa9]))).toBe('Cé');
    expect(decodeText(new Uint8Array([0x43, 0xe9]))).toBe('Cé');
    expect(decodeText(new Uint8Array([0xff, 0xfe, 0x43, 0x00, 0xe9, 0x00]))).toBe('Cé');
    expect(decodeText(new Uint8Array([0xfe, 0xff, 0x00, 0x43, 0x00, 0xe9]))).toBe('Cé');
  });
});

describe('readSpreadsheet', () => {
  const file = (bytes, name = 'board.csv') => ({ name, size: bytes.length, arrayBuffer: async () => bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.length) });

  it('reads a workbook or a delimited text file', async () => {
    expect((await readSpreadsheet(file(appWorkbook(DATA_SHEET), 'board.xlsx'))).sheets).toHaveLength(2);
    expect(await readSpreadsheet(file(new TextEncoder().encode('Company,Role\nAcme,Engineer')))).toEqual({
      dateOffset: 0, sheets: [{ name: 'board.csv', rows: [['Company', 'Role'], ['Acme', 'Engineer']] }],
    });
    expect((await readSpreadsheet({ ...file(new Uint8Array([0xff, 0xfe, 0x41, 0x00])), name: undefined })).sheets[0]).toEqual({ name: '', rows: [['A']] });
  });

  it('rejects binary files and files over the size limit', async () => {
    await expect(readSpreadsheet(file(new Uint8Array([0xd0, 0xcf, 0x11, 0xe0, 0, 0])))).rejects.toMatchObject({ code: 'unreadable' });
    await expect(readSpreadsheet({ size: MAX_SPREADSHEET_BYTES + 1 })).rejects.toMatchObject({ code: 'tooLarge' });
  });
});
