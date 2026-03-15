/**
 * ============================================================================
 * 💎 PROFESSIONEEL DIENSTEN DASHBOARD (V3.0 - 2026)
 * ============================================================================
 * - "Volgende Dienst" widget bovenaan
 * - Week-overzicht (komende 7 dagen)
 * - Statistieken: Vroeg / Laat / Dienst verdeling, totale uren
 * - Geschiedenis sectie
 * ============================================================================
 * ⚠️ onOpen() is verplaatst naar Code.gs om conflict te voorkomen.
 * Gebruik 🚀 Master Tools → 📊 Bouw Diensten Dashboard
 */

const DB_CONFIG = {
  DATA_SHEET:  'DienstenData',
  DASH_SHEET:  'Dashboard',
  // Kleuren
  C_BG:        '#0f0f17',   // donker achtergrond
  C_CARD:      '#1a1a2e',   // kaart achtergrond
  C_ACCENT:    '#f59e0b',   // amber (= Homeapp kleur)
  C_GREEN:     '#10b981',
  C_BLUE:      '#3b82f6',
  C_RED:       '#ef4444',
  C_ORANGE:    '#f97316',
  C_TEXT:      '#e2e8f0',
  C_MUTED:     '#64748b',
  C_VROEG:     '#fde68a',   // geel voor vroeg dienst
  C_VROEG_BG:  '#78350f',
  C_LAAT:      '#fca5a5',   // rood voor laat dienst
  C_LAAT_BG:   '#7f1d1d',
  C_DIENST:    '#93c5fd',   // blauw voor normale dienst
  C_DIENST_BG: '#1e3a5f',
};

// Kolomindex helpers
const HDR = {};

// ============================================================================
// HOOFDFUNCTIE
// ============================================================================

function buildOptimizedDashboard() {
  const ss = SpreadsheetApp.getActiveSpreadsheet();
  const dataSheet = ss.getSheetByName(DB_CONFIG.DATA_SHEET);

  if (!dataSheet) {
    SpreadsheetApp.getUi().alert('❌ DienstenData tabblad niet gevonden. Voer eerst een Rooster Sync uit.');
    return;
  }

  // Haal headers op en bouw HDR index
  const headers = dataSheet.getRange(1, 1, 1, dataSheet.getLastColumn()).getValues()[0];
  headers.forEach((h, i) => { HDR[h] = i; });

  // Haal alle data op (excl. header)
  const allData = dataSheet.getLastRow() > 1
    ? dataSheet.getRange(2, 1, dataSheet.getLastRow() - 1, headers.length).getValues()
    : [];

  const now = new Date();
  const todayMidnight = new Date(now); todayMidnight.setHours(0,0,0,0);
  const in7Days = new Date(todayMidnight); in7Days.setDate(in7Days.getDate() + 7);
  const in30Days = new Date(todayMidnight); in30Days.setDate(in30Days.getDate() + 30);

  // Segmenteer data
  const upcoming    = allData.filter(r => r[HDR['Status']] === 'Opkomend');
  const bezig       = allData.filter(r => r[HDR['Status']] === 'Bezig');
  const thisWeek    = allData.filter(r => {
    const d = new Date(r[HDR['Start Datum']]);
    return d >= todayMidnight && d <= in7Days && r[HDR['Status']] !== 'VERWIJDERD';
  });
  const history     = allData.filter(r => r[HDR['Status']] === 'Gedraaid')
    .slice(0, 10); // laatste 10

  // Statistieken (afgelopen 30 dagen + komende 30 dagen)
  const relevant = allData.filter(r => {
    const d = new Date(r[HDR['Start Datum']]);
    return d >= new Date(todayMidnight.getTime() - 30*86400000);
  });
  const stats = _computeStats(relevant);

  // Bouw dashboard tab
  let dash = ss.getSheetByName(DB_CONFIG.DASH_SHEET);
  if (dash) ss.deleteSheet(dash);
  dash = ss.insertSheet(DB_CONFIG.DASH_SHEET, 0);

  // Stel kolombreedte in
  dash.setColumnWidth(1, 20);   // marge
  dash.setColumnWidth(2, 160);
  dash.setColumnWidth(3, 120);
  dash.setColumnWidth(4, 100);
  dash.setColumnWidth(5, 100);
  dash.setColumnWidth(6, 120);
  dash.setColumnWidth(7, 100);
  dash.setColumnWidth(8, 80);
  dash.setColumnWidth(9, 20);   // marge

  // Donkere achtergrond voor hele sheet
  dash.getRange(1, 1, 100, 9).setBackground(DB_CONFIG.C_BG);
  dash.setHiddenGridlines(true);

  let row = 2;

  // ── HEADER ────────────────────────────────────────────────────────────────
  row = _writeHeader(dash, row, now);
  row += 1;

  // ── VOLGENDE DIENST (hero card) ───────────────────────────────────────────
  const nextDienst = bezig[0] || upcoming[0] || null;
  row = _writeNextDienstCard(dash, row, nextDienst);
  row += 1;

  // ── STATISTIEKEN ──────────────────────────────────────────────────────────
  row = _writeStats(dash, row, stats);
  row += 1;

  // ── KOMENDE WEEK ──────────────────────────────────────────────────────────
  row = _writeSectionHeader(dash, row, '📅 KOMENDE 7 DAGEN', thisWeek.length + ' diensten');
  row = _writeScheduleTable(dash, row, thisWeek.length > 0 ? thisWeek : null, 'week');
  row += 1;

  // ── AANKOMENDE DIENSTEN (alle) ─────────────────────────────────────────────
  row = _writeSectionHeader(dash, row, '🔜 ALLE AANKOMENDE DIENSTEN', upcoming.length + ' gepland');
  row = _writeScheduleTable(dash, row, upcoming.length > 0 ? upcoming.slice(0, 15) : null, 'full');
  row += 1;

  // ── GESCHIEDENIS ──────────────────────────────────────────────────────────
  row = _writeSectionHeader(dash, row, '📋 RECENTE GESCHIEDENIS', 'Laatste 10 diensten');
  row = _writeScheduleTable(dash, row, history.length > 0 ? history : null, 'history');

  dash.setFrozenRows(1);
  ss.setActiveSheet(dash);

  safeToast('Dashboard', '✅ Dashboard bijgewerkt!', 5);
}

// ============================================================================
// HEADER
// ============================================================================

function _writeHeader(dash, row, now) {
  const dateStr = Utilities.formatDate(now, Session.getScriptTimeZone(), "EEEE d MMMM yyyy · HH:mm");

  dash.getRange(row, 2, 1, 7).merge()
    .setValue('🏠  Diensten Overzicht')
    .setFontSize(20).setFontWeight('bold').setFontColor(DB_CONFIG.C_ACCENT)
    .setBackground(DB_CONFIG.C_BG).setVerticalAlignment('middle');

  dash.setRowHeight(row, 40);
  row++;

  dash.getRange(row, 2, 1, 7).merge()
    .setValue('Bijgewerkt op ' + dateStr)
    .setFontSize(9).setFontColor(DB_CONFIG.C_MUTED)
    .setBackground(DB_CONFIG.C_BG);
  dash.setRowHeight(row, 20);

  return row + 1;
}

// ============================================================================
// VOLGENDE DIENST CARD
// ============================================================================

function _writeNextDienstCard(dash, row, dienst) {
  dash.setRowHeight(row, 35);

  if (!dienst) {
    dash.getRange(row, 2, 2, 7).merge()
      .setValue('Geen aankomende diensten gevonden.')
      .setFontSize(12).setFontColor(DB_CONFIG.C_MUTED)
      .setBackground('#1a1a2e').setHorizontalAlignment('center').setVerticalAlignment('middle');
    return row + 2;
  }

  const isBezig    = dienst[HDR['Status']] === 'Bezig';
  const shiftType  = dienst[HDR['Shift Type']] || '';
  const datum      = dienst[HDR['Start Datum']];
  const startTijd  = dienst[HDR['Start Tijd']] || '';
  const eindTijd   = dienst[HDR['Eind Tijd']] || '';
  const locatie    = dienst[HDR['Locatie']] || '';
  const duur       = dienst[HDR['Duur (uur)']] || '';
  const team       = dienst[HDR['Team Prefix']] || '';
  const dagNaam    = dienst[HDR['Dag']] || '';

  const colors     = _shiftColors(shiftType);
  const statusLabel = isBezig ? '🟢 BEZIG' : '⏰ VOLGENDE DIENST';
  const datumStr   = datum instanceof Date
    ? Utilities.formatDate(datum, Session.getScriptTimeZone(), "EEEE d MMMM")
    : String(datum);

  // Top label
  dash.getRange(row, 2, 1, 7).merge()
    .setValue(statusLabel)
    .setFontSize(9).setFontWeight('bold').setFontColor(colors.text)
    .setBackground(colors.bg).setHorizontalAlignment('left').setVerticalAlignment('middle');
  dash.getRange(row, 2).setNumberFormat('@');
  row++;

  // Main card
  dash.setRowHeight(row, 50);
  dash.getRange(row, 2, 1, 4).merge()
    .setValue(`${dagNaam}  ·  ${datumStr}`)
    .setFontSize(18).setFontWeight('bold').setFontColor(colors.text)
    .setBackground(colors.bg).setVerticalAlignment('middle');

  dash.getRange(row, 6, 1, 2).merge()
    .setValue(`${startTijd} – ${eindTijd}`)
    .setFontSize(18).setFontWeight('bold').setFontColor(colors.text)
    .setBackground(colors.bg).setHorizontalAlignment('right').setVerticalAlignment('middle');
  row++;

  dash.setRowHeight(row, 30);
  dash.getRange(row, 2, 1, 3).merge()
    .setValue(locatie)
    .setFontSize(10).setFontColor(colors.text).setBackground(colors.bg)
    .setVerticalAlignment('middle');

  dash.getRange(row, 5, 1, 1)
    .setValue(shiftType)
    .setFontSize(10).setFontColor(colors.text).setBackground(colors.bg)
    .setHorizontalAlignment('center').setVerticalAlignment('middle');

  dash.getRange(row, 6, 1, 2).merge()
    .setValue(`${duur} uur  ·  Team ${team}`)
    .setFontSize(10).setFontColor(colors.text).setBackground(colors.bg)
    .setHorizontalAlignment('right').setVerticalAlignment('middle');

  return row + 1;
}

// ============================================================================
// STATISTIEKEN ROW
// ============================================================================

function _writeStats(dash, row, stats) {
  dash.setRowHeight(row, 16);
  row++;

  const cards = [
    { label: 'Vroeg diensten', value: stats.vroeg, color: DB_CONFIG.C_ORANGE },
    { label: 'Laat diensten',  value: stats.laat,  color: DB_CONFIG.C_RED },
    { label: 'Dag diensten',  value: stats.dienst, color: DB_CONFIG.C_BLUE },
    { label: 'Totale uren',    value: stats.uren + ' u', color: DB_CONFIG.C_GREEN },
  ];

  const cols = [2, 3, 5, 7]; // kolom starts
  cards.forEach((card, i) => {
    const col = cols[i];
    dash.setRowHeight(row, 18);
    dash.getRange(row, col).setValue(String(card.value))
      .setFontSize(20).setFontWeight('bold').setFontColor(card.color)
      .setBackground(DB_CONFIG.C_CARD).setHorizontalAlignment('center');
    dash.getRange(row + 1, col).setValue(card.label)
      .setFontSize(8).setFontColor(DB_CONFIG.C_MUTED)
      .setBackground(DB_CONFIG.C_CARD).setHorizontalAlignment('center');
    dash.setRowHeight(row + 1, 16);
  });

  return row + 3;
}

// ============================================================================
// SECTIE HEADER
// ============================================================================

function _writeSectionHeader(dash, row, title, sub) {
  dash.setRowHeight(row, 28);
  dash.getRange(row, 2, 1, 5).merge()
    .setValue(title)
    .setFontSize(11).setFontWeight('bold').setFontColor(DB_CONFIG.C_ACCENT)
    .setBackground(DB_CONFIG.C_BG).setVerticalAlignment('bottom');

  dash.getRange(row, 7, 1, 1)
    .setValue(sub)
    .setFontSize(9).setFontColor(DB_CONFIG.C_MUTED)
    .setBackground(DB_CONFIG.C_BG).setHorizontalAlignment('right').setVerticalAlignment('bottom');

  return row + 1;
}

// ============================================================================
// SCHEDULE TABLE
// ============================================================================

function _writeScheduleTable(dash, row, rows, mode) {
  const tz = Session.getScriptTimeZone();

  if (!rows || rows.length === 0) {
    dash.setRowHeight(row, 24);
    dash.getRange(row, 2, 1, 7).merge()
      .setValue('— Geen diensten —')
      .setFontSize(9).setFontColor(DB_CONFIG.C_MUTED)
      .setBackground(DB_CONFIG.C_CARD).setHorizontalAlignment('center').setVerticalAlignment('middle');
    return row + 1;
  }

  // Tabel header
  dash.setRowHeight(row, 22);
  const thCols = ['Datum', 'Dag', 'Werktijd', 'Type', 'Team', 'Uren', 'Status'];
  thCols.forEach((h, i) => {
    dash.getRange(row, 2 + i)
      .setValue(h).setFontSize(8).setFontWeight('bold')
      .setFontColor(DB_CONFIG.C_MUTED).setBackground('#12121f')
      .setVerticalAlignment('middle');
  });
  row++;

  rows.forEach(r => {
    const shiftType = r[HDR['Shift Type']] || '';
    const status    = r[HDR['Status']] || '';
    const colors    = mode === 'history' ? { bg: '#12121f', text: '#94a3b8' } : _shiftColors(shiftType);

    const datum = r[HDR['Start Datum']];
    const datumStr = datum instanceof Date
      ? Utilities.formatDate(datum, tz, "dd-MM")
      : String(datum).slice(5);

    dash.setRowHeight(row, 22);
    const vals = [
      datumStr,
      r[HDR['Dag']] || '',
      r[HDR['Werktijd']] || '',
      shiftType,
      r[HDR['Team Prefix']] || '',
      r[HDR['Duur (uur)']] || '',
      status,
    ];
    vals.forEach((v, i) => {
      dash.getRange(row, 2 + i)
        .setValue(v).setFontSize(9)
        .setFontColor(mode === 'history' ? DB_CONFIG.C_MUTED : colors.text)
        .setBackground(mode === 'history' ? '#12121f' : colors.bg)
        .setVerticalAlignment('middle');
    });
    row++;
  });

  return row;
}

// ============================================================================
// HELPERS
// ============================================================================

function _shiftColors(shiftType) {
  switch (shiftType) {
    case 'Vroeg':  return { bg: DB_CONFIG.C_VROEG_BG,  text: DB_CONFIG.C_VROEG  };
    case 'Laat':   return { bg: DB_CONFIG.C_LAAT_BG,   text: DB_CONFIG.C_LAAT   };
    default:       return { bg: DB_CONFIG.C_DIENST_BG, text: DB_CONFIG.C_DIENST };
  }
}

function _computeStats(rows) {
  let vroeg = 0, laat = 0, dienst = 0, uren = 0;
  rows.forEach(r => {
    const type = r[HDR['Shift Type']] || '';
    const duur = parseFloat(String(r[HDR['Duur (uur)']] || '0').replace(',', '.')) || 0;
    if (type === 'Vroeg') vroeg++;
    else if (type === 'Laat') laat++;
    else dienst++;
    uren += duur;
  });
  return { vroeg, laat, dienst, uren: Math.round(uren * 10) / 10 };
}