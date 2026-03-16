/**
 * ============================================================================
 * 💎 MAIN.GS — Configuratie, Menu & Sync Orchestratie
 * ============================================================================
 * Bestanden in dit project:
 *   main.gs        — CONFIG, onOpen, syncCalendarToSheet, sheet helpers
 *   Todoist.gs     — Todoist API integratie (aanmaken, sluiten, verwijderen)
 *   Googleagenda.gs — Google Calendar archief voor gedraaide diensten
 *   convex.gs      — Homeapp/Convex sync
 *   DashboardBuild.gs — Diensten dashboard (apart bestand)
 * ============================================================================
 */

// ============================================================================
// CONFIGURATIE
// ============================================================================

const CONFIG = {
  CALENDAR_ID: '7gml08968kada988va91mu3i2qkci0ts@import.calendar.google.com',

  // Token veilig opgeslagen in UserProperties (los quotum, niet in broncode).
  // Voer setTodoistToken() eenmalig uit via het menu om de token op te slaan.
  get TODOIST_API_TOKEN() {
    return PropertiesService.getUserProperties().getProperty('TODOIST_API_TOKEN')
        || PropertiesService.getScriptProperties().getProperty('TODOIST_API_TOKEN');
  },

  SHEET_NAME_ROSTER:    'DienstenData',
  SHEET_NAME_DASHBOARD: 'Todoist_Dashboard',
  SHEET_NAME_DB:        'Tasks_Todoist_DB',

  SYNC_DAYS_FORWARD: 90,
  SYNC_DAYS_BACK:    30, // Scan ook 30 dagen terug — past diensten correct registreren

  TODOIST_PROJECT_ID:    null,
  TODOIST_LABEL:         'Rooster',
  ARCHIVE_CALENDAR_NAME: 'Diensten Archief', // Native Google Calendar voor permanente geschiedenis

  KEYWORDS_INCLUDE: ['dienst', 'sdb', 'shift'],
  KEYWORDS_EXCLUDE: ['vrij', 'vakantie'],

  COLORS: {
    PRIMARY: '#e44332', ACCENT: '#1a73e8',
    SUCCESS: '#0f9d58', WARNING: '#f4b400', ERROR: '#d93025'
  },
  TODOIST_API_BASE: 'https://api.todoist.com/api/v1/'
};

// ============================================================================
// EENMALIGE SETUP FUNCTIES
// ============================================================================

/**
 * Sla Homeapp/Convex koppeling op in ScriptProperties.
 * Run eenmalig via het menu.
 */
function setHomeappProperties() {
  const CLERK_USER_ID = 'user_3Ax561ZvuSkGtWpKFooeY65HNtY'; // ✅ ingevuld

  if (CLERK_USER_ID === 'VERVANG_DIT_MET_JOUW_USER_ID') {
    throw new Error('❌ Vul eerst je Clerk User ID in!');
  }

  // ⚠️ SECURITY: Sla de HOMEAPP_SYNC_KEY NOOIT op in broncode!
  // Ga naar: Extensies → Apps Script → Projectinstellingen → Script properties
  // en voeg handmatig toe: HOMEAPP_SYNC_KEY = <jouw geheime sleutel>
  const props = PropertiesService.getScriptProperties();
  if (!props.getProperty('HOMEAPP_SYNC_KEY')) {
    throw new Error('❌ HOMEAPP_SYNC_KEY niet gevonden. Stel hem handmatig in via Script Properties (NOOIT in broncode).');
  }

  props.setProperty('HOMEAPP_USER_ID', CLERK_USER_ID);
  props.setProperty('HOMEAPP_CONVEX_URL', 'https://adorable-mink-458.eu-west-1.convex.site');

  Logger.log(`✅ Ingesteld! HOMEAPP_USER_ID = ${CLERK_USER_ID}`);
  SpreadsheetApp.getActiveSpreadsheet().toast(`✅ Homeapp gekoppeld! User: ${CLERK_USER_ID}`, '☁️ Setup Voltooid', 8);
}

/**
 * Sla Todoist API token veilig op in UserProperties (apart quotum van ScriptProperties).
 * Run eenmalig via het menu. Vervang TOKEN daarna door 'VERVANG_MET_JOUW_TOKEN'.
 */
function setTodoistToken() {
  // ⚠️ SECURITY AUDIT FIX: Token NOOIT hardcoden in broncode.
  // Gebruik de UI prompt hieronder — token gaat direct naar UserProperties.
  let ui;
  try { ui = SpreadsheetApp.getUi(); } catch (e) {}

  if (ui) {
    const result = ui.prompt(
      '🔑 Todoist Token Instellen',
      'Plak hier je Todoist API token (Settings → Integrations → API token):',
      ui.ButtonSet.OK_CANCEL
    );
    if (result.getSelectedButton() !== ui.Button.OK) return;
    const token = result.getResponseText().trim();
    if (!token) { ui.alert('❌ Token mag niet leeg zijn.'); return; }
    PropertiesService.getUserProperties().setProperty('TODOIST_API_TOKEN', token);
    Logger.log('✅ Todoist API token opgeslagen via UI prompt');
    SpreadsheetApp.getActiveSpreadsheet().toast('✅ Token veilig opgeslagen!', '🔑 Token Setup', 10);
  } else {
    // Fallback voor editor-run (geen UI context)
    throw new Error('Voer setTodoistToken() uit via het menu (niet via de editor) om de UI prompt te openen.');
  }
}

// ============================================================================
// MENU
// ============================================================================

function onOpen() {
  const ui = SpreadsheetApp.getUi();

  ui.createMenu('🚀 Master Tools')
    .addItem('🔄 Sync Rooster (Start)', 'syncCalendarToSheet')
    .addSeparator()
    .addItem('🧹 Eenmalige Opschoning (Oude Taken)', 'purgeLegacyTasks')
    .addItem('🗑️ Cleanup Duplicaten Todoist', 'cleanupTodoistDuplicates')
    .addItem('🧽 Opschoon Legacy Todoist IDs', 'cleanupLegacyTodoistIds')
    .addSeparator()
    .addItem('📊 Update Todoist Dashboard', 'mainTodoistDashboardSync')
    .addItem('📊 Bouw Diensten Dashboard', 'buildOptimizedDashboard')
    .addSeparator()
    .addItem('🔑 Sla Todoist Token Op (1x uitvoeren)', 'setTodoistToken')
    .addItem('🔎 Diagnose Todoist Verbinding', 'debugTodoistSync')
    .addToUi();

  ui.createMenu('💰 Salaris Tools')
    .addItem('📊 Bouw Salaris Dashboard', 'buildSalarisSheet')
    .addItem('📈 Prognose Huidige Maand', 'berekenPrognose')
    .addToUi();
}


// ============================================================================
// HOOFD SYNC FUNCTIE
// ============================================================================

function syncCalendarToSheet() {
  const stats = { added: 0, updated: 0, ghosts: 0, deduped: 0, unchanged: 0 };

  Logger.log(`🚀 Start Rooster Sync - ${new Date().toISOString()}`);

  try {
    cacheLabelIds();

    const ss = SpreadsheetApp.getActiveSpreadsheet();
    let sheet = ss.getSheetByName(CONFIG.SHEET_NAME_ROSTER) || ss.insertSheet(CONFIG.SHEET_NAME_ROSTER);

    const headers = _setupSheetHeaders(sheet);
    _setupConditionalFormatting(sheet, headers);

    Logger.log('🔍 Todoist taken ophalen...');
    const todoistMap = _buildTodoistMapAndCleanup(stats);
    Logger.log(`✅ ${todoistMap.size} unieke taken gevonden.`);

    const calendar = CalendarApp.getCalendarById(CONFIG.CALENDAR_ID);
    if (!calendar) throw new Error('Agenda niet gevonden! Check CALENDAR_ID.');

    // Scan window: SYNC_DAYS_BACK dagen terug t/m SYNC_DAYS_FORWARD dagen vooruit.
    const now = new Date();
    const startScanDate = new Date(now);
    startScanDate.setDate(startScanDate.getDate() - CONFIG.SYNC_DAYS_BACK);
    startScanDate.setHours(0, 0, 0, 0);
    const endScanDate = new Date(now);
    endScanDate.setDate(endScanDate.getDate() + CONFIG.SYNC_DAYS_FORWARD);

    Logger.log(`📅 Scan window: ${startScanDate.toDateString()} → ${endScanDate.toDateString()}`);
    const events = calendar.getEvents(startScanDate, endScanDate);
    Logger.log(`ℹ️ ${events.length} agenda events.`);

    const data = sheet.getDataRange().getValues();
    const existingSheetMap = new Map();
    const headerRow = data[0];
    const todoistIdIdx = headerRow.indexOf('Todoist ID');

    if (data.length > 1) {
      data.slice(1).forEach(row => {
        const eid = row[headerRow.indexOf('Event ID')];
        if (eid) existingSheetMap.set(eid, row);
      });
    }

    let processedRows = [];

    for (const event of events) {
      const titleLower = event.getTitle().toLowerCase();
      const descLower = (event.getDescription() || '').toLowerCase();

      const isMatch    = CONFIG.KEYWORDS_INCLUDE.some(k => titleLower.includes(k) || descLower.includes(k));
      const isExcluded = CONFIG.KEYWORDS_EXCLUDE.some(k => titleLower.includes(k));
      if (!isMatch || isExcluded) continue;

      const eventId     = event.getId();
      const currentHash = _computeEventHash(event);
      const newRow      = _computeRowData(event, currentHash, headerRow);

      const mappedData = todoistMap.get(eventId);
      const todoistId  = mappedData ? mappedData.id : null;
      const existingHash = mappedData ? mappedData.hash : null;

      // Bestaande sheet-rij voor dit event — nodig voor Archief ID dedup
      const existingRow      = existingSheetMap.get(eventId);
      const archiefIdx       = headerRow.indexOf('Archief ID');
      const existingArchiefId = (existingRow && archiefIdx !== -1) ? existingRow[archiefIdx] : '';

      if (new Date(event.getEndTime()) > new Date()) {
        // ── TOEKOMSTIGE DIENST: Todoist aanmaken of bijwerken ──────────────
        let result;
        if (todoistId) {
          if (existingHash === currentHash) {
            stats.unchanged++;
            newRow[todoistIdIdx] = todoistId;
          } else {
            result = _syncToTodoist(event, todoistId, currentHash);
            if (result) { newRow[todoistIdIdx] = result; stats.updated++; }
          }
        } else {
          Logger.log(`➕ Nieuwe taak: ${event.getTitle()}`);
          result = _syncToTodoist(event, null, currentHash);
          if (result) { newRow[todoistIdIdx] = result; stats.added++; }
        }
        if (archiefIdx !== -1 && existingArchiefId) newRow[archiefIdx] = existingArchiefId;

      } else {
        // ── GEDRAAIDE DIENST: Todoist sluiten + Google Calendar archiveren ─

        // 1. Todoist taak SLUITEN (niet verwijderen — blijft zichtbaar in geschiedenis)
        if (todoistId) {
          _closeTodoistTask(todoistId);
          Logger.log(`✅ Todoist taak gesloten (Gedraaid): ${event.getTitle()} → ID ${todoistId}`);
          newRow[todoistIdIdx] = '';
        } else if (existingRow) {
          const sheetTodoistId = existingRow[todoistIdIdx];
          if (_isValidTodoistId(sheetTodoistId)) {
            _closeTodoistTask(sheetTodoistId);
            Logger.log(`✅ Todoist taak gesloten (sheet fallback): ${event.getTitle()}`);
            newRow[todoistIdIdx] = '';
          } else {
            newRow[todoistIdIdx] = existingRow[todoistIdIdx] || '';
          }
        }

        // 2. Google Calendar Archief: éénmalig wegschrijven naar native kalender
        if (archiefIdx !== -1) {
          if (existingArchiefId) {
            newRow[archiefIdx] = existingArchiefId;
          } else {
            const archiefId = _archiveShiftToCalendar(event);
            if (archiefId) {
              newRow[archiefIdx] = archiefId;
              Logger.log(`📅 Gearchiveerd in '${CONFIG.ARCHIVE_CALENDAR_NAME}': ${event.getTitle()}`);
            }
            // M2: voorkom GAS 6-min timeout bij bulk-archivering (CalendarApp + Todoist calls)
            Utilities.sleep(50);
          }
        }
      }

      existingSheetMap.delete(eventId);
      processedRows.push(newRow);
    }

    const statusIdx = headerRow.indexOf('Status');
    const dateIdx   = headerRow.indexOf('Start Datum');
    const scanStartDate = new Date(startScanDate);
    scanStartDate.setHours(0, 0, 0, 0);

    for (const [id, row] of existingSheetMap) {
      const rowDate        = new Date(row[dateIdx]);
      const isInScanWindow = rowDate >= scanStartDate && rowDate <= endScanDate;
      const isAlreadyDeleted = row[statusIdx] === 'VERWIJDERD';

      if (isInScanWindow && !isAlreadyDeleted) {
        row[statusIdx] = 'VERWIJDERD';
        stats.ghosts++;
        Logger.log(`👻 Ghost gevonden: EID=${id}, datum=${row[dateIdx]}`);

        const mapped = todoistMap.get(id);
        const tId = mapped ? mapped.id : (_isValidTodoistId(row[todoistIdIdx]) ? row[todoistIdIdx] : null);
        if (tId) _deleteTodoistTask(tId);
        processedRows.push(row);
      } else if (!isAlreadyDeleted) {
        if (!isInScanWindow && rowDate < scanStartDate) {
          if (row[statusIdx] === 'Bezig' || row[statusIdx] === 'Opkomend') {
            Logger.log(`🔄 Status gecorrigeerd: EID=${id} → "Gedraaid"`);
            row[statusIdx] = 'Gedraaid';
          }
        }
        processedRows.push(row);
      }
    }

    // Sortering: Bezig → Opkomend (asc) → Gedraaid (desc) → VERWIJDERD
    const statusOrder = { 'Bezig': 0, 'Opkomend': 1, 'Gedraaid': 2, 'VERWIJDERD': 3 };
    processedRows.sort((a, b) => {
      const sa = statusOrder[a[statusIdx]] ?? 4;
      const sb = statusOrder[b[statusIdx]] ?? 4;
      if (sa !== sb) return sa - sb;
      const da = new Date(a[dateIdx]);
      const db = new Date(b[dateIdx]);
      return (sa === 2) ? db - da : da - db;
    });

    if (processedRows.length > 0) {
      if (sheet.getLastRow() > 1) sheet.getRange(2, 1, sheet.getLastRow() - 1, sheet.getLastColumn()).clearContent();
      sheet.getRange(2, 1, processedRows.length, headerRow.length).setValues(processedRows);
    }

    const msg = `Sync klaar! +${stats.added} nieuw | ↻${stats.updated} bijgewerkt | ⏭️${stats.unchanged} skip | 🗑️${stats.ghosts} ghosts | dedup ${stats.deduped}`;
    Logger.log(msg);
    safeToast('Sync Voltooid', msg, 10);

    // 🔁 Push data automatisch naar Homeapp (Convex)
    try {
      const pushResult = pushScheduleToConvex(sheet, headers);
      Logger.log(`☁️ Convex push: ${pushResult}`);
      safeToast('☁️ Homeapp Sync', pushResult, 5);
    } catch (pushErr) {
      Logger.log(`⚠️ Convex push mislukt: ${pushErr.message}`);
      safeToast('⚠️ Homeapp Push', pushErr.message, 8);
    }

  } catch (e) {
    Logger.log(`Sync fout: ${e.message}\n${e.stack}`);
    safeToast('Sync Mislukt', e.message, 15);
  }
}

// ============================================================================
// SHEET HELPERS
// ============================================================================

function _setupSheetHeaders(sheet) {
  const headers = [
    'Event ID', 'Titel', 'Start Datum', 'Start Tijd', 'Eind Datum', 'Eind Tijd',
    'Werktijd', 'Locatie', 'Team Prefix', 'Shift Type', 'Prioriteit', 'Duur (uur)',
    'Weeknr', 'Dag', 'Status', 'Beschrijving', 'Hele Dag', 'Hash',
    'Todoist ID', 'Archief ID', 'Laatst Bijgewerkt'
  ];

  if (sheet.getLastRow() === 0) {
    // Lege sheet: schrijf headers direct
    sheet.getRange(1, 1, 1, headers.length).setValues([headers]).setFontWeight('bold');
    sheet.setFrozenRows(1);
  } else {
    const current = sheet.getRange(1, 1, 1, sheet.getLastColumn()).getValues()[0];

    // Veilige migratie: voeg ontbrekende kolommen RECHTS toe ipv alles te overschrijven.
    // Overschrijven zou bestaande data kunnen verschuiven als de kolomvolgorde wijzigt.
    headers.forEach((h, i) => {
      if (!current.includes(h)) {
        const colIdx = i + 1;
        // Voeg kolom in op de juiste positie (of append als hij er nog niet staat)
        const insertAt = Math.min(colIdx, sheet.getLastColumn() + 1);
        sheet.getRange(1, insertAt).setValue(h).setFontWeight('bold');
        Logger.log(`📋 Header migratie: kolom '${h}' toegevoegd op kolom ${insertAt}`);
      }
    });
  }
  return headers;
}

function _computeEventHash(event) {
  const data = [
    event.getTitle(), event.getStartTime().toISOString(),
    event.getEndTime().toISOString(), event.getLocation(), event.getDescription()
  ].join('|');
  return Utilities.computeDigest(Utilities.DigestAlgorithm.MD5, data)
    .map(b => ('0' + (b & 0xFF).toString(16)).slice(-2)).join('');
}

function _computeRowData(event, hash, headers) {
  const start    = event.getStartTime();
  const end      = event.getEndTime();
  const tz       = Session.getScriptTimeZone();
  const isAllDay = event.isAllDayEvent();

  const startS = isAllDay ? '' : Utilities.formatDate(start, tz, 'HH:mm');
  const endS   = isAllDay ? '' : Utilities.formatDate(end, tz, 'HH:mm');

  let status = 'Opkomend';
  if (end < new Date()) status = 'Gedraaid';
  else if (start < new Date() && end > new Date()) status = 'Bezig';

  const loc  = event.getLocation() || '';
  let team   = '?';
  if (loc.toLowerCase().includes('appartementen')) team = 'R.';
  else if (loc.toLowerCase().includes('aa')) team = 'A.';

  let type = 'Dienst', prio = 1;
  if (!isAllDay) {
    if (start.getHours() < 10)      { type = 'Vroeg'; prio = 4; }
    else if (start.getHours() >= 13) { type = 'Laat';  prio = 2; }
  }

  const map = {
    'Event ID':    event.getId(),
    'Titel':       event.getTitle(),
    'Start Datum': _formatDate(start),
    'Start Tijd':  startS,
    'Eind Datum':  _formatDate(end),
    'Eind Tijd':   endS,
    'Werktijd':    isAllDay ? 'Hele Dag' : `${startS} - ${endS}`,
    'Locatie':     loc,
    'Team Prefix': team,
    'Shift Type':  type,
    'Prioriteit':  prio,
    'Duur (uur)':  Math.round(((end - start) / 36e5) * 100) / 100,
    // ISO 8601 week notatie — W-prefix voorkomt dat Sheets '2026-12' als datum leest
    'Weeknr':      `${Utilities.formatDate(start, tz, 'YYYY')}-W${Utilities.formatDate(start, tz, 'ww')}`,
    'Dag':         ['Zondag','Maandag','Dinsdag','Woensdag','Donderdag','Vrijdag','Zaterdag'][start.getDay()],
    'Status':      status,
    'Beschrijving': event.getDescription() || '',
    'Hele Dag':    isAllDay ? 'Ja' : 'Nee',
    'Hash':        hash,
    'Archief ID':  '', // Gevuld door sync loop na archivering
    'Todoist ID':  '', // Gevuld door sync loop
    'Laatst Bijgewerkt': Utilities.formatDate(new Date(), tz, 'yyyy-MM-dd HH:mm:ss')
  };
  return headers.map(h => map[h] !== undefined ? map[h] : '');
}

function _setupConditionalFormatting(sheet, headers) {
  sheet.clearConditionalFormatRules();
  const statCol = _getColLetter(headers.indexOf('Status') + 1);
  const prioCol = headers.indexOf('Prioriteit') + 1;
  const range   = sheet.getRange(2, 1, sheet.getMaxRows(), sheet.getMaxColumns());

  const rules = [
    SpreadsheetApp.newConditionalFormatRule()
      .whenFormulaSatisfied(`=$${statCol}2="VERWIJDERD"`)
      .setBackground('#EEE').setFontColor('#AAA').setStrikethrough(true).setRanges([range]).build(),
    SpreadsheetApp.newConditionalFormatRule()
      .whenNumberEqualTo(4).setBackground('#FF0000').setFontColor('#FFF')
      .setRanges([sheet.getRange(2, prioCol, sheet.getMaxRows(), 1)]).build(),
    SpreadsheetApp.newConditionalFormatRule()
      .whenNumberEqualTo(2).setBackground('#FFA500')
      .setRanges([sheet.getRange(2, prioCol, sheet.getMaxRows(), 1)]).build()
  ];
  sheet.setConditionalFormatRules(rules);
}

function _formatDate(d) {
  return Utilities.formatDate(d, Session.getScriptTimeZone(), 'yyyy-MM-dd');
}

function _getColLetter(c) {
  let l = '';
  while (c > 0) {
    const t = (c - 1) % 26;
    l = String.fromCharCode(t + 65) + l;
    c = (c - t - 1) / 26;
  }
  return l;
}

/**
 * Controleert of een waarde eruitziet als een echt Todoist ID.
 * Filtert epoch-datums, Sheets Date-objecten en NL-datum strings eruit.
 */
function _isValidTodoistId(val) {
  if (!val && val !== 0) return false;
  if (val instanceof Date) return false; // Sheets levert datumcellen als Date objecten
  const s = String(val).trim();
  if (s === '') return false;
  if (/^\d{4}-\d{2}-\d{2}/.test(s)) return false; // '1970-01-01' of timestamp
  if (/^\d{1,2}-\d{1,2}-\d{4}/.test(s)) return false; // '27-11-2025' (NL formaat)
  return true;
}

/**
 * Ruimt legacy garbage-waarden op in de Todoist ID kolom.
 * Doet: epoch-datums, timestamps en NL-datums → lege string.
 */
function cleanupLegacyTodoistIds() {
  const ss    = SpreadsheetApp.getActiveSpreadsheet();
  const sheet = ss.getSheetByName(CONFIG.SHEET_NAME_ROSTER);
  if (!sheet || sheet.getLastRow() < 2) { safeToast('Cleanup', 'Geen data gevonden.', 5); return; }

  const headers   = sheet.getRange(1, 1, 1, sheet.getLastColumn()).getValues()[0];
  const tidIdx    = headers.indexOf('Todoist ID');
  if (tidIdx === -1) { safeToast('Cleanup', 'Todoist ID kolom niet gevonden.', 5); return; }

  const dataRange = sheet.getRange(2, 1, sheet.getLastRow() - 1, headers.length);
  const data      = dataRange.getValues();
  let fixed = 0;

  data.forEach((row, i) => {
    const val = row[tidIdx];
    if (!_isValidTodoistId(val) && val !== '') {
      Logger.log(`🧹 Rij ${i + 2}: Todoist ID '${val}' → leeg`);
      row[tidIdx] = '';
      fixed++;
    }
  });

  if (fixed > 0) {
    dataRange.setValues(data);
    safeToast('Opschoning', `✅ ${fixed} ongeldige Todoist IDs verwijderd.`, 8);
  } else {
    safeToast('Opschoning', 'Geen garbage IDs gevonden — sheet is al schoon ✅', 8);
  }
}

function safeToast(title, msg, seconds) {
  try {
    SpreadsheetApp.getActiveSpreadsheet().toast(msg, title, seconds);
  } catch (e) {
    Logger.log(`Toast niet mogelijk: ${msg}`);
  }
}
