import { describe, expect, it } from 'vitest';
import { buildWorkbook, cleanText, columnName, dateSerial, escapeXml, MAX_CELL_LENGTH, sheetName, workbookParts, worksheetXml } from '../../public/js/utils/xlsx.js';
import { readZip } from './zipReader.js';

const parse = (xml) => new DOMParser().parseFromString(xml, 'application/xml');

const sampleSheet = {
  name: "Preguntes d'entrevista",
  columns: [{ width: 20 }, { width: 12 }, { width: 30, hidden: true }],
  rows: [
    [{ kind: 'group', value: 'Group' }, { kind: 'group', value: '' }, { kind: 'group', value: 'Other' }],
    [{ kind: 'header', value: 'Name' }, { kind: 'header', value: 'Amount' }, { kind: 'header', value: 'ID' }],
    [{ kind: 'text', value: '=HYPERLINK("https://evil.example","x")' }, { kind: 'number', value: 60000 }, { kind: 'text', value: 'id-1' }],
    [{ kind: 'wrap', value: 'a < b & "c"\r\nnext' }, { kind: 'date', value: dateSerial(2026, 11, 2) }, null],
    [{ kind: 'text', value: '' }, { kind: 'number', value: null }, { kind: 'date', value: 'soon' }],
  ],
  merges: ['A1:B1'],
  frozenRows: 2,
  filterRow: 2,
};

describe('xlsx helpers', () => {
  it('names columns past Z', () => {
    expect([0, 25, 26, 27, 51, 52, 701, 702].map(columnName)).toEqual(['A', 'Z', 'AA', 'AB', 'AZ', 'BA', 'ZZ', 'AAA']);
  });

  it('converts calendar dates to Excel serial numbers', () => {
    expect(dateSerial(1900, 3, 1)).toBe(61);
    expect(dateSerial(2026, 11, 2)).toBe(46328);
    expect(dateSerial(2000, 1, 1)).toBe(36526);
  });

  it('escapes XML and strips characters XML 1.0 forbids', () => {
    expect(escapeXml(`<a href="x">Tom & Jerry's</a>`)).toBe('&lt;a href=&quot;x&quot;&gt;Tom &amp; Jerry&apos;s&lt;/a&gt;');
    expect(cleanText('a\u0000b\u0008c\u000Bd\u001Fe\tf\r\ng\rh￾i\uD800j\uDC00k😀l')).toBe('abcde\tf\ng\nhijk😀l');
    expect(cleanText(null)).toBe('');
    expect(cleanText(42)).toBe('42');
  });

  it('makes sheet names Excel accepts', () => {
    expect(sheetName('Board')).toBe('Board');
    expect(sheetName("Preguntes d'entrevista")).toBe("Preguntes d'entrevista");
    expect(sheetName('a[b]c:d*e?f/g\\h')).toBe('a b c d e f g h');
    expect(sheetName("'quoted'")).toBe('quoted');
    expect(sheetName('x'.repeat(40))).toHaveLength(31);
    expect(sheetName(' :: ')).toBe('Sheet');
  });
});

describe('worksheetXml', () => {
  const xml = worksheetXml(sampleSheet);
  const doc = parse(xml);
  const cell = (reference) => doc.querySelector(`c[r="${reference}"]`);

  it('is well-formed XML with schema-ordered children', () => {
    expect(doc.querySelector('parsererror')).toBeNull();
    expect([...doc.documentElement.children].map((child) => child.tagName)).toEqual(['sheetViews', 'sheetFormatPr', 'cols', 'sheetData', 'autoFilter', 'mergeCells']);
  });

  it('freezes the header rows and filters from the column header row', () => {
    const pane = doc.querySelector('pane');
    expect(pane.getAttribute('ySplit')).toBe('2');
    expect(pane.getAttribute('topLeftCell')).toBe('A3');
    expect(pane.getAttribute('state')).toBe('frozen');
    expect(doc.querySelector('autoFilter').getAttribute('ref')).toBe('A2:C5');
    expect(doc.querySelector('mergeCells').getAttribute('count')).toBe('1');
    expect(doc.querySelector('mergeCell').getAttribute('ref')).toBe('A1:B1');
  });

  it('sets column widths and hides columns', () => {
    const cols = [...doc.querySelectorAll('col')];
    expect(cols.map((col) => [col.getAttribute('min'), col.getAttribute('width'), col.getAttribute('hidden')])).toEqual([['1', '20', null], ['2', '12', null], ['3', '30', '1']]);
  });

  it('writes text as inline strings, never formulas', () => {
    expect(doc.querySelector('f')).toBeNull();
    expect(cell('A3').getAttribute('t')).toBe('inlineStr');
    expect(cell('A3').querySelector('is > t').textContent).toBe('=HYPERLINK("https://evil.example","x")');
    expect(cell('A3').querySelector('t').getAttribute('xml:space')).toBe('preserve');
    expect(cell('A4').querySelector('t').textContent).toBe('a < b & "c"\nnext');
  });

  it('writes numbers and dates as numeric cells with their styles', () => {
    expect(cell('B3').hasAttribute('t')).toBe(false);
    expect(cell('B3').querySelector('v').textContent).toBe('60000');
    expect(cell('B3').getAttribute('s')).toBe('3');
    expect(cell('B4').querySelector('v').textContent).toBe('46328');
    expect(cell('B4').getAttribute('s')).toBe('4');
    expect(cell('C5').getAttribute('t')).toBe('inlineStr');
    expect(cell('A4').getAttribute('s')).toBe('2');
    expect(cell('A2').getAttribute('s')).toBe('5');
    expect(cell('A1').getAttribute('s')).toBe('6');
  });

  it('omits empty cells but keeps styled blanks inside merged group headers', () => {
    expect(cell('B1').getAttribute('s')).toBe('6');
    expect(cell('B1').children).toHaveLength(0);
    expect(cell('C4')).toBeNull();
    expect(cell('A5')).toBeNull();
    expect(cell('B5')).toBeNull();
    expect([...doc.querySelectorAll('row')].map((row) => row.getAttribute('r'))).toEqual(['1', '2', '3', '4', '5']);
  });

  it(`truncates a cell at ${MAX_CELL_LENGTH} characters without splitting a surrogate pair`, () => {
    const long = (value) => parse(worksheetXml({ name: 'S', columns: [{ width: 1 }], rows: [[{ kind: 'text', value }]] })).querySelector('t').textContent;
    expect(long('x'.repeat(40000))).toHaveLength(MAX_CELL_LENGTH);
    expect(long(`${'x'.repeat(MAX_CELL_LENGTH - 1)}😀`)).toHaveLength(MAX_CELL_LENGTH - 1);
    expect(long('short')).toBe('short');
  });

  it('writes a sheet without frozen rows, filters, or merges', () => {
    const plain = parse(worksheetXml({ name: 'S', columns: [{ width: 5 }], rows: [[{ kind: 'unknown', value: 'x' }]] }));
    expect(plain.querySelector('pane')).toBeNull();
    expect(plain.querySelector('autoFilter')).toBeNull();
    expect(plain.querySelector('mergeCells')).toBeNull();
    expect(plain.querySelector('c').getAttribute('s')).toBe('0');
  });
});

describe('workbook package', () => {
  const sheets = [sampleSheet, { name: 'Board & more', columns: [{ width: 5 }], rows: [] }];

  it('lists every required part', () => {
    expect(workbookParts(sheets).map((part) => part.name)).toEqual([
      '[Content_Types].xml', '_rels/.rels', 'xl/workbook.xml', 'xl/_rels/workbook.xml.rels', 'xl/styles.xml', 'xl/worksheets/sheet1.xml', 'xl/worksheets/sheet2.xml',
    ]);
  });

  it('zips well-formed parts that reference each other', () => {
    const { files } = readZip(buildWorkbook(sheets, new Date(2026, 10, 16)));
    for (const [name, xml] of files) expect(parse(xml).querySelector('parsererror'), name).toBeNull();
    const types = parse(files.get('[Content_Types].xml'));
    expect([...types.querySelectorAll('Override')].map((item) => item.getAttribute('PartName'))).toEqual(['/xl/workbook.xml', '/xl/styles.xml', '/xl/worksheets/sheet1.xml', '/xl/worksheets/sheet2.xml']);
    expect(parse(files.get('_rels/.rels')).querySelector('Relationship').getAttribute('Target')).toBe('xl/workbook.xml');
    const workbook = parse(files.get('xl/workbook.xml'));
    expect([...workbook.querySelectorAll('sheet')].map((sheet) => [sheet.getAttribute('name'), sheet.getAttribute('r:id')])).toEqual([["Preguntes d'entrevista", 'rId1'], ['Board & more', 'rId2']]);
    const filter = workbook.querySelector('definedName');
    expect(filter.getAttribute('name')).toBe('_xlnm._FilterDatabase');
    expect(filter.getAttribute('localSheetId')).toBe('0');
    expect(filter.textContent).toBe("'Preguntes d''entrevista'!$A$2:$C$5");
    expect(workbook.querySelectorAll('definedName')).toHaveLength(1);
    const rels = parse(files.get('xl/_rels/workbook.xml.rels'));
    expect([...rels.querySelectorAll('Relationship')].map((item) => [item.getAttribute('Id'), item.getAttribute('Target')])).toEqual([['rId1', 'worksheets/sheet1.xml'], ['rId2', 'worksheets/sheet2.xml'], ['rId3', 'styles.xml']]);
  });

  it('defines the reserved fills, a yyyy-mm-dd date format, and one cell format per kind', () => {
    const styles = parse(workbookParts(sheets).find((part) => part.name === 'xl/styles.xml').data);
    expect([...styles.documentElement.children].map((child) => child.tagName)).toEqual(['numFmts', 'fonts', 'fills', 'borders', 'cellStyleXfs', 'cellXfs', 'cellStyles']);
    expect(styles.querySelector('numFmt').getAttribute('formatCode')).toBe('yyyy-mm-dd');
    expect(styles.querySelector('numFmt').getAttribute('numFmtId')).toBe('164');
    const fills = [...styles.querySelectorAll('patternFill')].map((fill) => fill.getAttribute('patternType'));
    expect(fills.slice(0, 2)).toEqual(['none', 'gray125']);
    const xfs = [...styles.querySelectorAll('cellXfs > xf')];
    expect(xfs).toHaveLength(Number(styles.querySelector('cellXfs').getAttribute('count')));
    expect(xfs[4].getAttribute('numFmtId')).toBe('164');
    expect(xfs[5].getAttribute('fontId')).toBe('1');
    expect(xfs[6].querySelector('alignment').getAttribute('horizontal')).toBe('center');
    expect(xfs[2].querySelector('alignment').getAttribute('wrapText')).toBe('1');
  });

  it('writes an empty sheet with a filter row on its header', () => {
    const xml = worksheetXml({ name: 'S', columns: [{ width: 5 }, { width: 5 }], rows: [], filterRow: 1, frozenRows: 1 });
    expect(parse(xml).querySelector('autoFilter').getAttribute('ref')).toBe('A1:B1');
  });
});
