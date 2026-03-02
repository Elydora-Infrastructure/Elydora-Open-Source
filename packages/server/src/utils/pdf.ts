/**
 * Branded PDF generator for Elydora compliance exports.
 *
 * Produces professional multi-page PDF/1.4 documents with:
 *   - Elydora logo and brand header bar
 *   - Structured metadata section
 *   - Formatted operations table with alternating row stripes
 *   - Branded footer with page numbers
 *
 * Uses only built-in PDF Type1 fonts (Helvetica, Courier) —
 * no external dependencies, works entirely in the Node.js runtime.
 */

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface PDFReportData {
  export_id: string;
  org_id: string;
  generated_at: string;
  time_range: string;
  agent_filter: string;
  type_filter: string;
  total: number;
  operations: Array<{
    operation_id: string;
    agent_id: string;
    operation_type: string;
    seq_no: number;
    chain_hash: string;
    payload_hash: string;
    created_at: number;
  }>;
}

// ---------------------------------------------------------------------------
// Page layout constants (US Letter: 612 x 792 pt)
// ---------------------------------------------------------------------------

const PW = 612;
const PH = 792;
const ML = 56;             // left margin
const MR = 56;             // right margin
const CW = PW - ML - MR;  // 500pt content width

// Header bar
const HDR_H = 56;
const HDR_Y = PH - HDR_H; // 736 — bottom edge of header

// Footer
const FTR_LINE = 44;      // y of separator line
const FTR_TEXT = 28;       // y of footer text baseline

// Content area
const CT = HDR_Y - 20;    // 716 — content top
const CB = FTR_LINE + 12; // 56  — content bottom

// Operations table
const ROW_H = 12;         // row height
const TBL_HDR = 22;       // table header + separator + gap

// Metadata section (first page only)
const META_H = 172;

// Table column x-positions
const COL_SEQ   = ML;
const COL_TIME  = ML + 38;
const COL_OPID  = ML + 142;
const COL_AGENT = ML + 296;
const COL_TYPE  = ML + 398;

// ---------------------------------------------------------------------------
// Brand colours (RGB 0‑1 scale)
// ---------------------------------------------------------------------------

const INK        = '0.071 0.071 0.071';  // #121212
const BG         = '0.918 0.918 0.898';  // #EAEAE5
const DIM        = '0.400 0.400 0.400';  // #666666
const BORDER     = '0.753 0.753 0.729';  // #C0C0BA
const HDR_SUB    = '0.620 0.620 0.600';  // subtitle on dark header
const STRIPE     = '0.955 0.955 0.942';  // subtle even-row background

// ---------------------------------------------------------------------------
// PDF drawing primitives
// ---------------------------------------------------------------------------

/** Escape special chars and strip non-ASCII for PDF text strings. */
function esc(s: string): string {
  return s
    .replace(/[^\x20-\x7E]/g, '?')
    .replace(/\\/g, '\\\\')
    .replace(/\(/g, '\\(')
    .replace(/\)/g, '\\)');
}

/** Truncate string with ".." suffix. */
function trunc(s: string, max: number): string {
  return s.length <= max ? s : s.slice(0, max - 2) + '..';
}

/** Filled rectangle. */
function fillRect(x: number, y: number, w: number, h: number, rgb: string): string {
  return `${rgb} rg ${x} ${y} ${w} ${h} re f`;
}

/** Horizontal line. */
function hline(x1: number, y: number, x2: number, rgb: string, w = 0.5): string {
  return `${rgb} RG ${w} w ${x1} ${y} m ${x2} ${y} l S`;
}

/** Text at absolute position. */
function txt(s: string, x: number, y: number, font: string, size: number, rgb: string): string {
  return `BT ${rgb} rg ${font} ${size} Tf 1 0 0 1 ${x} ${y} Tm (${esc(s)}) Tj ET`;
}

/**
 * Draw the Elydora "E" logo mark.
 * SVG source: M3 2H21V7H8V10H17V15H8V18H21V23H3V2Z  (viewBox 0 0 24 24)
 * Transform flips Y for PDF coordinate system.
 */
function drawLogo(x: number, y: number, size: number, rgb: string): string {
  const sc = size / 24;
  return [
    'q',
    `${sc} 0 0 ${-sc} ${x} ${y + size} cm`,
    `${rgb} rg`,
    '3 2 m 21 2 l 21 7 l 8 7 l 8 10 l 17 10 l 17 15 l 8 15 l 8 18 l 21 18 l 21 23 l 3 23 l h f',
    'Q',
  ].join('\n');
}

// ---------------------------------------------------------------------------
// Section renderers — each appends drawing instructions to the page array
// ---------------------------------------------------------------------------

/** Dark header bar with logo and brand text (every page). */
function renderHeader(p: string[]): void {
  // Full-width dark bar
  p.push(fillRect(0, HDR_Y, PW, HDR_H, INK));
  // Thin accent line at bottom edge
  p.push(hline(0, HDR_Y, PW, BG, 1.5));

  // Logo mark
  const sz = 30;
  const lx = ML;
  const ly = HDR_Y + (HDR_H - sz) / 2; // vertically centred
  p.push(drawLogo(lx, ly, sz, BG));

  // Brand name
  const tx = lx + sz + 12;
  p.push(txt('ELYDORA', tx, ly + sz - 16, '/F1', 18, BG));
  // Subtitle
  p.push(txt('COMPLIANCE EXPORT REPORT', tx, ly + 2, '/F2', 8, HDR_SUB));
}

/** Footer with confidentiality note and page number (every page). */
function renderFooter(p: string[], pageNum: number, totalPages: number, exportId: string): void {
  p.push(hline(ML, FTR_LINE, PW - MR, BORDER, 0.5));
  p.push(txt('CONFIDENTIAL', ML, FTR_TEXT, '/F2', 7, DIM));
  p.push(txt(`Export ${exportId.slice(0, 8)}`, PW / 2 - 28, FTR_TEXT, '/F3', 7, DIM));
  p.push(txt(`Page ${pageNum} of ${totalPages}`, PW - MR - 56, FTR_TEXT, '/F2', 7, DIM));
}

/** Metadata key-value section (first page only). Returns new y position. */
function renderMetadata(p: string[], data: PDFReportData): number {
  let y = CT;

  // Section title
  p.push(txt('EXPORT DETAILS', ML, y, '/F1', 10, INK));
  y -= 6;
  p.push(hline(ML, y, PW - MR, BORDER, 0.5));
  y -= 18;

  // Key-value rows
  const valX = ML + 112;
  const fields: [string, string][] = [
    ['Export ID',      data.export_id],
    ['Organization',   data.org_id],
    ['Generated',      data.generated_at],
    ['Time Range',     data.time_range],
    ['Agent Filter',   data.agent_filter],
    ['Type Filter',    data.type_filter],
    ['Total Records',  String(data.total)],
  ];
  for (const [label, value] of fields) {
    p.push(txt(label, ML, y, '/F2', 8.5, DIM));
    p.push(txt(value, valX, y, '/F3', 8.5, INK));
    y -= 16;
  }

  y -= 8;

  // Operations section title
  p.push(txt('OPERATIONS', ML, y, '/F1', 10, INK));
  y -= 6;
  p.push(hline(ML, y, PW - MR, BORDER, 0.5));
  y -= 4;

  return y;
}

/** Table column headers. Returns y of first data row. */
function renderTableHeader(p: string[], y: number): number {
  const hy = y - 14;
  p.push(txt('#', COL_SEQ, hy, '/F1', 7.5, DIM));
  p.push(txt('TIMESTAMP', COL_TIME, hy, '/F1', 7.5, DIM));
  p.push(txt('OPERATION ID', COL_OPID, hy, '/F1', 7.5, DIM));
  p.push(txt('AGENT', COL_AGENT, hy, '/F1', 7.5, DIM));
  p.push(txt('TYPE', COL_TYPE, hy, '/F1', 7.5, DIM));

  const sy = hy - 5;
  p.push(hline(ML, sy, PW - MR, BORDER, 0.3));

  return sy - 3;
}

/** Single operation data row. */
function renderRow(
  p: string[],
  y: number,
  op: PDFReportData['operations'][number],
  index: number,
): void {
  // Alternating stripe background
  if (index % 2 === 0) {
    p.push(fillRect(ML - 4, y - 3, CW + 8, ROW_H, STRIPE));
  }

  const ts = new Date(op.created_at).toISOString().slice(0, 19).replace('T', ' ');

  p.push(txt(String(op.seq_no).padStart(4), COL_SEQ, y, '/F3', 8, DIM));
  p.push(txt(ts, COL_TIME, y, '/F3', 8, INK));
  p.push(txt(trunc(op.operation_id, 30), COL_OPID, y, '/F3', 8, INK));
  p.push(txt(trunc(op.agent_id, 18), COL_AGENT, y, '/F3', 8, INK));
  p.push(txt(trunc(op.operation_type, 20), COL_TYPE, y, '/F3', 8, INK));
}

/** End-of-report note (last page, if space allows). */
function renderEndNote(p: string[], y: number): void {
  y -= 20;
  p.push(hline(ML, y, PW - MR, BORDER, 0.3));
  y -= 18;
  p.push(txt('End of Report', ML, y, '/F1', 9, DIM));
  y -= 14;
  p.push(txt(
    'Full cryptographic chain and payload hashes are available in JSON export format.',
    ML, y, '/F2', 7.5, DIM,
  ));
  y -= 12;
  p.push(txt(
    'Elydora  -  The Responsibility Layer for AI Agents',
    ML, y, '/F2', 7.5, DIM,
  ));
}

// ---------------------------------------------------------------------------
// Main export
// ---------------------------------------------------------------------------

export function buildBrandedPDF(data: PDFReportData): Uint8Array {
  const ops = data.operations;
  const totalOps = ops.length;

  // Calculate pagination
  const firstPageArea = CT - META_H - CB;        // ~488pt
  const opsFirstPage = Math.max(0, Math.floor((firstPageArea - TBL_HDR) / ROW_H));
  const fullPageArea = CT - CB;                   // ~660pt
  const opsPerPage = Math.floor((fullPageArea - TBL_HDR) / ROW_H);

  const totalPages = totalOps <= opsFirstPage
    ? 1
    : 1 + Math.ceil((totalOps - opsFirstPage) / Math.max(1, opsPerPage));

  // Build content streams (one per page)
  const streams: string[] = [];
  let opIdx = 0;

  for (let pg = 0; pg < totalPages; pg++) {
    const p: string[] = [];

    renderHeader(p);

    let y: number;
    if (pg === 0) {
      y = renderMetadata(p, data);
    } else {
      y = CT;
    }

    y = renderTableHeader(p, y);

    const maxOps = pg === 0 ? opsFirstPage : opsPerPage;
    const endIdx = Math.min(opIdx + maxOps, totalOps);

    while (opIdx < endIdx) {
      renderRow(p, y, ops[opIdx]!, opIdx);
      y -= ROW_H;
      opIdx++;
    }

    // End note on last page if enough space
    if (pg === totalPages - 1 && y > CB + 70) {
      renderEndNote(p, y);
    }

    renderFooter(p, pg + 1, totalPages, data.export_id);
    streams.push(p.join('\n'));
  }

  return assemblePDF(streams);
}

// ---------------------------------------------------------------------------
// PDF document assembly
// ---------------------------------------------------------------------------

function assemblePDF(pageStreams: string[]): Uint8Array {
  const objContents: string[] = [];

  // Obj 1: Catalog
  objContents.push('<< /Type /Catalog /Pages 2 0 R >>');

  // Obj 2: Pages (placeholder — patched after page objects are created)
  objContents.push('');

  // Obj 3–5: Built-in fonts
  objContents.push('<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold >>'); // /F1
  objContents.push('<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>');      // /F2
  objContents.push('<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>');        // /F3

  const fontRes = '/F1 3 0 R /F2 4 0 R /F3 5 0 R';

  // Content stream + page object for each page
  const pageObjNums: number[] = [];

  for (const stream of pageStreams) {
    const csNum = objContents.length + 1;
    objContents.push(`<< /Length ${stream.length} >>\nstream\n${stream}\nendstream`);

    const pgNum = objContents.length + 1;
    objContents.push(
      `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 ${PW} ${PH}] ` +
        `/Contents ${csNum} 0 R /Resources << /Font << ${fontRes} >> >> >>`,
    );
    pageObjNums.push(pgNum);
  }

  // Patch Pages object
  const kids = pageObjNums.map((n) => `${n} 0 R`).join(' ');
  objContents[1] = `<< /Type /Pages /Kids [${kids}] /Count ${pageStreams.length} >>`;

  // Assemble byte stream
  const header = '%PDF-1.4\n';
  const parts: string[] = [header];
  const offsets: number[] = [];
  let pos = header.length;

  for (let i = 0; i < objContents.length; i++) {
    offsets.push(pos);
    const obj = `${i + 1} 0 obj\n${objContents[i]}\nendobj\n`;
    parts.push(obj);
    pos += obj.length;
  }

  // Cross-reference table
  const xrefPos = pos;
  const numEntries = objContents.length + 1;
  parts.push(`xref\n0 ${numEntries}\n`);
  parts.push('0000000000 65535 f \n');
  for (const off of offsets) {
    parts.push(`${String(off).padStart(10, '0')} 00000 n \n`);
  }

  // Trailer
  parts.push(
    `trailer\n<< /Size ${numEntries} /Root 1 0 R >>\nstartxref\n${xrefPos}\n%%EOF\n`,
  );

  return new TextEncoder().encode(parts.join(''));
}
