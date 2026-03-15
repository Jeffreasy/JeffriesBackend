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
  TODOIST_API_TOKEN: '4309998c3e3588535556645b55f67769ea65430c', 
  SHEET_NAME_ROSTER: 'DienstenData',
  SHEET_NAME_DASHBOARD: 'Todoist_Dashboard',
  SHEET_NAME_DB: 'Tasks_Todoist_DB',
  SYNC_DAYS_FORWARD: 90,
  TODOIST_PROJECT_ID: null, 
  TODOIST_LABEL: 'Rooster',
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
  props.setProperty('HOMEAPP_SYNC_KEY', 'homeapp-gas-sync-2026-secure');
  props.setProperty('HOMEAPP_USER_ID', CLERK_USER_ID);
  props.setProperty('HOMEAPP_CONVEX_URL', 'https://adorable-mink-458.eu-west-1.convex.site');

  Logger.log(`✅ Ingesteld! HOMEAPP_SYNC_KEY + HOMEAPP_USER_ID = ${CLERK_USER_ID}`);
  SpreadsheetApp.getActiveSpreadsheet().toast(
    `✅ Homeapp gekoppeld! User: ${CLERK_USER_ID}`,
    '☁️ Setup Voltooid', 8
  );
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
    .addSeparator()
    .addItem('📊 Update Todoist Dashboard', 'mainTodoistDashboardSync')
    .addItem('📊 Bouw Diensten Dashboard', 'buildOptimizedDashboard')
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

    const startScanDate = new Date();
    const endScanDate = new Date(startScanDate);
    endScanDate.setDate(endScanDate.getDate() + CONFIG.SYNC_DAYS_FORWARD);
    
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

      if (new Date(event.getEndTime()) > new Date()) { 
        let result;
        if (todoistId) {
          // ✅ Directe check op hash zonder extra API call
          if (existingHash === currentHash) {
            stats.unchanged++;
            newRow[todoistIdIdx] = todoistId;
          } else {
            result = _syncToTodoist(event, todoistId, currentHash);
            if (result) {
              newRow[todoistIdIdx] = result;
              stats.updated++;
            }
          }
        } else {
          Logger.log(`➕ Nieuwe taak: ${event.getTitle()}`);
          result = _syncToTodoist(event, null, currentHash);
          if (result) {
            newRow[todoistIdIdx] = result;
            stats.added++;
            Logger.log(`   → ID: ${result}`);
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
        const tId = mapped ? mapped.id : row[todoistIdIdx];
        // BUG FIX 3: _deleteTodoistTask i.p.v. _closeTodoistTask —
        //            gecancelde dienst als 'gedaan' markeren is misleidend.
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

    // 🔁 Push data automatisch naar Homeapp (Convex)
    try {
      const pushResult = pushScheduleToConvex(sheet, headers);
      Logger.log(`☁️ Convex push: ${pushResult}`);
      safeToast("☁️ Homeapp Sync", pushResult, 5);
    } catch (pushErr) {
      Logger.log(`⚠️ Convex push mislukt: ${pushErr.message}`);
      safeToast("⚠️ Homeapp Push", `${pushErr.message}`, 8);
    }
    
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

  const payload = {
    content: title,
    description: description,
    label_ids: LABEL_ID_ROOSTER ? [LABEL_ID_ROOSTER] : []
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

    if (res.getResponseCode() >= 400) {
      Logger.log(`Sync error ${res.getResponseCode()}: ${res.getContentText()}`);
      return null;
    }

    return JSON.parse(res.getContentText()).id;
  } catch (e) {
    Logger.log(`_syncToTodoist fout: ${e.message}`);
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
    UrlFetchApp.fetch(CONFIG.TODOIST_API_BASE + `tasks/${taskId}/close`, {
      method: "post",
      headers: { "Authorization": "Bearer " + CONFIG.TODOIST_API_TOKEN }
    });
  } catch (e) {}
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
  const headers = ['Event ID', 'Titel', 'Start Datum', 'Start Tijd', 'Eind Datum', 'Eind Tijd', 'Werktijd', 'Locatie', 'Team Prefix', 'Shift Type', 'Prioriteit', 'Duur (uur)', 'Weeknr', 'Dag', 'Status', 'Beschrijving', 'Hele Dag', 'Hash', 'Todoist ID', 'Laatst Bijgewerkt'];
  if (sheet.getLastRow() === 0) {
    sheet.getRange(1, 1, 1, headers.length).setValues([headers]).setFontWeight('bold');
    sheet.setFrozenRows(1);
  } else {
    const current = sheet.getRange(1, 1, 1, sheet.getLastColumn()).getValues()[0];
    if (current.indexOf('Todoist ID') === -1) {
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
    'Weeknr': `${Utilities.formatDate(start, tz, "yyyy")}-${Utilities.formatDate(start, tz, "w")}`,
    'Dag': ["Zondag","Maandag","Dinsdag","Woensdag","Donderdag","Vrijdag","Zaterdag"][start.getDay()],
    'Status': status,
    'Beschrijving': event.getDescription() || "",
    'Hele Dag': isAllDay ? "Ja" : "Nee",
    'Hash': hash,
    'Laatst Bijgewerkt': Utilities.formatDate(new Date(), tz, "yyyy-MM-dd HH:mm:ss")
  };
  return headers.map(h => map[h] || "");
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
 * Eenmalige opschoning: verwijder alle Todoist taken waarvan de dienst
 * al voorbij is (due_date of due_datetime in het verleden).
 * Wordt getriggerd via 🚀 Master Tools → Eenmalige Opschoning.
 */
function purgeLegacyTasks() {
  let ui = null;
  try { ui = SpreadsheetApp.getUi(); } catch (e) {}

  Logger.log('🧹 Start purgeLegacyTasks...');

  try {
    const tasks = fetchPaginated('tasks');
    const now = new Date();
    now.setHours(0, 0, 0, 0); // vergelijk op dagbasis

    // Filter: alleen rooster-taken (hebben [EID:...] in description)
    const roosterTasks = tasks.filter(t =>
      t.description && t.description.includes('[EID:')
    );

    // Bepaal welke taken verlopen zijn (due date in het verleden)
    const expired = roosterTasks.filter(t => {
      if (!t.due) return false; // geen due date → sla over
      const dueStr = t.due.datetime || t.due.date;
      if (!dueStr) return false;
      const due = new Date(dueStr);
      due.setHours(0, 0, 0, 0);
      return due < now; // in het verleden
    });

    Logger.log(`Gevonden: ${roosterTasks.length} rooster-taken, ${expired.length} verlopen.`);

    if (expired.length === 0) {
      safeToast('Opschoning', 'Geen verlopen taken gevonden — Todoist is al schoon ✅', 8);
      if (ui) ui.alert('Geen verlopen taken gevonden. Todoist is al schoon ✅');
      return;
    }

    // Bevestiging vragen
    if (ui) {
      const resp = ui.alert(
        '🧹 Todoist Opschoning',
        `${expired.length} verlopen dienst-taken gevonden (due date in het verleden).\n\nDeze worden permanent verwijderd uit Todoist.\n\nDoorgaan?`,
        ui.ButtonSet.YES_NO
      );
      if (resp !== ui.Button.YES) {
        Logger.log('Opschoning geannuleerd door gebruiker.');
        return;
      }
    }

    // Verwijder verlopen taken
    let deleted = 0;
    let failed = 0;

    expired.forEach(task => {
      try {
        const res = UrlFetchApp.fetch(CONFIG.TODOIST_API_BASE + `tasks/${task.id}`, {
          method: 'delete',
          headers: { 'Authorization': 'Bearer ' + CONFIG.TODOIST_API_TOKEN },
          muteHttpExceptions: true
        });
        if (res.getResponseCode() === 204 || res.getResponseCode() === 200) {
          deleted++;
          Logger.log(`🗑️ Verwijderd: "${task.content}" (ID: ${task.id})`);
        } else {
          failed++;
          Logger.log(`❌ Fout bij verwijderen ${task.id}: HTTP ${res.getResponseCode()}`);
        }
        Utilities.sleep(100); // voorkom rate limiting
      } catch (e) {
        failed++;
        Logger.log(`Fout: ${e.message}`);
      }
    });

    const msg = `Opschoning klaar! ${deleted} taken verwijderd${failed > 0 ? `, ${failed} mislukt` : ''}.`;
    Logger.log(msg);
    safeToast('Opschoning Voltooid', msg, 10);
    if (ui) ui.alert(msg);

  } catch (e) {
    Logger.log(`purgeLegacyTasks fout: ${e.message}\n${e.stack}`);
    safeToast('Opschoning Mislukt', e.message, 15);
    if (ui) ui.alert('❌ Fout: ' + e.message);
  }
}

// ============================================================================
// ☁️ CONVEX PUSH — Homeapp Real-time Sync
// ============================================================================
// Instellingen (eenmalig): Ga naar Extensies → Apps Script → Projectinstellingen
// → Script properties → Voeg toe:
//   HOMEAPP_SYNC_KEY = <jouw geheime sleutel>
//   HOMEAPP_USER_ID  = <jouw Clerk user ID (bijv. user_2xyz...)>
//   HOMEAPP_CONVEX_URL = https://adorable-mink-458.eu-west-1.convex.site

/**
 * Leest de DienstenData sheet en pusht alle rijen naar Convex
 * @param {GoogleAppsScript.Spreadsheet.Sheet} sheet
 * @param {string[]} headers
 * @returns {string} Resultaat bericht
 */
function pushScheduleToConvex(sheet, headers) {
  const props = PropertiesService.getScriptProperties();
  const syncKey = props.getProperty('HOMEAPP_SYNC_KEY');
  const userId  = props.getProperty('HOMEAPP_USER_ID');
  const baseUrl = props.getProperty('HOMEAPP_CONVEX_URL') 
                  || 'https://adorable-mink-458.eu-west-1.convex.site';

  if (!syncKey) throw new Error('HOMEAPP_SYNC_KEY niet ingesteld in Script Properties');
  if (!userId)  throw new Error('HOMEAPP_USER_ID niet ingesteld in Script Properties');

  const lastRow = sheet.getLastRow();
  if (lastRow < 2) return 'Geen diensten om te pushen';

  const dataRange = sheet.getRange(2, 1, lastRow - 1, headers.length);
  const rows = dataRange.getValues();

  // Map header naam → index
  const hi = {};
  headers.forEach((h, i) => hi[h] = i);

  const diensten = [];
  rows.forEach(row => {
    const eventId   = String(row[hi['Event ID']] || '').trim();
    const status    = String(row[hi['Status']]   || '').trim();
    if (!eventId || status === 'VERWIJDERD') return;

    const rawDuur = row[hi['Duur (uur)']] ?? 0;
    const duur = typeof rawDuur === 'number' ? rawDuur
               : parseFloat(String(rawDuur).replace(',', '.')) || 0;

    diensten.push({
      userId,
      eventId,
      titel:        String(row[hi['Titel']]         || ''),
      startDatum:   _gasDateToIso(row[hi['Start Datum']]),
      startTijd:    _gasTimeToHHMM(row[hi['Start Tijd']]),
      eindDatum:    _gasDateToIso(row[hi['Eind Datum']]),
      eindTijd:     _gasTimeToHHMM(row[hi['Eind Tijd']]),
      werktijd:     String(row[hi['Werktijd']]       || ''),
      locatie:      String(row[hi['Locatie']]        || ''),
      team:         String(row[hi['Team Prefix']]    || ''),
      shiftType:    String(row[hi['Shift Type']]     || 'Dienst'),
      prioriteit:   Number(row[hi['Prioriteit']]     || 1),
      duur,
      weeknr:       String(row[hi['Weeknr']]         || ''),
      dag:          String(row[hi['Dag']]            || ''),
      status,
      beschrijving: String(row[hi['Beschrijving']]   || ''),
      heledag:      String(row[hi['Hele Dag']]       || 'Nee').toLowerCase() === 'ja',
    });
  });

  return pushToConvex(baseUrl, syncKey, userId, diensten);
}

/**
 * Doet de daadwerkelijke HTTP POST naar Convex
 */
function pushToConvex(baseUrl, syncKey, userId, diensten) {
  const url = `${baseUrl}/sync-schedule`;
  const payload = JSON.stringify({ userId, diensten });

  const options = {
    method: 'post',
    contentType: 'application/json',
    headers: { 'Authorization': `Bearer ${syncKey}` },
    payload,
    muteHttpExceptions: true,
  };

  const resp = UrlFetchApp.fetch(url, options);
  const code = resp.getResponseCode();
  const body = JSON.parse(resp.getContentText() || '{}');

  if (code !== 200 || !body.ok) {
    throw new Error(`HTTP ${code}: ${body.error || resp.getContentText()}`);
  }

  return `✅ ${body.count} diensten gesynchroniseerd naar Homeapp`;
}

/**
 * Converteert GAS Date object of string naar "YYYY-MM-DD"
 */
function _gasDateToIso(val) {
  if (!val) return '';
  if (val instanceof Date) {
    return Utilities.formatDate(val, Session.getScriptTimeZone(), 'yyyy-MM-dd');
  }
  const s = String(val).trim();
  // Al in YYYY-MM-DD formaat
  if (/^\d{4}-\d{2}-\d{2}/.test(s)) return s.slice(0, 10);
  // DD-MM-YYYY formaat
  if (/^\d{2}-\d{2}-\d{4}/.test(s)) {
    const [d, m, y] = s.split('-');
    return `${y}-${m}-${d}`;
  }
  return s;
}


/**
 * Converteert GAS tijdwaarde → "HH:MM"
 * GAS leest tijdkolommen als Date-objecten op 30-12-1899 (epoch 0)
 */
function _gasTimeToHHMM(val) {
  if (!val && val !== 0) return '';
  if (val instanceof Date) {
    // GAS Date met tijdcomponent op 1899-12-30
    return Utilities.formatDate(val, Session.getScriptTimeZone(), 'HH:mm');
  }
  if (typeof val === 'number') {
    // Excel/Sheets fractioneel getal: 0.208333 = 05:00
    const totalMinutes = Math.round(val * 24 * 60);
    const h = Math.floor(totalMinutes / 60) % 24;
    const m = totalMinutes % 60;
    return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}`;
  }
  const s = String(val).trim();
  if (/^\d{1,2}:\d{2}/.test(s)) return s.slice(0, 5);
  return s;
}
