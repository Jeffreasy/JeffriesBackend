/**
 * ============================================================================
 * 💎 MASTER SCRIPT: ROSTER SYNC & TODOIST DASHBOARD (V14.5 - AUDIT FIX)
 * ============================================================================
 * - Geoptimaliseerde Hash-checking (geen GET requests meer per taak = sneller)
 * - Volledige pagination voor labels & tasks
 * - Run altijd VIA MENU om alerts te zien
 */

const CONFIG = {
  CALENDAR_ID: '7gml08968kada988va91mu3i2qkci0ts@import.calendar.google.com',
  // Token veilig opgeslagen in UserProperties (los quotum, niet in broncode)
  get TODOIST_API_TOKEN() {
    return PropertiesService.getUserProperties().getProperty('TODOIST_API_TOKEN')
        || PropertiesService.getScriptProperties().getProperty('TODOIST_API_TOKEN');
  },
  SHEET_NAME_ROSTER: 'DienstenData',
  SHEET_NAME_DASHBOARD: 'Todoist_Dashboard',
  SHEET_NAME_DB: 'Tasks_Todoist_DB',
  SYNC_DAYS_FORWARD: 90,
  SYNC_DAYS_BACK: 30,     // Scan ook 30 dagen terug — diensten die vóór de sync lagen worden nu wél opgehaald
  TODOIST_PROJECT_ID: null, 
  TODOIST_LABEL: 'Rooster',
  ARCHIVE_CALENDAR_NAME: 'Diensten Archief', // Native Google Calendar voor permanente geschiedenis
  KEYWORDS_INCLUDE: ['dienst', 'sdb', 'shift'], 
  KEYWORDS_EXCLUDE: ['vrij', 'vakantie'],
  COLORS: { PRIMARY: "#e44332", ACCENT: "#1a73e8", SUCCESS: "#0f9d58", WARNING: "#f4b400", ERROR: "#d93025" },
  TODOIST_API_BASE: 'https://api.todoist.com/api/v1/'
};

let LABEL_ID_ROOSTER = null;

// ============================================================================
// ⚙️ EENMALIGE SETUP — Run dit 1x om Homeapp properties in te stellen
// ============================================================================
// STAP 1: Vul je Clerk User ID in hieronder (bijv. user_2abc...)
// STAP 2: Selecteer "setHomeappProperties" in de dropdown → ▶ Run
// STAP 3: Klaar — je kunt deze functie laten staan, hij doet niets bij sync

function setHomeappProperties() {
  const CLERK_USER_ID = 'user_3Ax561ZvuSkGtWpKFooeY65HNtY'; // ✅ ingevuld

  if (CLERK_USER_ID === 'VERVANG_DIT_MET_JOUW_USER_ID') {
    throw new Error('❌ Vul eerst je Clerk User ID in! (bijv. user_2abc...)');
  }

  const props = PropertiesService.getScriptProperties();
  props.setProperty('HOMEAPP_USER_ID', CLERK_USER_ID);

  Logger.log(`✅ Ingesteld! HOMEAPP_SYNC_KEY + HOMEAPP_USER_ID = ${CLERK_USER_ID}`);
  SpreadsheetApp.getActiveSpreadsheet().toast(
    `✅ Homeapp gekoppeld! User: ${CLERK_USER_ID}`,
    '☁️ Setup Voltooid', 8
  );
}

// ============================================================================
// 🔑 EENMALIGE TOKEN SETUP — Sla Todoist token veilig op in UserProperties
// ============================================================================
// UserProperties heeft een apart quotum (los van ScriptProperties).
// De token wordt via een dialoog gevraagd en komt nooit in de broncode terecht.
function setTodoistToken() {
  const ui = SpreadsheetApp.getUi();
  const response = ui.prompt(
    'Todoist-token instellen',
    'Plak je nieuw gegenereerde Todoist API-token. De waarde wordt alleen in UserProperties opgeslagen.',
    ui.ButtonSet.OK_CANCEL
  );

  if (response.getSelectedButton() !== ui.Button.OK) return;

  const token = response.getResponseText().trim();
  if (token.length < 20) throw new Error('❌ Ongeldig Todoist API-token.');

  PropertiesService.getUserProperties().setProperty('TODOIST_API_TOKEN', token);
  Logger.log('✅ Todoist API token opgeslagen in UserProperties');
  SpreadsheetApp.getActiveSpreadsheet().toast(
    '✅ Token veilig opgeslagen in UserProperties.',
    '🔑 Token Setup', 10
  );
}

/**
 * Diagnose: test de Todoist-verbinding stap voor stap.
 * Run via 🚀 Master Tools → 🔎 Diagnose Todoist Verbinding
 */
function debugTodoistSync() {
  const token = CONFIG.TODOIST_API_TOKEN;
  Logger.log('=== TODOIST DIAGNOSE ===');

  // Stap 1: Token check
  if (!token) {
    Logger.log('❌ STAP 1 MISLUKT: Token is null/leeg. Voer setTodoistToken() uit!');
    safeToast('Diagnose', '❌ Token ontbreekt — voer 🔑 Sla Todoist Token Op uit', 10);
    return;
  }
  Logger.log(`✅ Stap 1: Token aanwezig (eindigt op ...${token.slice(-6)})`);

  // Stap 2: API bereikbaar?
  try {
    const res = UrlFetchApp.fetch(CONFIG.TODOIST_API_BASE + 'projects', {
      headers: { 'Authorization': 'Bearer ' + token },
      muteHttpExceptions: true
    });
    Logger.log(`✅ Stap 2: API bereikbaar — HTTP ${res.getResponseCode()}`);
    if (res.getResponseCode() === 401) {
      Logger.log('❌ HTTP 401 = Token ongeldig of verlopen!');
      safeToast('Diagnose', '❌ Token ongeldig (401)', 10);
      return;
    }
    if (res.getResponseCode() !== 200) {
      Logger.log(`❌ Onverwachte HTTP ${res.getResponseCode()}: ${res.getContentText()}`);
      safeToast('Diagnose', `❌ API fout HTTP ${res.getResponseCode()}`, 10);
      return;
    }
  } catch(e) {
    Logger.log(`❌ Stap 2 MISLUKT: Netwerk fout — ${e.message}`);
    return;
  }

  // Stap 3: Labels check
  const labels = fetchPaginated('labels');
  const roosterLabel = labels.find(l => (l.name || '').toLowerCase() === CONFIG.TODOIST_LABEL.toLowerCase());
  if (roosterLabel) {
    Logger.log(`✅ Stap 3: Label '${CONFIG.TODOIST_LABEL}' gevonden (ID: ${roosterLabel.id})`);
  } else {
    Logger.log(`⚠️ Stap 3: Label '${CONFIG.TODOIST_LABEL}' NIET gevonden. Beschikbaar: ${labels.map(l => l.name).join(', ')}`);
    Logger.log('   → Taken worden aangemaakt ZONDER label. Dit blokkeert de sync NIET.');
  }

  // Stap 4: Bestaande taken ophalen
  const tasks = fetchPaginated('tasks');
  const roosterTasks = tasks.filter(t => t.description && t.description.includes('[EID:'));
  Logger.log(`✅ Stap 4: ${tasks.length} actieve taken, waarvan ${roosterTasks.length} rooster-taken (met EID)`);

  // Stap 5: Test taak aanmaken
  try {
    const testPayload = {
      content: '🧪 DIAGNOSE TEST — mag worden verwijderd',
      description: 'Automatische diagnose. Verwijder deze taak.',
      due_date: Utilities.formatDate(new Date(), Session.getScriptTimeZone(), 'yyyy-MM-dd'),
      labels: roosterLabel ? [CONFIG.TODOIST_LABEL] : []
    };
    const res = UrlFetchApp.fetch(CONFIG.TODOIST_API_BASE + 'tasks', {
      method: 'post',
      headers: { 'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json' },
      payload: JSON.stringify(testPayload),
      muteHttpExceptions: true
    });
    if (res.getResponseCode() === 200) {
      const created = JSON.parse(res.getContentText());
      Logger.log(`✅ Stap 5: Test taak aangemaakt (ID: ${created.id}) — verwijder deze handmatig in Todoist`);
      safeToast('Diagnose Gelukt ✅', 'Alles werkt! Zie logs voor details.', 10);
    } else {
      Logger.log(`❌ Stap 5 MISLUKT: HTTP ${res.getResponseCode()}\nResponse: ${res.getContentText()}`);
      safeToast('Diagnose', `❌ Taak aanmaken mislukt: HTTP ${res.getResponseCode()}`, 15);
    }
  } catch(e) {
    Logger.log(`❌ Stap 5 fout: ${e.message}`);
  }

  Logger.log('=== EINDE DIAGNOSE ===');
}

// ============================================================================
// MENU
// ============================================================================

function onOpen() {
  SpreadsheetApp.getUi().createMenu('🚀 Master Tools')
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
}

// ============================================================================
// PAGINATION HELPER (voor labels & tasks)
// ============================================================================

function fetchPaginated(endpoint) {
  let allItems = [];
  let cursor = null;

  do {
    let url = CONFIG.TODOIST_API_BASE + endpoint;
    if (cursor) {
      url += (url.includes('?') ? '&' : '?') + `cursor=${encodeURIComponent(cursor)}`;
    }

    const res = UrlFetchApp.fetch(url, {
      headers: { "Authorization": "Bearer " + CONFIG.TODOIST_API_TOKEN },
      muteHttpExceptions: true
    });

    if (res.getResponseCode() !== 200) {
      Logger.log(`Fetch ${endpoint} fout: HTTP ${res.getResponseCode()} - ${res.getContentText()}`);
      return allItems;
    }

    const data = JSON.parse(res.getContentText());
    const items = data.results || [];
    
    if (!Array.isArray(items)) {
      Logger.log(`Onverwachte response bij ${endpoint}: ${JSON.stringify(data)}`);
      return allItems;
    }

    allItems = allItems.concat(items);
    cursor = data.next_cursor || null;

    Logger.log(`Fetched ${items.length} items from ${endpoint}, next_cursor: ${cursor || 'geen'}`);
  } while (cursor);

  return allItems;
}

// ============================================================================
// CACHE LABEL ID
// ============================================================================

function cacheLabelIds() {
  if (LABEL_ID_ROOSTER !== null) return LABEL_ID_ROOSTER;

  try {
    const labels = fetchPaginated("labels");
    const roosterLabel = labels.find(l => l.name?.toLowerCase() === CONFIG.TODOIST_LABEL.toLowerCase());

    if (roosterLabel?.id) {
      LABEL_ID_ROOSTER = roosterLabel.id;
      Logger.log(`✅ Label 'Rooster' gevonden → ID: ${LABEL_ID_ROOSTER}`);
    } else {
      Logger.log(`⚠️ Label 'Rooster' niet gevonden. Beschikbare labels: ${labels.map(l => l.name || 'onbekend').join(', ')}`);
    }
  } catch (e) {
    Logger.log(`Label cache fout: ${e.message}`);
  }

  return LABEL_ID_ROOSTER;
}

// ============================================================================
// SYNC ROOSTER (volledig)
// ============================================================================

function syncCalendarToSheet() {
  const stats = { added: 0, updated: 0, ghosts: 0, deduped: 0, unchanged: 0 };
  
  Logger.log(`🚀 Start Rooster Sync V14.5 - ${new Date().toISOString()}`);
  
  try {
    cacheLabelIds();

    const ss = SpreadsheetApp.getActiveSpreadsheet();
    let sheet = ss.getSheetByName(CONFIG.SHEET_NAME_ROSTER) || ss.insertSheet(CONFIG.SHEET_NAME_ROSTER);
    
    const headers = _setupSheetHeaders(sheet);
    _setupConditionalFormatting(sheet, headers);
    
    Logger.log("🔍 Todoist taken ophalen...");
    const todoistMap = _buildTodoistMapAndCleanup(stats);
    Logger.log(`✅ ${todoistMap.size} unieke taken gevonden.`);

    const calendar = CalendarApp.getCalendarById(CONFIG.CALENDAR_ID);
    if (!calendar) throw new Error("Agenda niet gevonden! Check CALENDAR_ID.");

    // Scan window: SYNC_DAYS_BACK dagen terug t/m SYNC_DAYS_FORWARD dagen vooruit.
    // Zo worden diensten die al plaatsvonden (maar nog niet in de sheet staan)
    // alsnog opgehaald en als 'Gedraaid' geregistreerd.
    // Todoist-taken worden alleen aangemaakt voor toekomstige diensten (guard op regel ~230).
    const now = new Date();
    const startScanDate = new Date(now);
    startScanDate.setDate(startScanDate.getDate() - CONFIG.SYNC_DAYS_BACK);
    startScanDate.setHours(0, 0, 0, 0); // altijd vanaf dag-begin
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
      const descLower = (event.getDescription() || "").toLowerCase();
      
      const isMatch = CONFIG.KEYWORDS_INCLUDE.some(k => titleLower.includes(k) || descLower.includes(k));
      const isExcluded = CONFIG.KEYWORDS_EXCLUDE.some(k => titleLower.includes(k));
      
      if (!isMatch || isExcluded) continue;

      const eventId = event.getId();
      const currentHash = _computeEventHash(event);
      const newRow = _computeRowData(event, currentHash, headerRow);
      
      // ✅ Haal gemapte data op uit het geheugen (ID en Hash)
      const mappedData = todoistMap.get(eventId);
      const todoistId = mappedData ? mappedData.id : null;
      const existingHash = mappedData ? mappedData.hash : null;

      // Bestaande sheet-rij voor dit event — nodig voor Archief ID dedup
      const existingRow = existingSheetMap.get(eventId);
      const archiefIdx = headerRow.indexOf('Archief ID');
      const existingArchiefId = (existingRow && archiefIdx !== -1) ? existingRow[archiefIdx] : '';

      if (new Date(event.getEndTime()) > new Date()) {
        // ── TOEKOMSTIGE DIENST: Todoist aanmaken of bijwerken ──────────────────
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
        // Bewaar bestaand Archief ID (dienst nog niet gedraaid)
        if (archiefIdx !== -1 && existingArchiefId) newRow[archiefIdx] = existingArchiefId;

      } else {
        // ── GEDRAAIDE DIENST: Todoist sluiten + Google Calendar archiveren ────
        
        // 1. Todoist taak SLUITEN (niet verwijderen — blijft zichtbaar in geschiedenis)
        //    Taak staat alleen in todoistMap als hij nog actief/open is.
        if (todoistId) {
          _closeTodoistTask(todoistId);
          Logger.log(`✅ Todoist taak gesloten (Gedraaid): ${event.getTitle()} → ID ${todoistId}`);
          // Todoist ID wissen: gesloten taken zijn niet meer actief opvraagbaar
          newRow[todoistIdIdx] = '';
        } else {
          // Fallback: check sheet voor eventueel nog open ID (bv. van vóór de close-logica)
          if (existingRow) {
            const sheetTodoistId = existingRow[todoistIdIdx];
            if (_isValidTodoistId(sheetTodoistId)) {
              _closeTodoistTask(sheetTodoistId);
              Logger.log(`✅ Todoist taak gesloten (sheet fallback): ${event.getTitle()}`);
              newRow[todoistIdIdx] = '';
            } else if (existingRow) {
              newRow[todoistIdIdx] = existingRow[todoistIdIdx] || '';
            }
          }
        }

        // 2. Google Calendar Archief: schrijf éénmalig naar native kalender
        //    Dedup: als Archief ID al bestaat → nooit opnieuw aanmaken
        if (archiefIdx !== -1) {
          if (existingArchiefId) {
            // Al gearchiveerd — bewaar ID
            newRow[archiefIdx] = existingArchiefId;
          } else {
            // Nog niet gearchiveerd → nu wegschrijven
            const archiefId = _archiveShiftToCalendar(event);
            if (archiefId) {
              newRow[archiefIdx] = archiefId;
              Logger.log(`📅 Dienst gearchiveerd in '${CONFIG.ARCHIVE_CALENDAR_NAME}': ${event.getTitle()} → ${archiefId}`);
            }
          }
        }
      }

      existingSheetMap.delete(eventId);
      processedRows.push(newRow);
    }

    const statusIdx = headerRow.indexOf('Status');
    const dateIdx = headerRow.indexOf('Start Datum');

    // ── Normaliseer startScanDate naar dag-begin (00:00) zodat vandaag's
    //    gecancelde diensten ook worden gepakt, ongeacht het huidige tijdstip.
    const scanStartDate = new Date(startScanDate);
    scanStartDate.setHours(0, 0, 0, 0);

    for (const [id, row] of existingSheetMap) {
      const rowDate = new Date(row[dateIdx]);

      // BUG FIX 1: Gebruik dag-begin voor vergelijking (niet huidig tijdstip)
      const isInScanWindow = rowDate >= scanStartDate && rowDate <= endScanDate;
      const isAlreadyDeleted = row[statusIdx] === 'VERWIJDERD';

      if (isInScanWindow && !isAlreadyDeleted) {
        // Dienst bestaat niet meer in de agenda → markeer als verwijderd
        row[statusIdx] = 'VERWIJDERD';
        stats.ghosts++;
        Logger.log(`👻 Ghost gevonden: EID=${id}, datum=${row[dateIdx]}`);

        const mapped = todoistMap.get(id);
        // Gebruik altijd het Todoist ID uit de live API-map (betrouwbaar).
        // Val ALLEEN terug op sheet-waarde als dat een echt Todoist ID is.
        const tId = mapped ? mapped.id : (_isValidTodoistId(row[todoistIdIdx]) ? row[todoistIdIdx] : null);
        if (tId) _deleteTodoistTask(tId);
        processedRows.push(row);
      } else if (!isAlreadyDeleted) {
        // BUG FIX 2: Sla al-VERWIJDERD rijen NIET opnieuw op (voorkomen ophoping).
        //            Behoud historische (verleden) diensten, maar herbereken de status.
        // BUG FIX 4: Verouderde "Bezig" / "Opkomend" corrigeren naar "Gedraaid".
        //
        // rowDate is de startdatum van de dienst (als Date object van Sheets).
        // Als de startdag vóór vandaag ligt, is de dienst sowieso voorbij.
        // We vergelijken dag-voor-dag (scanStartDate is al genormaliseerd op 00:00).
        if (!isInScanWindow && rowDate < scanStartDate) {
          if (row[statusIdx] === 'Bezig' || row[statusIdx] === 'Opkomend') {
            Logger.log(`🔄 Status gecorrigeerd: EID=${id} was "${row[statusIdx]}" → "Gedraaid"`);
            row[statusIdx] = 'Gedraaid';
          }
        }
        processedRows.push(row);
      }
      // isAlreadyDeleted && !isInScanWindow → gooi weg (sheet opschonen)
    }

    // Sortering: 1) Bezig boven, 2) Opkomend oplopend (eerstvolgende eerst),
    //            3) Gedraaid aflopend (meest recente geschiedenis onderaan)
    const statusOrder = { 'Bezig': 0, 'Opkomend': 1, 'Gedraaid': 2, 'VERWIJDERD': 3 };
    processedRows.sort((a, b) => {
      const sa = statusOrder[a[statusIdx]] ?? 4;
      const sb = statusOrder[b[statusIdx]] ?? 4;
      if (sa !== sb) return sa - sb;
      // Binnen Opkomend: vroegste datum eerst (asc)
      // Binnen Gedraaid: nieuwste datum eerst (desc)
      const da = new Date(a[dateIdx]);
      const db = new Date(b[dateIdx]);
      return (sa === 2) ? db - da : da - db;
    });
    
    if (processedRows.length > 0) {
      if (sheet.getLastRow() > 1) sheet.getRange(2, 1, sheet.getLastRow()-1, sheet.getLastColumn()).clearContent();
      sheet.getRange(2, 1, processedRows.length, headerRow.length).setValues(processedRows);
    }
    
    const msg = `Sync klaar! +${stats.added} nieuw | ↻${stats.updated} bijgewerkt | ⏭️${stats.unchanged} skip | 🗑️${stats.ghosts} ghosts | dedup ${stats.deduped}`;
    Logger.log(msg);
    safeToast("Sync Voltooid", msg, 10);

  } catch (e) {
    Logger.log(`Sync fout: ${e.message}\n${e.stack}`);
    safeToast("Sync Mislukt", e.message, 15);
  }
}

function safeToast(title, msg, seconds) {
  try {
    SpreadsheetApp.getActiveSpreadsheet().toast(msg, title, seconds);
  } catch (e) {
    Logger.log(`Toast niet mogelijk: ${msg}`);
  }
}

// ============================================================================
// TODOIST SYNC & MAP
// ============================================================================

function _buildTodoistMapAndCleanup(stats) {
  const map = new Map();

  try {
    const tasks = fetchPaginated("tasks");
    const eidSeen = new Map();

    tasks.forEach(task => {
      const eidMatch = task.description ? task.description.match(/\[EID:(.*?)\]/) : null;
      const hashMatch = task.description ? task.description.match(/Hash:\s([a-f0-9]+)/) : null;
      
      if (eidMatch) {
        const eid = eidMatch[1];
        const hash = hashMatch ? hashMatch[1] : null;

        if (eidSeen.has(eid)) {
          _deleteTodoistTask(task.id);
          stats.deduped++;
          Logger.log(`Duplicaat verwijderd tijdens scan: ${task.id} (EID ${eid})`);
        } else {
          eidSeen.set(eid, task.id);
          map.set(eid, { id: task.id, hash: hash }); // ✅ Hash in geheugen opslaan
        }
      }
    });
  } catch (e) {
    Logger.log(`Map bouwen fout: ${e.message}`);
  }
  return map;
}

function _syncToTodoist(event, existingTaskId, currentHash) {
  if (!CONFIG.TODOIST_API_TOKEN) return null;

  const loc = (event.getLocation() || "").toLowerCase();
  const start = event.getStartTime();
  const end = event.getEndTime();
  const startHour = start.getHours();

  let team = "?";
  if (loc.includes("appartementen")) team = "R.";
  else if (loc.includes("aa")) team = "A.";

  let type = "Dienst";
  if (startHour < 10) type = "Vroeg";
  else if (startHour >= 13) type = "Laat";

  const title = (team !== "?") ? `${team} ${type}` : `💼 ${event.getTitle()} (${team})`;

  let durationMin = Math.floor((end - start) / (1000 * 60)) || 15;
  const description = `Locatie: ${event.getLocation() || 'Onbekend'}\nDuur: ${Math.round(durationMin / 60 * 10)/10} uur\nHash: ${currentHash}\n\n[EID:${event.getId()}]`;

  // BUG FIX: Todoist REST API v1 gebruikt 'labels' (array van naam-strings),
  // NIET 'label_ids'. label_ids is de oude v8/v9 API en wordt stil genegeerd.
  const payload = {
    content: title,
    description: description,
    labels: [CONFIG.TODOIST_LABEL]  // Altijd 'Rooster' label meegeven op naam
  };

  if (CONFIG.TODOIST_PROJECT_ID) payload.project_id = CONFIG.TODOIST_PROJECT_ID;

  if (event.isAllDayEvent()) {
    payload.due_date = Utilities.formatDate(start, Session.getScriptTimeZone(), "yyyy-MM-dd");
  } else {
    payload.due_datetime = start.toISOString();
    payload.duration = durationMin;
    payload.duration_unit = "minute";
  }

  const headers = {
    "Authorization": "Bearer " + CONFIG.TODOIST_API_TOKEN,
    "Content-Type": "application/json"
  };

  try {
    let url = CONFIG.TODOIST_API_BASE + "tasks";
    let method = "post"; // Todoist updates via POST

    if (existingTaskId) {
      url += `/${existingTaskId}`;
    }

    const res = UrlFetchApp.fetch(url, {
      method: method,
      headers: headers,
      payload: JSON.stringify(payload),
      muteHttpExceptions: true
    });

    const httpCode = res.getResponseCode();
    const body = res.getContentText();

    if (httpCode >= 400) {
      // Log de volledige response zodat we exact weten wat er misging
      Logger.log(`❌ Todoist API fout HTTP ${httpCode} voor "${event.getTitle()}"`);
      Logger.log(`   URL: ${url}`);
      Logger.log(`   Response: ${body}`);
      Logger.log(`   Payload: ${JSON.stringify(payload)}`);
      return null;
    }

    const created = JSON.parse(body);
    Logger.log(`✅ Todoist taak ${existingTaskId ? 'bijgewerkt' : 'aangemaakt'}: "${created.content}" (ID: ${created.id})`);
    return created.id;
  } catch (e) {
    Logger.log(`❌ _syncToTodoist netwerk fout voor "${event.getTitle()}": ${e.message}`);
    return null;
  }
}

function _deleteTodoistTask(taskId) {
  try {
    UrlFetchApp.fetch(CONFIG.TODOIST_API_BASE + `tasks/${taskId}`, {
      method: "delete",
      headers: { "Authorization": "Bearer " + CONFIG.TODOIST_API_TOKEN }
    });
  } catch (e) {}
}

function _closeTodoistTask(taskId) {
  try {
    const res = UrlFetchApp.fetch(CONFIG.TODOIST_API_BASE + `tasks/${taskId}/close`, {
      method: "post",
      headers: { "Authorization": "Bearer " + CONFIG.TODOIST_API_TOKEN },
      muteHttpExceptions: true
    });
    if (res.getResponseCode() !== 204) {
      Logger.log(`⚠️ Close task ${taskId} HTTP ${res.getResponseCode()}: ${res.getContentText()}`);
    }
  } catch (e) {
    Logger.log(`❌ _closeTodoistTask fout: ${e.message}`);
  }
}

// ============================================================================
// 📅 GOOGLE CALENDAR ARCHIEF HELPERS
// ============================================================================

/**
 * Haal de native archief-kalender op, of maak hem aan als hij niet bestaat.
 * De naam is instelbaar via CONFIG.ARCHIVE_CALENDAR_NAME.
 */
function _getOrCreateArchiveCalendar() {
  const name = CONFIG.ARCHIVE_CALENDAR_NAME;
  const existing = CalendarApp.getCalendarsByName(name);
  if (existing.length > 0) return existing[0];

  // Kalender bestaat nog niet — aanmaken
  Logger.log(`📅 Archief-kalender '${name}' niet gevonden — aanmaken...`);
  const cal = CalendarApp.createCalendar(name, {
    color: CalendarApp.Color.TEAL,
    summary: 'Permanente geschiedenis van gedraaide diensten (bijgehouden door het GAS sync-script).'
  });
  Logger.log(`✅ Archief-kalender aangemaakt: ${cal.getId()}`);
  return cal;
}

/**
 * Schrijft een gedraaide dienst als permanent event naar de archief-kalender.
 * @returns {string|null} Calendar event ID (als dedup-sleutel in de sheet)
 */
function _archiveShiftToCalendar(event) {
  try {
    const cal = _getOrCreateArchiveCalendar();
    const title = `✅ ${event.getTitle()}`;
    const desc  = `Gedraaid op ${_formatDate(new Date())}\n` +
                  `Origineel agenda-event: ${event.getId()}\n\n` +
                  (event.getDescription() || '');

    let archived;
    if (event.isAllDayEvent()) {
      archived = cal.createAllDayEvent(title, event.getStartTime(), { description: desc, location: event.getLocation() || '' });
    } else {
      archived = cal.createEvent(title, event.getStartTime(), event.getEndTime(), { description: desc, location: event.getLocation() || '' });
    }
    return archived.getId();
  } catch (e) {
    Logger.log(`❌ _archiveShiftToCalendar fout voor "${event.getTitle()}": ${e.message}`);
    return null;
  }
}

function cleanupTodoistDuplicates() {
  let ui = null;
  try {
    ui = SpreadsheetApp.getUi();
    ui.alert("🗑️ Cleanup gestart – bekijk logs voor details.");
  } catch (e) {
    Logger.log("Geen UI context (normaal bij editor run)");
  }

  const stats = { removed: 0 };
  
  try {
    cacheLabelIds();

    const tasks = fetchPaginated("tasks");
    const eidMap = new Map();

    tasks.forEach(task => {
      const match = task.description ? task.description.match(/\[EID:(.*?)\]/) : null;
      if (match) {
        const eid = match[1];
        if (eidMap.has(eid)) {
          _deleteTodoistTask(task.id);
          stats.removed++;
          Logger.log(`Duplicaat verwijderd: ${task.id} voor EID ${eid}`);
        } else {
          eidMap.set(eid, task.id);
        }
      }
    });
    
    const msg = `Cleanup klaar! ${stats.removed} duplicaten verwijderd.`;
    Logger.log(msg);
    if (ui) ui.alert(msg);
  } catch (e) {
    Logger.log(`Cleanup fout: ${e.message}`);
    if (ui) ui.alert("❌ Cleanup fout: " + e.message);
  }
}

// ============================================================================
// DASHBOARD FUNCTIES
// ============================================================================

function mainTodoistDashboardSync() {
  const ss = SpreadsheetApp.getActiveSpreadsheet();
  let globalStats = { active: 0, completed: 0 };
  
  let dbSheet = ss.getSheetByName(CONFIG.SHEET_NAME_DB);
  if (!dbSheet) {
    dbSheet = ss.insertSheet(CONFIG.SHEET_NAME_DB);
    dbSheet.appendRow(["Task ID", "Project", "Content", "Due Date", "Description", "Status", "Priority", "RAG Context", "Link"]);
    dbSheet.setFrozenRows(1);
  }

  const projectMap = _fetchTodoistProjects();
  let allTasksBuffer = [];

  try {
    const activeTasks = fetchPaginated("tasks");
    activeTasks.forEach(task => { 
      allTasksBuffer.push(_processTaskForDB(task, projectMap, "Active")); 
      globalStats.active++; 
    });
  } catch (e) { Logger.log(`Active tasks fout: ${e.message}`); }

  try {
    const completedTasks = _fetchTodoistCompletedTasks(50);
    completedTasks.forEach(task => { 
      allTasksBuffer.push(_processTaskForDB(task, projectMap, "Completed")); 
      globalStats.completed++; 
    });
  } catch (e) { Logger.log(`Completed tasks fout: ${e.message}`); }

  if (allTasksBuffer.length > 0) {
    if (dbSheet.getLastRow() > 1) dbSheet.getRange(2, 1, dbSheet.getLastRow()-1, dbSheet.getLastColumn()).clearContent();
    dbSheet.getRange(2, 1, allTasksBuffer.length, allTasksBuffer[0].length).setValues(allTasksBuffer);
  }

  _buildDashboard(ss, globalStats);
}

function _buildDashboard(ss, stats) {
  let dash = ss.getSheetByName(CONFIG.SHEET_NAME_DASHBOARD);
  if (!dash) dash = ss.insertSheet(CONFIG.SHEET_NAME_DASHBOARD, 0);
  const c = CONFIG.COLORS;
  dash.getRange("B2").setValue("🔴 Todoist Intelligence (v1)").setFontSize(18).setFontColor(c.PRIMARY);
  dash.getRange("B3").setValue("Update: " + _formatDate(new Date()));
  _createCard(dash, 5, 2, "⚡ Actief", stats.active, c.PRIMARY, "#ffe0e0");
  _createCard(dash, 5, 5, "🚨 P1", `=COUNTIF('${CONFIG.SHEET_NAME_DB}'!G:G, "*P1*")`, c.ERROR, "#fce8e6");
  _createCard(dash, 5, 8, "✅ Klaar", stats.completed, c.SUCCESS, "#e6f4ea");
}

function _createCard(sheet, r, c, title, val, color, bg) {
  sheet.getRange(r, c, 3, 2).merge().setBackground(bg).setBorder(true,true,true,true,null,null,"#ddd",null);
  sheet.getRange(r, c).setValue(title).setFontColor(color).setFontWeight("bold").setHorizontalAlignment("center");
  const vRange = sheet.getRange(r+1, c);
  if(String(val).startsWith('=')) vRange.setFormula(val); else vRange.setValue(val);
  vRange.setFontSize(24).setHorizontalAlignment("center");
}

// ============================================================================
// TODOIST HELPERS
// ============================================================================

function _getHeaders() {
  return { "Authorization": "Bearer " + CONFIG.TODOIST_API_TOKEN };
}

function _fetchTodoistProjects() {
  try {
    const projects = fetchPaginated("projects");
    const map = {};
    projects.forEach(p => map[p.id] = p.name);
    return map;
  } catch (e) { return {}; }
}

function _fetchTodoistCompletedTasks(limit) {
  try {
    let url = `${CONFIG.TODOIST_API_BASE}tasks/completed/by_completion_date?limit=${limit}`;
    const res = UrlFetchApp.fetch(url, {headers: _getHeaders()});
    return JSON.parse(res.getContentText()).results || [];
  } catch (e) { return []; }
}

function _processTaskForDB(task, pMap, status) {
  const pName = pMap[task.project_id] || "Inbox";
  let due = task.due ? (task.due.datetime || task.due.date || "") : "";
  const prio = {4:"🚨 P1", 3:"🔸 P2", 2:"🔹 P3", 1:"⚪ P4"}[task.priority] || "⚪ P4";
  return [task.id, pName, task.content, due, task.description || "", status, prio, `${status} [${prio}] ${task.content}`, `https://todoist.com/app/task/${task.id}`];
}

// ============================================================================
// SHEET HELPERS
// ============================================================================

function _setupSheetHeaders(sheet) {
  const headers = ['Event ID', 'Titel', 'Start Datum', 'Start Tijd', 'Eind Datum', 'Eind Tijd', 'Werktijd', 'Locatie', 'Team Prefix', 'Shift Type', 'Prioriteit', 'Duur (uur)', 'Weeknr', 'Dag', 'Status', 'Beschrijving', 'Hele Dag', 'Hash', 'Todoist ID', 'Archief ID', 'Laatst Bijgewerkt'];
  if (sheet.getLastRow() === 0) {
    sheet.getRange(1, 1, 1, headers.length).setValues([headers]).setFontWeight('bold');
    sheet.setFrozenRows(1);
  } else {
    const current = sheet.getRange(1, 1, 1, sheet.getLastColumn()).getValues()[0];
    // Update header als verplichte kolommen ontbreken
    const needsUpdate = !current.includes('Todoist ID') || !current.includes('Archief ID');
    if (needsUpdate) {
      sheet.getRange(1, 1, 1, headers.length).setValues([headers]);
    }
  }
  return headers;
}

function _computeEventHash(event) {
  const data = [event.getTitle(), event.getStartTime().toISOString(), event.getEndTime().toISOString(), event.getLocation(), event.getDescription()].join("|");
  return Utilities.computeDigest(Utilities.DigestAlgorithm.MD5, data).map(b => ('0' + (b & 0xFF).toString(16)).slice(-2)).join('');
}

function _computeRowData(event, hash, headers) {
  const start = event.getStartTime();
  const end = event.getEndTime(); 
  const tz = Session.getScriptTimeZone(); 
  const isAllDay = event.isAllDayEvent();
  
  const startS = isAllDay ? "" : Utilities.formatDate(start, tz, "HH:mm");
  const endS = isAllDay ? "" : Utilities.formatDate(end, tz, "HH:mm");
  
  let status = "Opkomend";
  if (end < new Date()) status = "Gedraaid";
  else if (start < new Date() && end > new Date()) status = "Bezig";
  
  const loc = event.getLocation() || "";
  let team = "?"; 
  if (loc.toLowerCase().includes("appartementen")) team = "R.";
  else if (loc.toLowerCase().includes("aa")) team = "A.";

  let type = "Dienst"; 
  let prio = 1;
  if (!isAllDay) {
    if (start.getHours() < 10) { type = "Vroeg"; prio = 4; }
    else if (start.getHours() >= 13) { type = "Laat"; prio = 2; }
  }

  const map = {
    'Event ID': event.getId(),
    'Titel': event.getTitle(),
    'Start Datum': _formatDate(start),
    'Start Tijd': startS,
    'Eind Datum': _formatDate(end),
    'Eind Tijd': endS,
    'Werktijd': isAllDay ? "Hele Dag" : `${startS} - ${endS}`,
    'Locatie': loc,
    'Team Prefix': team,
    'Shift Type': type,
    'Prioriteit': prio,
    'Duur (uur)': Math.round(((end - start) / 36e5) * 100) / 100,
    // ISO 8601 week notatie (YYYY-WNN) — zero-padded zodat Sheets dit NOOIT als
    // datum interpreteert. Zonder W-prefix zet Sheets '2026-12' om naar 1 dec 2026.
    'Weeknr': `${Utilities.formatDate(start, tz, "YYYY")}-W${Utilities.formatDate(start, tz, "ww")}`,
    'Dag': ["Zondag","Maandag","Dinsdag","Woensdag","Donderdag","Vrijdag","Zaterdag"][start.getDay()],
    'Status': status,
    'Beschrijving': event.getDescription() || "",
    'Hele Dag': isAllDay ? "Ja" : "Nee",
    'Hash': hash,
    'Archief ID': '',        // Wordt gevuld door de sync loop na archivering
    'Todoist ID': '',        // Wordt gevuld door de sync loop
    'Laatst Bijgewerkt': Utilities.formatDate(new Date(), tz, "yyyy-MM-dd HH:mm:ss")
  };
  return headers.map(h => map[h] !== undefined ? map[h] : '');
}

function _setupConditionalFormatting(sheet, headers) {
  sheet.clearConditionalFormatRules();
  const statCol = _getColLetter(headers.indexOf('Status') + 1);
  const prioCol = headers.indexOf('Prioriteit') + 1;
  const range = sheet.getRange(2, 1, sheet.getMaxRows(), sheet.getMaxColumns());
  
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
  return Utilities.formatDate(d, Session.getScriptTimeZone(), "yyyy-MM-dd");
}

/**
 * Controleert of een waarde eruitziet als een echt Todoist ID.
 * Filtert epoch-datums (1970-01-01...), Sheets-datums (2025-11-27...) en lege strings eruit.
 * Echte Todoist IDs zijn alfanumerieke strings zoals '6g965HhrMRVFFCmg'.
 */
function _isValidTodoistId(val) {
  if (!val && val !== 0) return false;
  // Sheets levert datumcellen terug als Date objecten (NIET als strings).
  // String(new Date('1970-01-01')) = "Thu Jan 01 1970 01:00:00 GMT+0100..." → regex match nooit.
  if (val instanceof Date) return false;
  const s = String(val).trim();
  if (s === '') return false;
  // Patroon: datum- of timestamp-achtige strings → ongeldig (voor string-representaties)
  if (/^\d{4}-\d{2}-\d{2}/.test(s)) return false; // '2025-11-27 22:40:27' of '1970-01-01'
  if (/^\d{1,2}-\d{1,2}-\d{4}/.test(s)) return false; // '27-11-2025' (NL formaat)
  return true;
}

/**
 * Ruimt legacy garbage-waarden op in de Todoist ID kolom.
 * Doet: epoch-datums, timestamps en NL-datums → lege string.
 * Veilig: alleen cellen die geen geldig Todoist ID bevatten worden gereset.
 * Run via 🚀 Master Tools → 🧽 Opschoon Legacy Todoist IDs
 */
function cleanupLegacyTodoistIds() {
  const ss = SpreadsheetApp.getActiveSpreadsheet();
  const sheet = ss.getSheetByName(CONFIG.SHEET_NAME_ROSTER);
  if (!sheet || sheet.getLastRow() < 2) {
    safeToast('Cleanup', 'Geen data gevonden.', 5);
    return;
  }

  const headers = sheet.getRange(1, 1, 1, sheet.getLastColumn()).getValues()[0];
  const tidIdx = headers.indexOf('Todoist ID');
  if (tidIdx === -1) { safeToast('Cleanup', 'Todoist ID kolom niet gevonden.', 5); return; }

  const dataRange = sheet.getRange(2, 1, sheet.getLastRow() - 1, headers.length);
  const data = dataRange.getValues();

  let fixed = 0;
  data.forEach((row, i) => {
    const val = row[tidIdx];
    if (!_isValidTodoistId(val) && val !== '') {
      Logger.log(`🧹 Rij ${i+2}: Todoist ID '${val}' → leeg`);
      row[tidIdx] = '';
      fixed++;
    }
  });

  if (fixed > 0) {
    dataRange.setValues(data);
    Logger.log(`✅ ${fixed} legacy Todoist IDs opgeschoond.`);
    safeToast('Opschoning', `✅ ${fixed} ongeldige Todoist IDs verwijderd.`, 8);
  } else {
    safeToast('Opschoning', 'Geen garbage IDs gevonden — sheet is al schoon ✅', 8);
  }
}

function _getColLetter(c) {
  let l = '';
  while (c > 0) {
    let t = (c - 1) % 26;
    l = String.fromCharCode(t + 65) + l;
    c = (c - t - 1) / 26;
  }
  return l;
}

/**
 * Eenmalige opschoning: SLUIT alle Todoist taken waarvan de dienst al voorbij is.
 * Taken worden GESLOTEN (niet verwijderd) zodat ze zichtbaar blijven in geschiedenis.
 * Wordt getriggerd via 🚀 Master Tools → Eenmalige Opschoning.
 */
function purgeLegacyTasks() {
  let ui = null;
  try { ui = SpreadsheetApp.getUi(); } catch (e) {}

  Logger.log('🧹 Start purgeLegacyTasks (CLOSE modus)...');

  try {
    const tasks = fetchPaginated('tasks');
    const now = new Date();
    now.setHours(0, 0, 0, 0);

    const roosterTasks = tasks.filter(t =>
      t.description && t.description.includes('[EID:')
    );

    const expired = roosterTasks.filter(t => {
      if (!t.due) return false;
      const dueStr = t.due.datetime || t.due.date;
      if (!dueStr) return false;
      const due = new Date(dueStr);
      due.setHours(0, 0, 0, 0);
      return due < now;
    });

    Logger.log(`Gevonden: ${roosterTasks.length} rooster-taken, ${expired.length} verlopen.`);

    if (expired.length === 0) {
      safeToast('Opschoning', 'Geen verlopen taken gevonden — Todoist is al schoon ✅', 8);
      if (ui) ui.alert('Geen verlopen taken gevonden. Todoist is al schoon ✅');
      return;
    }

    if (ui) {
      const resp = ui.alert(
        '🧹 Todoist Opschoning',
        `${expired.length} verlopen dienst-taken gevonden.\n\nDeze worden als VOLTOOID gemarkeerd (niet verwijderd) — ze blijven zichtbaar in je geschiedenis.\n\nDoorgaan?`,
        ui.ButtonSet.YES_NO
      );
      if (resp !== ui.Button.YES) { Logger.log('Opschoning geannuleerd.'); return; }
    }

    let closed = 0, failed = 0;

    expired.forEach(task => {
      try {
        const res = UrlFetchApp.fetch(CONFIG.TODOIST_API_BASE + `tasks/${task.id}/close`, {
          method: 'post',
          headers: { 'Authorization': 'Bearer ' + CONFIG.TODOIST_API_TOKEN },
          muteHttpExceptions: true
        });
        if (res.getResponseCode() === 204) {
          closed++;
          Logger.log(`✅ Gesloten: "${task.content}" (ID: ${task.id})`);
        } else {
          failed++;
          Logger.log(`❌ Fout bij sluiten ${task.id}: HTTP ${res.getResponseCode()}`);
        }
        Utilities.sleep(100);
      } catch (e) {
        failed++;
        Logger.log(`Fout: ${e.message}`);
      }
    });

    const msg = `Opschoning klaar! ${closed} taken voltooid${failed > 0 ? `, ${failed} mislukt` : ''}.`;
    Logger.log(msg);

    safeToast('Opschoning Voltooid', msg, 10);
    if (ui) ui.alert(msg);

  } catch (e) {
    Logger.log(`purgeLegacyTasks fout: ${e.message}\n${e.stack}`);
    safeToast('Opschoning Mislukt', e.message, 15);
    if (ui) ui.alert('❌ Fout: ' + e.message);
  }
}
