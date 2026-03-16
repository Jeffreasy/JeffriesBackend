/**
 * ============================================================================
 * TODOIST.GS — Todoist API Integratie
 * ============================================================================
 * Verantwoordelijk voor:
 *   - Paginatie helper (fetchPaginated)
 *   - Label caching
 *   - Taken aanmaken / bijwerken / sluiten / verwijderen
 *   - Todoist dashboard sync
 *   - Diagnose en opschoning functies
 * ============================================================================
 */

// Label ID cache (in-memory, per script-run)
let LABEL_ID_ROOSTER = null;

// ============================================================================
// PAGINATIE HELPER
// ============================================================================

function fetchPaginated(endpoint) {
  let allItems = [];
  let cursor   = null;

  do {
    let url = CONFIG.TODOIST_API_BASE + endpoint;
    if (cursor) url += (url.includes('?') ? '&' : '?') + `cursor=${encodeURIComponent(cursor)}`;

    const res = UrlFetchApp.fetch(url, {
      headers: { 'Authorization': 'Bearer ' + CONFIG.TODOIST_API_TOKEN },
      muteHttpExceptions: true
    });

    if (res.getResponseCode() !== 200) {
      Logger.log(`Fetch ${endpoint} fout: HTTP ${res.getResponseCode()} - ${res.getContentText()}`);
      return allItems;
    }

    const data  = JSON.parse(res.getContentText());
    const items = data.results || [];

    if (!Array.isArray(items)) {
      Logger.log(`Onverwachte response bij ${endpoint}: ${JSON.stringify(data)}`);
      return allItems;
    }

    allItems = allItems.concat(items);
    cursor   = data.next_cursor || null;
    Logger.log(`Fetched ${items.length} items from ${endpoint}, next_cursor: ${cursor || 'geen'}`);
  } while (cursor);

  return allItems;
}

// ============================================================================
// LABEL CACHE
// ============================================================================

function cacheLabelIds() {
  if (LABEL_ID_ROOSTER !== null) return LABEL_ID_ROOSTER;

  try {
    const labels       = fetchPaginated('labels');
    const roosterLabel = labels.find(l => l.name?.toLowerCase() === CONFIG.TODOIST_LABEL.toLowerCase());

    if (roosterLabel?.id) {
      LABEL_ID_ROOSTER = roosterLabel.id;
      Logger.log(`✅ Label '${CONFIG.TODOIST_LABEL}' gevonden → ID: ${LABEL_ID_ROOSTER}`);
    } else {
      Logger.log(`⚠️ Label '${CONFIG.TODOIST_LABEL}' niet gevonden. Beschikbaar: ${labels.map(l => l.name || 'onbekend').join(', ')}`);
    }
  } catch (e) {
    Logger.log(`Label cache fout: ${e.message}`);
  }

  return LABEL_ID_ROOSTER;
}

// ============================================================================
// TAKEN SYNC HELPERS
// ============================================================================

/**
 * Bouw een Map van Event ID → { id, hash } op basis van actieve Todoist taken.
 * Verwijdert duplicaten automatisch.
 */
function _buildTodoistMapAndCleanup(stats) {
  const map = new Map();

  try {
    const tasks   = fetchPaginated('tasks');
    const eidSeen = new Map();

    tasks.forEach(task => {
      const eidMatch  = task.description ? task.description.match(/\[EID:(.*?)\]/) : null;
      const hashMatch = task.description ? task.description.match(/Hash:\s([a-f0-9]+)/) : null;

      if (eidMatch) {
        const eid  = eidMatch[1];
        const hash = hashMatch ? hashMatch[1] : null;

        if (eidSeen.has(eid)) {
          _deleteTodoistTask(task.id);
          stats.deduped++;
          Logger.log(`Duplicaat verwijderd tijdens scan: ${task.id} (EID ${eid})`);
        } else {
          eidSeen.set(eid, task.id);
          map.set(eid, { id: task.id, hash });
        }
      }
    });
  } catch (e) {
    Logger.log(`Map bouwen fout: ${e.message}`);
  }

  return map;
}

/**
 * Maak een nieuwe Todoist taak aan of update een bestaande.
 * @returns {string|null} Todoist task ID
 */
function _syncToTodoist(event, existingTaskId, currentHash) {
  if (!CONFIG.TODOIST_API_TOKEN) return null;

  const loc       = (event.getLocation() || '').toLowerCase();
  const start     = event.getStartTime();
  const end       = event.getEndTime();
  const startHour = start.getHours();

  let team = '?';
  if (loc.includes('appartementen')) team = 'R.';
  else if (loc.includes('aa'))       team = 'A.';

  let type = 'Dienst';
  if (startHour < 10)      type = 'Vroeg';
  else if (startHour >= 13) type = 'Laat';

  const title       = (team !== '?') ? `${team} ${type}` : `💼 ${event.getTitle()} (${team})`;
  const durationMin = Math.floor((end - start) / (1000 * 60)) || 15;
  const description = `Locatie: ${event.getLocation() || 'Onbekend'}\nDuur: ${Math.round(durationMin / 60 * 10) / 10} uur\nHash: ${currentHash}\n\n[EID:${event.getId()}]`;

  // V5: als team onbekend is, gebruik event-title zonder vraagteken in de taaknaam
  const title       = (team !== '?') ? `${team} ${type}` : `💼 ${type}: ${event.getTitle()}`;
  const payload = {
    content:     title,
    description: description,
    labels:      [CONFIG.TODOIST_LABEL]
  };

  if (CONFIG.TODOIST_PROJECT_ID) payload.project_id = CONFIG.TODOIST_PROJECT_ID;

  if (event.isAllDayEvent()) {
    payload.due_date = Utilities.formatDate(start, Session.getScriptTimeZone(), 'yyyy-MM-dd');
  } else {
    payload.due_datetime  = start.toISOString();
    payload.duration      = durationMin;
    payload.duration_unit = 'minute';
  }

  const headers = {
    'Authorization': 'Bearer ' + CONFIG.TODOIST_API_TOKEN,
    'Content-Type':  'application/json'
  };

  try {
    let url = CONFIG.TODOIST_API_BASE + 'tasks';
    if (existingTaskId) url += `/${existingTaskId}`;

    const res      = UrlFetchApp.fetch(url, { method: 'post', headers, payload: JSON.stringify(payload), muteHttpExceptions: true });
    const httpCode = res.getResponseCode();
    const body     = res.getContentText();

    if (httpCode >= 400) {
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
    const res = UrlFetchApp.fetch(CONFIG.TODOIST_API_BASE + `tasks/${taskId}`, {
      method:  'delete',
      headers: { 'Authorization': 'Bearer ' + CONFIG.TODOIST_API_TOKEN },
      muteHttpExceptions: true
    });
    if (res.getResponseCode() !== 204) {
      Logger.log(`⚠️ Delete task ${taskId} HTTP ${res.getResponseCode()}`);
    }
  } catch (e) {
    Logger.log(`❌ _deleteTodoistTask fout: ${e.message}`);
  }
}

function _closeTodoistTask(taskId) {
  try {
    const res = UrlFetchApp.fetch(CONFIG.TODOIST_API_BASE + `tasks/${taskId}/close`, {
      method:  'post',
      headers: { 'Authorization': 'Bearer ' + CONFIG.TODOIST_API_TOKEN },
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
// OPSCHONING FUNCTIES
// ============================================================================

/**
 * Verwijder duplicate Todoist taken (meerdere taken met zelfde EID).
 */
function cleanupTodoistDuplicates() {
  let ui = null;
  try { ui = SpreadsheetApp.getUi(); ui.alert('🗑️ Cleanup gestart – bekijk logs voor details.'); } catch (e) {}

  const stats = { removed: 0 };

  try {
    cacheLabelIds();
    const tasks  = fetchPaginated('tasks');
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
    if (ui) ui.alert('❌ Cleanup fout: ' + e.message);
  }
}

/**
 * SLUIT alle Todoist taken waarvan de dienst al voorbij is.
 * Taken worden gesloten (niet verwijderd) — blijven zichtbaar in geschiedenis.
 */
function purgeLegacyTasks() {
  let ui = null;
  try { ui = SpreadsheetApp.getUi(); } catch (e) {}

  Logger.log('🧹 Start purgeLegacyTasks (CLOSE modus)...');

  try {
    const tasks = fetchPaginated('tasks');
    const now   = new Date();
    now.setHours(0, 0, 0, 0);

    const roosterTasks = tasks.filter(t => t.description && t.description.includes('[EID:'));
    const expired      = roosterTasks.filter(t => {
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
        `${expired.length} verlopen dienst-taken gevonden.\n\nDeze worden als VOLTOOID gemarkeerd (niet verwijderd).\n\nDoorgaan?`,
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
        Utilities.sleep(150); // V6: rate limit buffer (Todoist max 50 req/sec voor REST v1)
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

// ============================================================================
// TODOIST DASHBOARD
// ============================================================================

function mainTodoistDashboardSync() {
  const ss          = SpreadsheetApp.getActiveSpreadsheet();
  let globalStats   = { active: 0, completed: 0 };
  let dbSheet       = ss.getSheetByName(CONFIG.SHEET_NAME_DB);

  if (!dbSheet) {
    dbSheet = ss.insertSheet(CONFIG.SHEET_NAME_DB);
    dbSheet.appendRow(['Task ID', 'Project', 'Content', 'Due Date', 'Description', 'Status', 'Priority', 'RAG Context', 'Link']);
    dbSheet.setFrozenRows(1);
  }

  const projectMap    = _fetchTodoistProjects();
  let allTasksBuffer  = [];

  try {
    fetchPaginated('tasks').forEach(task => {
      allTasksBuffer.push(_processTaskForDB(task, projectMap, 'Active'));
      globalStats.active++;
    });
  } catch (e) { Logger.log(`Active tasks fout: ${e.message}`); }

  try {
    _fetchTodoistCompletedTasks(50).forEach(task => {
      allTasksBuffer.push(_processTaskForDB(task, projectMap, 'Completed'));
      globalStats.completed++;
    });
  } catch (e) { Logger.log(`Completed tasks fout: ${e.message}`); }

  if (allTasksBuffer.length > 0) {
    if (dbSheet.getLastRow() > 1) dbSheet.getRange(2, 1, dbSheet.getLastRow() - 1, dbSheet.getLastColumn()).clearContent();
    dbSheet.getRange(2, 1, allTasksBuffer.length, allTasksBuffer[0].length).setValues(allTasksBuffer);
  }

  _buildDashboard(ss, globalStats);
}

function _buildDashboard(ss, stats) {
  let dash = ss.getSheetByName(CONFIG.SHEET_NAME_DASHBOARD);
  if (!dash) dash = ss.insertSheet(CONFIG.SHEET_NAME_DASHBOARD, 0);
  const c = CONFIG.COLORS;
  dash.getRange('B2').setValue('🔴 Todoist Intelligence (v1)').setFontSize(18).setFontColor(c.PRIMARY);
  dash.getRange('B3').setValue('Update: ' + _formatDate(new Date()));
  _createCard(dash, 5, 2, '⚡ Actief', stats.active, c.PRIMARY, '#ffe0e0');
  _createCard(dash, 5, 5, '🚨 P1', `=COUNTIF('${CONFIG.SHEET_NAME_DB}'!G:G, "*P1*")`, c.ERROR, '#fce8e6');
  _createCard(dash, 5, 8, '✅ Klaar', stats.completed, c.SUCCESS, '#e6f4ea');
}

function _createCard(sheet, r, c, title, val, color, bg) {
  sheet.getRange(r, c, 3, 2).merge().setBackground(bg).setBorder(true, true, true, true, null, null, '#ddd', null);
  sheet.getRange(r, c).setValue(title).setFontColor(color).setFontWeight('bold').setHorizontalAlignment('center');
  const vRange = sheet.getRange(r + 1, c);
  if (String(val).startsWith('=')) vRange.setFormula(val); else vRange.setValue(val);
  vRange.setFontSize(24).setHorizontalAlignment('center');
}

function _getHeaders() {
  return { 'Authorization': 'Bearer ' + CONFIG.TODOIST_API_TOKEN };
}

function _fetchTodoistProjects() {
  try {
    const map = {};
    fetchPaginated('projects').forEach(p => map[p.id] = p.name);
    return map;
  } catch (e) { return {}; }
}

function _fetchTodoistCompletedTasks(limit) {
  try {
    const url = `${CONFIG.TODOIST_API_BASE}tasks/completed/by_completion_date?limit=${limit}`;
    // M3: muteHttpExceptions zodat 401/429 geen harde crash geeft in dashboard sync
    const res = UrlFetchApp.fetch(url, { headers: _getHeaders(), muteHttpExceptions: true });
    if (res.getResponseCode() !== 200) {
      Logger.log(`⚠️ Completed tasks HTTP ${res.getResponseCode()}: ${res.getContentText().slice(0, 200)}`);
      return [];
    }
    return JSON.parse(res.getContentText()).results || [];
  } catch (e) { Logger.log(`Completed tasks fout: ${e.message}`); return []; }
}

function _processTaskForDB(task, pMap, status) {
  const pName = pMap[task.project_id] || 'Inbox';
  const due   = task.due ? (task.due.datetime || task.due.date || '') : '';
  const prio  = { 4: '🚨 P1', 3: '🔸 P2', 2: '🔹 P3', 1: '⚪ P4' }[task.priority] || '⚪ P4';
  return [task.id, pName, task.content, due, task.description || '', status, prio,
          `${status} [${prio}] ${task.content}`, `https://todoist.com/app/task/${task.id}`];
}

// ============================================================================
// DIAGNOSE
// ============================================================================

function debugTodoistSync() {
  const token = CONFIG.TODOIST_API_TOKEN;
  Logger.log('=== TODOIST DIAGNOSE ===');

  if (!token) {
    Logger.log('❌ STAP 1 MISLUKT: Token is null/leeg. Voer setTodoistToken() uit!');
    safeToast('Diagnose', '❌ Token ontbreekt — voer 🔑 Sla Todoist Token Op uit', 10);
    return;
  }
  Logger.log(`✅ Stap 1: Token aanwezig (eindigt op ...${token.slice(-6)})`);

  try {
    const res = UrlFetchApp.fetch(CONFIG.TODOIST_API_BASE + 'projects', {
      headers: { 'Authorization': 'Bearer ' + token },
      muteHttpExceptions: true
    });
    Logger.log(`✅ Stap 2: API bereikbaar — HTTP ${res.getResponseCode()}`);
    if (res.getResponseCode() === 401) { Logger.log('❌ HTTP 401 = Token ongeldig of verlopen!'); safeToast('Diagnose', '❌ Token ongeldig (401)', 10); return; }
    if (res.getResponseCode() !== 200) { Logger.log(`❌ Onverwachte HTTP ${res.getResponseCode()}`); return; }
  } catch (e) { Logger.log(`❌ Stap 2 MISLUKT: ${e.message}`); return; }

  const labels       = fetchPaginated('labels');
  const roosterLabel = labels.find(l => (l.name || '').toLowerCase() === CONFIG.TODOIST_LABEL.toLowerCase());
  Logger.log(roosterLabel
    ? `✅ Stap 3: Label '${CONFIG.TODOIST_LABEL}' gevonden (ID: ${roosterLabel.id})`
    : `⚠️ Stap 3: Label '${CONFIG.TODOIST_LABEL}' NIET gevonden. Beschikbaar: ${labels.map(l => l.name).join(', ')}`
  );

  const tasks        = fetchPaginated('tasks');
  const roosterTasks = tasks.filter(t => t.description && t.description.includes('[EID:'));
  Logger.log(`✅ Stap 4: ${tasks.length} actieve taken, waarvan ${roosterTasks.length} rooster-taken`);

  try {
    const testPayload = {
      content:     '🧪 DIAGNOSE TEST — mag worden verwijderd',
      description: 'Automatische diagnose. Verwijder deze taak.',
      due_date:    Utilities.formatDate(new Date(), Session.getScriptTimeZone(), 'yyyy-MM-dd'),
      labels:      roosterLabel ? [CONFIG.TODOIST_LABEL] : []
    };
    const res = UrlFetchApp.fetch(CONFIG.TODOIST_API_BASE + 'tasks', {
      method: 'post',
      headers: { 'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json' },
      payload: JSON.stringify(testPayload),
      muteHttpExceptions: true
    });
    if (res.getResponseCode() === 200) {
      const created = JSON.parse(res.getContentText());
      Logger.log(`✅ Stap 5: Test taak aangemaakt (ID: ${created.id})`);
      // V2: Diagnose test-taak direct sluiten zodat Todoist niet vegvervuilt
      _closeTodoistTask(created.id);
      Logger.log(`✅ Stap 5b: Test taak direct gesloten (${created.id})`);
      safeToast('Diagnose Gelukt ✅', 'Alles werkt! Test taak aangemaakt én gesloten.', 10);
    } else {
      Logger.log(`❌ Stap 5 MISLUKT: HTTP ${res.getResponseCode()}\nResponse: ${res.getContentText()}`);
      safeToast('Diagnose', `❌ Taak aanmaken mislukt: HTTP ${res.getResponseCode()}`, 15);
    }
  } catch (e) { Logger.log(`❌ Stap 5 fout: ${e.message}`); }

  Logger.log('=== EINDE DIAGNOSE ===');
}
