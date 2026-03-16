/**
 * ============================================================================
 * CONVEX.GS — Homeapp Real-time Sync
 * ============================================================================
 * Verantwoordelijk voor:
 *   - Lezen van DienstenData sheet en omzetten naar Convex-formaat
 *   - HTTP POST naar het /sync-schedule endpoint
 *   - GAS datum/tijd conversie helpers
 *
 * Setup (eenmalig):
 *   Ga naar Extensies → Apps Script → Projectinstellingen → Script properties:
 *     HOMEAPP_SYNC_KEY  = <jouw geheime sleutel>
 *     HOMEAPP_USER_ID   = <jouw Clerk user ID (bijv. user_2xyz...)>
 *     HOMEAPP_CONVEX_URL = https://adorable-mink-458.eu-west-1.convex.site
 *   Of run setHomeappProperties() eenmalig via het menu.
 * ============================================================================
 */

/**
 * Leest de DienstenData sheet en pusht alle rijen naar Convex.
 * @param {GoogleAppsScript.Spreadsheet.Sheet} sheet
 * @param {string[]} headers
 * @returns {string} Resultaat bericht
 */
function pushScheduleToConvex(sheet, headers) {
  const props   = PropertiesService.getScriptProperties();
  const syncKey = props.getProperty('HOMEAPP_SYNC_KEY');
  const userId  = props.getProperty('HOMEAPP_USER_ID');
  const baseUrl = props.getProperty('HOMEAPP_CONVEX_URL')
                  || 'https://adorable-mink-458.eu-west-1.convex.site';

  if (!syncKey) throw new Error('HOMEAPP_SYNC_KEY niet ingesteld in Script Properties');
  if (!userId)  throw new Error('HOMEAPP_USER_ID niet ingesteld in Script Properties');

  const lastRow = sheet.getLastRow();
  if (lastRow < 2) return 'Geen diensten om te pushen';

  const rows = sheet.getRange(2, 1, lastRow - 1, headers.length).getValues();

  // Map header naam → index
  const hi = {};
  headers.forEach((h, i) => hi[h] = i);

  const diensten = [];
  rows.forEach(row => {
    const eventId = String(row[hi['Event ID']] || '').trim();
    const status  = String(row[hi['Status']]   || '').trim();
    if (!eventId || status === 'VERWIJDERD') return;

    const rawDuur = row[hi['Duur (uur)']] ?? 0;
    const duur    = typeof rawDuur === 'number' ? rawDuur
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
 * Doet de daadwerkelijke HTTP POST naar Convex.
 */
function pushToConvex(baseUrl, syncKey, userId, diensten) {
  const url     = `${baseUrl}/sync-schedule`;
  const payload = JSON.stringify({ userId, diensten });

  const resp = UrlFetchApp.fetch(url, {
    method:      'post',
    contentType: 'application/json',
    headers:     { 'Authorization': `Bearer ${syncKey}` },
    payload,
    muteHttpExceptions: true,
  });

  const code = resp.getResponseCode();
  // V4: JSON.parse in try/catch — bij gateway errors (502) geeft server HTML terug, geen JSON
  let body = {};
  try { body = JSON.parse(resp.getContentText() || '{}'); } catch (_) {}

  if (code !== 200 || !body.ok) {
    throw new Error(`HTTP ${code}: ${body.error || resp.getContentText().slice(0, 200)}`);
  }

  return `✅ ${body.count} diensten gesynchroniseerd naar Homeapp`;
}

// ============================================================================
// SALARIS PUSH → CONVEX /sync-salary
// ============================================================================

/**
 * Pusht alle maand-resultaten van buildSalarisSheet() naar de Convex salary table.
 * Wordt automatisch aangeroepen aan het einde van buildSalarisSheet().
 *
 * @param {Array} resultaten  Array van maand-objecten uit _berekenMaandloon()
 * @returns {string} Resultaat bericht
 */
function pushSalarisToConvex(resultaten) {
  const props   = PropertiesService.getScriptProperties();
  const syncKey = props.getProperty('HOMEAPP_SYNC_KEY');
  const userId  = props.getProperty('HOMEAPP_USER_ID');
  const baseUrl = props.getProperty('HOMEAPP_CONVEX_URL')
                  || 'https://adorable-mink-458.eu-west-1.convex.site';

  if (!syncKey || !userId) {
    Logger.log('⚠️ pushSalarisToConvex: credentials niet ingesteld — skip');
    return '⚠️ Salaris push overgeslagen (credentials niet geconfigureerd)';
  }

  const now = new Date().toISOString();

  const salarisData = resultaten.map(r => ({
    userId,
    periode:            r.maandLabel,
    jaar:               r.jaar,
    maand:              r.maand,
    aantalDiensten:     r.aantalDiensten,
    uurloonORT:         r.tarieven.uurloonORT,
    basisLoon:          r.basisLoon,
    amtZeerintensief:   r.amtZeerintensief,
    toeslagBalansvif:   r.toeslagBalansvif,
    ortTotaal:          r.ortTotaalBedrag,
    extraUrenBedrag:    r.extraUrenBedrag,
    toeslagVakatieUren: r.toeslagVakatieUren,
    reiskosten:         r.reiskosten,
    eenmaligTotaal:     r.eenmaligTotaal,
    brutoBetaling:      r.brutoBetaling,
    pensioenpremie:     r.pensioenpremie,
    loonheffingSchat:   r.loonheffingSchat,
    nettoPrognose:      r.nettoPrognose,
    ortDetail:          JSON.stringify(r.ortTotalen),
    eenmaligDetail:     r.eenmalig.length > 0 ? JSON.stringify(r.eenmalig) : undefined,
    berekendOp:         now,
  }));

  let resp;
  try {
    resp = UrlFetchApp.fetch(`${baseUrl}/sync-salary`, {
      method:      'post',
      contentType: 'application/json',
      payload:     JSON.stringify({ userId, salarisData }),
      headers:     { Authorization: `Bearer ${syncKey}` },
      muteHttpExceptions: true,
    });
  } catch (e) {
    throw new Error(`pushSalarisToConvex netwerk fout: ${e.message}`);
  }

  const code = resp.getResponseCode();
  let body = {};
  try { body = JSON.parse(resp.getContentText() || '{}'); } catch (_) {}

  if (code !== 200 || !body.ok) {
    throw new Error(`Salaris sync HTTP ${code}: ${body.error || resp.getContentText().slice(0, 200)}`);
  }

  Logger.log(`☁️ Salaris push: ${body.count} maanden gepusht naar Convex`);
  return `☁️ ${body.count} maanden gesynchroniseerd naar Homeapp`;
}

// ============================================================================
// DATUM / TIJD HELPERS
// ============================================================================


/**
 * Converteert GAS Date object of string naar "YYYY-MM-DD".
 * GAS leest datumkolommen als Date objecten — dit handelt beide gevallen af.
 */
function _gasDateToIso(val) {
  if (!val) return '';
  if (val instanceof Date) {
    return Utilities.formatDate(val, Session.getScriptTimeZone(), 'yyyy-MM-dd');
  }
  const s = String(val).trim();
  if (/^\d{4}-\d{2}-\d{2}/.test(s)) return s.slice(0, 10); // Al in YYYY-MM-DD
  if (/^\d{2}-\d{2}-\d{4}/.test(s)) {                       // DD-MM-YYYY
    const [d, m, y] = s.split('-');
    return `${y}-${m}-${d}`;
  }
  return s;
}

/**
 * Converteert GAS tijdwaarde → "HH:MM".
 * GAS leest tijdkolommen als Date-objecten op 30-12-1899 (epoch 0).
 */
function _gasTimeToHHMM(val) {
  if (!val && val !== 0) return '';
  if (val instanceof Date) {
    return Utilities.formatDate(val, Session.getScriptTimeZone(), 'HH:mm');
  }
  if (typeof val === 'number') {
    // Excel/Sheets fractioneel getal: 0.208333... = 05:00
    const totalMinutes = Math.round(val * 24 * 60);
    const h = Math.floor(totalMinutes / 60) % 24;
    const m = totalMinutes % 60;
    return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}`;
  }
  const s = String(val).trim();
  if (/^\d{1,2}:\d{2}/.test(s)) return s.slice(0, 5);
  return s;
}
