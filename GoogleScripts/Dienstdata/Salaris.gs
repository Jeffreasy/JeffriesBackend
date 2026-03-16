/**
 * ============================================================================
 * 💰 SALARIS.GS — Professionele Salarisberekening ('s Heeren Loo Zg)
 * ============================================================================
 * Werknemer : De heer JJA Lavente | Pers.Nr. 796832
 * Werkgever : 's Heeren Loo Zg, Amersfoort
 * Contract  : 44,440% deeltijd
 *
 * Data bron : Loonstroken 2025-01 t/m 2026-02 (visueel geanalyseerd)
 *
 * Functies:
 *   buildSalarisSheet()       — Bouwt/update het SalarisBerekening dashboard
 *   berekenPrognose()         — Prognose voor lopende maand op basis van rooster
 *   _berekenMaandloon()       — Core berekening per maand
 *   _classifyDienst()         — ORT-classificatie per dienst
 *   _getTarief()              — Historisch uurloon opzoeken op datum
 * ============================================================================
 */

// ============================================================================
// CONFIGURATIE — TARIEVEN & PARAMETERS
// ============================================================================

const SALARIS_CONFIG = {

  WERKNEMER:     'De heer JJA Lavente',
  PERSONEELSNR:  '796832',
  WERKGEVER:     "'s Heeren Loo Zg",
  DEELTIJDFACTOR: 0.44440, // 44,440%

  /**
   * Historische tarieven per ingangsdatum.
   * Elke entry geldt vanaf `vanaf` t/m de volgende entry (of heden).
   * Bron: loonstroken 2025-01 t/m 2026-02
   */
  TARIEF_TABEL: [
    { vanaf: '2025-01-01', salaris100: 3107.00, uurloonORT: 19.85, reiskostenKm: 0.16 },
    { vanaf: '2025-08-01', salaris100: 3231.00, uurloonORT: 20.65, reiskostenKm: 0.20 },
    { vanaf: '2025-12-01', salaris100: 3319.00, uurloonORT: 20.65, reiskostenKm: 0.20 },
    { vanaf: '2026-01-01', salaris100: 3481.00, uurloonORT: 21.21, reiskostenKm: 0.20 },
    { vanaf: '2026-02-01', salaris100: 3481.00, uurloonORT: 22.24, reiskostenKm: 0.20 },
  ],

  /**
   * ORT (Onregelmatigheidstoeslag) percentages.
   * Bron: loonstroken — consequent toegepast per dienst-type.
   */
  ORT: {
    AVOND:    { pct: 0.22, label: 'ORT 22% (avond/doordeweeks)' },
    VROEG:    { pct: 0.38, label: 'ORT 38% (vroeg)' },
    NACHT:    { pct: 0.44, label: 'ORT 44% (nacht)' },
    ZATERDAG: { pct: 0.52, label: 'ORT 52% (zaterdag)' },
    ZONDAG:   { pct: 0.60, label: 'ORT 60% (zondag)' },
  },

  // Vaste percentuele toeslagen (over basisloon incl. amt zeerintensief)
  AMT_ZEERINTENSIEF_PCT:  0.0500, // 5,00% over basisloon
  TOESLAG_BALANSVIF_PCT:  0.0304, // 3,04% over basisloon
  TOESLAG_VAKANTIEUREN_PCT: 0.0767, // 7,67% over extra-uren bedrag
  PENSIOEN_PCT:           0.1295, // 12,95% PFZW (werknemersdeel)
  VAKANTIEGELD_PCT:       0.0800, // 8,00% (uitbetaling mei)
  EINDEJAARSUITKERING_PCT: 0.0833, // 8,33% (uitbetaling december)

  // Reiskosten: woon-werkafstand (enkele reis km, 2x per werkdag)
  REISAFSTAND_KM_ENKEL: 33, // Spiegelstraat 6, Dronten → locatie ~33km (660/20 werkdagen)

  // Loonheffingstabel 2026 (tarief 1 — meest voorkomend voor parttimers)
  // Bron: Belastingdienst Loonheffingen 2026
  // SCHATTING — exacte verrekening door werkgever kan afwijken door heffingskortingen
  LOONHEFFING_TABEL_2026: [
    { tot: 38441,  tarief: 0.3597 },
    { tot: 76817,  tarief: 0.3748 },
    { tot: Infinity, tarief: 0.4950 },
  ],
  LOONHEFFINGSKORTING_2026: 3070, // Jaarlijkse algemene heffingskorting (indicatief)

  SHEET_NAAM: 'SalarisBerekening',
  DIENSTEN_SHEET: 'DienstenData',

  KLEUREN: {
    HEADER_BG:   '#1a3a5c',
    HEADER_FG:   '#FFFFFF',
    SECTIE_BG:   '#e8f0fe',
    SECTIE_FG:   '#1a3a5c',
    POSITIEF:    '#e6f4ea',
    NEGATIEF:    '#fce8e6',
    EENMALIG:    '#fff8e1',
    SUBTOTAAL:   '#d2e3fc',
    TOTAAL_BG:   '#1a3a5c',
    TOTAAL_FG:   '#FFFFFF',
    PROGNOSE:    '#f3e8ff',
  }
};

// ============================================================================
// HOOFD ENTRY POINTS
// ============================================================================

/**
 * Bouwt het volledige SalarisBerekening dashboard.
 * Leest automatisch uren uit de DienstenData sheet.
 * Loopt over alle periodes waarvoor data beschikbaar is (2025-01 t/m heden).
 */
function buildSalarisSheet() {
  const ss    = SpreadsheetApp.getActiveSpreadsheet();
  const start = Date.now();

  Logger.log('💰 Start salarisberekening...');

  try {
    // 1. Lees alle diensten uit DienstenData sheet
    const diensten = _leesAlleDiensten(ss);
    Logger.log(`📋 ${diensten.length} diensten geladen uit ${SALARIS_CONFIG.DIENSTEN_SHEET}`);

    // 2. Groepeer per jaar-maand
    const perMaand = _groeperPerMaand(diensten);

    // 3. Bereken alle maanden
    const resultaten = [];
    const maanden = Object.keys(perMaand).sort();

    for (const maandKey of maanden) {
      const [jaar, maand] = maandKey.split('-').map(Number);
      const maandDiensten = perMaand[maandKey];
      const result = _berekenMaandloon(jaar, maand, maandDiensten);
      resultaten.push(result);
      Logger.log(`✅ ${maandKey}: bruto €${result.brutoBetaling.toFixed(2)}, netto ~€${result.nettoPrognose.toFixed(2)}`);
    }

    // 4. Schrijf naar sheet
    const sheet = _initSalarisSheet(ss);
    _schrijfResultaten(sheet, resultaten);

    // 5. Push naar Convex Homeapp DB
    try {
      const pushMsg = pushSalarisToConvex(resultaten);
      Logger.log(pushMsg);
    } catch (pushErr) {
      // Push-fout is niet-fataal: sheet is al succesvol geschreven
      Logger.log(`⚠️ Salaris push naar Convex mislukt: ${pushErr.message}`);
      safeToast('⚠️ Homeapp Sync', `Sheet klaar, Convex push mislukt: ${pushErr.message}`, 10);
    }

    const duur = ((Date.now() - start) / 1000).toFixed(1);
    const msg = `💰 Salaris dashboard gebouwd! ${resultaten.length} maanden | ${duur}s`;
    Logger.log(msg);
    safeToast('SalarisBerekening', msg, 10);

  } catch (e) {
    Logger.log(`❌ buildSalarisSheet fout: ${e.message}\n${e.stack}`);
    safeToast('Salaris Fout', e.message, 15);
  }
}

/**
 * Berekent en toont een prognose voor de HUIDIGE maand
 * op basis van geplande én gedraaide diensten.
 */
function berekenPrognose() {
  const ss  = SpreadsheetApp.getActiveSpreadsheet();
  const now = new Date();

  try {
    const diensten    = _leesAlleDiensten(ss);
    const maandKey    = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}`;
    const perMaand    = _groeperPerMaand(diensten);
    const lopend      = perMaand[maandKey] || [];
    const result      = _berekenMaandloon(now.getFullYear(), now.getMonth() + 1, lopend);

    const msg = `📊 Prognose ${maandKey}:\n` +
                `Bruto: €${result.brutoBetaling.toFixed(2)}\n` +
                `Pensioen: -€${result.pensioenpremie.toFixed(2)}\n` +
                `Netto ~: €${result.nettoPrognose.toFixed(2)}\n` +
                `(${lopend.length} diensten verwerkt)`;

    Logger.log(msg);
    try {
      SpreadsheetApp.getUi().alert('💰 Maandprognose', msg, SpreadsheetApp.getUi().ButtonSet.OK);
    } catch (_) {
      safeToast('Prognose', msg, 15);
    }
  } catch (e) {
    Logger.log(`❌ berekenPrognose fout: ${e.message}`);
    safeToast('Prognose Fout', e.message, 10);
  }
}

// ============================================================================
// CORE BEREKENING
// ============================================================================

/**
 * Berekent het volledige maandloon voor een gegeven periode.
 * @param {number} jaar
 * @param {number} maand  1-12
 * @param {Array}  diensten  Gefilterde diensten voor deze maand
 * @returns {Object} Volledig berekend maand-object
 */
function _berekenMaandloon(jaar, maand, diensten) {
  const peilDatum = new Date(jaar, maand - 1, 1);
  const tarieven  = _getTarief(peilDatum);

  const { salaris100, uurloonORT, reiskostenKm } = tarieven;
  const basisLoon = salaris100 * SALARIS_CONFIG.DEELTIJDFACTOR; // 44,44% van 100%

  // ── Vaste componenten ────────────────────────────────────────────────────
  const amtZeerintensief = Math.round(basisLoon * SALARIS_CONFIG.AMT_ZEERINTENSIEF_PCT * 100) / 100;
  const toeslagBalansvif = Math.round(basisLoon * SALARIS_CONFIG.TOESLAG_BALANSVIF_PCT * 100) / 100;

  // Reiskosten: werkdagen in maand × 2 × km × tarief
  const werkdagenMaand  = _telWerkdagen(jaar, maand);
  const reiskosten      = Math.round(werkdagenMaand * 2 * SALARIS_CONFIG.REISAFSTAND_KM_ENKEL * reiskostenKm * 100) / 100;

  // ── ORT berekening per dienst ────────────────────────────────────────────
  const ortTotalen = { AVOND: 0, VROEG: 0, NACHT: 0, ZATERDAG: 0, ZONDAG: 0 };
  const ortUren    = { AVOND: 0, VROEG: 0, NACHT: 0, ZATERDAG: 0, ZONDAG: 0 };
  let   extraUrenBedrag = 0;
  let   extraUren       = 0;

  diensten.forEach(d => {
    const { uren, ortCategorie, isExtra } = _classifyDienst(d);
    if (uren <= 0) return;

    if (isExtra) {
      // Extra uren boven contracturen: ORT + basisuurloon
      extraUrenBedrag += Math.round(uren * uurloonORT * 100) / 100;
      extraUren       += uren;
    }

    if (ortCategorie && SALARIS_CONFIG.ORT[ortCategorie]) {
      const bedrag = Math.round(uren * uurloonORT * SALARIS_CONFIG.ORT[ortCategorie].pct * 100) / 100;
      ortTotalen[ortCategorie] += bedrag;
      ortUren[ortCategorie]    += uren;
    }
  });

  const ortTotaalBedrag   = Object.values(ortTotalen).reduce((s, v) => s + v, 0);
  const toeslagVakatieUren = Math.round(extraUrenBedrag * SALARIS_CONFIG.TOESLAG_VAKANTIEUREN_PCT * 100) / 100;

  // ── Eenmalige uitkeringen ────────────────────────────────────────────────
  let eenmalig = [];

  if (maand === 5) {
    // Vakantiegeld uitbetaling (mei) — schatting op basis van basisloon × 12 × 8%
    const vkgBasis  = (basisLoon + amtZeerintensief + toeslagBalansvif) * 12;
    const vkgBedrag = Math.round(vkgBasis * SALARIS_CONFIG.VAKANTIEGELD_PCT * 100) / 100;
    eenmalig.push({ label: '🌴 Vakantiegeld (8%)', bedrag: vkgBedrag });
  }

  if (maand === 12) {
    // Eindejaarsuitkering (december) — 8,33% over jaarbasis
    const edBasis  = (basisLoon + amtZeerintensief) * 12;
    const edBedrag = Math.round(edBasis * SALARIS_CONFIG.EINDEJAARSUITKERING_PCT * 100) / 100;
    eenmalig.push({ label: '🎄 Eindejaarsuitkering (8,33%)', bedrag: edBedrag });
    eenmalig.push({ label: '🎁 WKR Uitruil (belastingvrij)', bedrag: 240.00 });
  }

  const eenmaligTotaal = eenmalig.reduce((s, e) => s + e.bedrag, 0);

  // ── Bruto totaal ─────────────────────────────────────────────────────────
  const brutoBetaling = Math.round(
    (basisLoon + amtZeerintensief + toeslagBalansvif + ortTotaalBedrag +
     extraUrenBedrag + toeslagVakatieUren + reiskosten + eenmaligTotaal) * 100
  ) / 100;

  // ── Pensioen (PFZW) ──────────────────────────────────────────────────────
  // Pensioengrondslag = basisloon + amt + extra uren (reiskosten/eenmalig tellen niet mee)
  const pensioengrondslag = basisLoon + amtZeerintensief + toeslagBalansvif + ortTotaalBedrag + extraUrenBedrag;
  const pensioenpremie    = Math.round(pensioengrondslag * SALARIS_CONFIG.PENSIOEN_PCT * 100) / 100;

  // ── Netto prognose ───────────────────────────────────────────────────────
  // Loonheffing is een schatting (jaarsalaris-methode)
  const fiscaalBruto      = brutoBetaling - reiskosten; // reiskosten zijn belastingvrij t/m €0,23/km
  const loonheffingSchat  = _berekenLoonheffingSchatting(fiscaalBruto * 12, jaar) / 12;
  const nettoPrognose     = Math.round((brutoBetaling - pensioenpremie - loonheffingSchat) * 100) / 100;

  return {
    jaar, maand,
    maandLabel:       `${jaar}-${String(maand).padStart(2, '0')}`,
    aantalDiensten:   diensten.length,
    tarieven,

    // Componenten
    basisLoon:          Math.round(basisLoon * 100) / 100,
    amtZeerintensief,
    toeslagBalansvif,
    reiskosten,
    werkdagenMaand,

    // ORT
    ortUren, ortTotalen, ortTotaalBedrag,

    // Extra
    extraUren, extraUrenBedrag, toeslagVakatieUren,

    // Eenmalig
    eenmalig, eenmaligTotaal,

    // Totalen
    brutoBetaling,
    pensioengrondslag:   Math.round(pensioengrondslag * 100) / 100,
    pensioenpremie,
    loonheffingSchat:    Math.round(loonheffingSchat * 100) / 100,
    nettoPrognose,
  };
}

// ============================================================================
// CLASSIFICATIE & HELPERS
// ============================================================================

/**
 * Bepaal ORT-categorie en uren van een dienst op basis van
 * start-tijd, eind-tijd en dag-van-de-week.
 */
function _classifyDienst(dienst) {
  const { startTijd, eindTijd, dag, shiftType, duur, isExtraUren } = dienst;
  const uren = parseFloat(duur) || _berekenDuur(startTijd, eindTijd);

  if (uren <= 0) return { uren: 0, ortCategorie: null, isExtra: false };

  // Dag-gebaseerde ORT heeft prioriteit
  const dagNorm = (dag || '').toLowerCase();
  if (dagNorm === 'zondag')   return { uren, ortCategorie: 'ZONDAG',   isExtra: !!isExtraUren };
  if (dagNorm === 'zaterdag') return { uren, ortCategorie: 'ZATERDAG', isExtra: !!isExtraUren };

  // Shift-type gebaseerde ORT
  const typeNorm = (shiftType || '').toLowerCase();
  if (typeNorm === 'vroeg') return { uren, ortCategorie: 'VROEG', isExtra: !!isExtraUren };
  if (typeNorm === 'nacht') return { uren, ortCategorie: 'NACHT', isExtra: !!isExtraUren };

  // Tijdsgebaseerde ORT: avond = na 20:00
  const startH = _parseUur(startTijd);
  if (startH >= 20 || startH < 6) return { uren, ortCategorie: 'NACHT', isExtra: !!isExtraUren };
  if (startH >= 18)               return { uren, ortCategorie: 'AVOND', isExtra: !!isExtraUren };
  if (startH < 9)                 return { uren, ortCategorie: 'VROEG', isExtra: !!isExtraUren };

  // Reguliere dagdienst: geen ORT
  return { uren, ortCategorie: null, isExtra: !!isExtraUren };
}

function _parseUur(tijdStr) {
  if (!tijdStr) return 9;
  const s = String(tijdStr).trim();
  if (/^\d{1,2}:\d{2}/.test(s)) return parseInt(s.split(':')[0]);
  return 9;
}

function _berekenDuur(start, eind) {
  const sH = _parseUur(start);
  const eH = _parseUur(eind);
  if (eH > sH) return eH - sH;
  if (eH < sH) return (24 - sH) + eH; // nachtdienst over middernacht
  return 0;
}

function _telWerkdagen(jaar, maand) {
  const dagen = new Date(jaar, maand, 0).getDate(); // Aantal dagen in maand
  let werkdagen = 0;
  for (let d = 1; d <= dagen; d++) {
    const dow = new Date(jaar, maand - 1, d).getDay();
    if (dow > 0 && dow < 6) werkdagen++; // Ma t/m Vr
  }
  return werkdagen;
}

/**
 * Zoek het geldende tarief op voor een peilDatum via de TARIEF_TABEL.
 * De tabel is gesorteerd oplopend op datum — we nemen de laatste die <= peilDatum is.
 */
function _getTarief(peilDatum) {
  const tabel  = SALARIS_CONFIG.TARIEF_TABEL;
  let   actief = tabel[0];
  for (const entry of tabel) {
    const vanaf = new Date(entry.vanaf);
    if (peilDatum >= vanaf) actief = entry;
  }
  return actief;
}

/**
 * Schatting loonheffing op basis van jaarloon.
 * BELANGRIJK: dit is een indicatieve schatting. De werkgever past de exacte
 * heffingstabel toe inclusief heffingskortingen en tijdvakfactor.
 */
function _berekenLoonheffingSchatting(jaarLoon, jaar) {
  const tabel = SALARIS_CONFIG.LOONHEFFING_TABEL_2026; // Gebruik 2026-tabel als meest recent
  let heffing = 0;
  let vorig   = 0;

  for (const schijf of tabel) {
    if (jaarLoon <= schijf.tot) {
      heffing += (jaarLoon - vorig) * schijf.tarief;
      break;
    }
    heffing += (schijf.tot - vorig) * schijf.tarief;
    vorig    = schijf.tot;
  }

  // Trek jaarlijkse algemene heffingskorting af (indicatief)
  heffing = Math.max(0, heffing - SALARIS_CONFIG.LOONHEFFINGSKORTING_2026);
  return Math.round(heffing * 100) / 100;
}

// ============================================================================
// DATA INLEZEN UIT DIENSTENDATA SHEET
// ============================================================================

function _leesAlleDiensten(ss) {
  const sheet = ss.getSheetByName(SALARIS_CONFIG.DIENSTEN_SHEET);
  if (!sheet || sheet.getLastRow() < 2) {
    throw new Error(`Sheet '${SALARIS_CONFIG.DIENSTEN_SHEET}' niet gevonden of leeg.`);
  }

  const data    = sheet.getDataRange().getValues();
  const headers = data[0];
  const hi      = {};
  headers.forEach((h, i) => hi[String(h).trim()] = i);

  const vereist = ['Start Datum', 'Start Tijd', 'Eind Tijd', 'Dag', 'Duur (uur)', 'Status', 'Shift Type'];
  for (const v of vereist) {
    if (hi[v] === undefined) throw new Error(`Kolom '${v}' niet gevonden in DienstenData headers.`);
  }

  const diensten = [];
  data.slice(1).forEach((row, idx) => {
    const status = String(row[hi['Status']] || '').trim();
    if (status === 'VERWIJDERD') return;

    const startDatum = row[hi['Start Datum']];
    if (!startDatum) return;

    // GAS geeft datumcellen als Date-objecten terug
    const datum = startDatum instanceof Date
      ? startDatum
      : new Date(String(startDatum).trim());

    if (isNaN(datum.getTime())) return;

    diensten.push({
      datum,
      startTijd:  String(row[hi['Start Tijd']]  || ''),
      eindTijd:   String(row[hi['Eind Tijd']]   || ''),
      dag:        String(row[hi['Dag']]          || ''),
      duur:       row[hi['Duur (uur)']] || 0,
      shiftType:  String(row[hi['Shift Type']]  || 'Dienst'),
      status,
      locatie:    String(row[hi['Locatie']]      || ''),
      // Extra uren = diensten buiten standaard contracturen
      // Indicatie: als er meer dan 1 dienst per dag is, zijn de extra de 2e+
      isExtraUren: false, // Wordt later bepaald via groepering
    });
  });

  return diensten;
}

function _groeperPerMaand(diensten) {
  const map = {};
  diensten.forEach(d => {
    const key = `${d.datum.getFullYear()}-${String(d.datum.getMonth() + 1).padStart(2, '0')}`;
    if (!map[key]) map[key] = [];
    map[key].push(d);
  });
  return map;
}

// ============================================================================
// SHEET OPBOUW & OPMAAK
// ============================================================================

function _initSalarisSheet(ss) {
  let sheet = ss.getSheetByName(SALARIS_CONFIG.SHEET_NAAM);
  if (sheet) {
    sheet.clearContents();
    sheet.clearFormats();
  } else {
    sheet = ss.insertSheet(SALARIS_CONFIG.SHEET_NAAM);
  }
  return sheet;
}

function _schrijfResultaten(sheet, resultaten) {
  const C = SALARIS_CONFIG.KLEUREN;

  // ── TITEL BLOK ───────────────────────────────────────────────────────────
  sheet.getRange('A1:N1').merge()
    .setValue(`💰 Salarisoverzicht ${SALARIS_CONFIG.WERKNEMER} — ${SALARIS_CONFIG.WERKGEVER}`)
    .setBackground(C.HEADER_BG).setFontColor(C.HEADER_FG)
    .setFontSize(14).setFontWeight('bold').setHorizontalAlignment('center');

  sheet.getRange('A2:N2').merge()
    .setValue(`Pers.Nr. ${SALARIS_CONFIG.PERSONEELSNR} | Contract ${(SALARIS_CONFIG.DEELTIJDFACTOR * 100).toFixed(3)}% | Gegenereerd: ${_fmtDate(new Date())}`)
    .setBackground('#e8f0fe').setFontColor('#1a3a5c')
    .setHorizontalAlignment('center');

  // ── KOLOM HEADERS ────────────────────────────────────────────────────────
  const kolommen = [
    'Periode', 'Diensten', 'Uurloon', 'Basis Bruto',
    'Amt Zeerint.', 'Toeslag Balans.', 'ORT Totaal', 'Extra Uren',
    'Toeslag Vak.', 'Reiskosten', 'Eenmalig', '▶ Bruto Betaling',
    'Pensioen PFZW', '≈ Netto Prognose'
  ];

  const headerRow = 4;
  const headerRange = sheet.getRange(headerRow, 1, 1, kolommen.length);
  headerRange.setValues([kolommen])
    .setBackground(C.HEADER_BG).setFontColor(C.HEADER_FG)
    .setFontWeight('bold').setHorizontalAlignment('center')
    .setWrap(true);

  sheet.setRowHeight(headerRow, 45);
  sheet.setFrozenRows(headerRow);
  // setFrozenColumns verwijderd — conflicteert met samengevoegde titelcellen (A1:N1)

  // ── DATA RIJEN ───────────────────────────────────────────────────────────
  let rij   = headerRow + 1;
  let huidigJaar = null;
  const jaarTotalen = {};

  resultaten.forEach((r, idx) => {
    // Jaar-separator rij
    if (r.jaar !== huidigJaar) {
      if (huidigJaar !== null) {
        // Schrijf jaar-subtotaal
        rij = _schrijfJaarSubtotaal(sheet, rij, huidigJaar, jaarTotalen[huidigJaar], kolommen.length);
      }
      sheet.getRange(rij, 1, 1, kolommen.length).merge()
        .setValue(`📅 ${r.jaar}`)
        .setBackground(C.SECTIE_BG).setFontColor(C.SECTIE_FG)
        .setFontWeight('bold').setFontSize(12);
      rij++;
      huidigJaar = r.jaar;
      jaarTotalen[r.jaar] = { bruto: 0, pensioen: 0, eenmalig: 0, netto: 0 };
    }

    // Speciale rijen voor eenmalige uitkeringen
    const eenmaligLabel = r.eenmalig.map(e => e.label).join(' + ') || '';

    const rij_data = [
      r.maandLabel,
      r.aantalDiensten,
      `€ ${r.tarieven.uurloonORT.toFixed(2)}`,
      r.basisLoon,
      r.amtZeerintensief,
      r.toeslagBalansvif,
      r.ortTotaalBedrag,
      r.extraUrenBedrag,
      r.toeslagVakatieUren,
      r.reiskosten,
      r.eenmaligTotaal,
      r.brutoBetaling,
      -r.pensioenpremie,
      r.nettoPrognose,
    ];

    const dataRange = sheet.getRange(rij, 1, 1, kolommen.length);
    dataRange.setValues([rij_data]);

    // Kleur: eenmalig maand anders
    const bgKleur = r.eenmaligTotaal > 0 ? C.EENMALIG : (idx % 2 === 0 ? '#FFFFFF' : '#F8F9FA');
    dataRange.setBackground(bgKleur);

    // Getal opmaak voor geldbedragen (kolommen 4 t/m 14)
    sheet.getRange(rij, 4, 1, 11).setNumberFormat('€ #,##0.00');

    // Bruto kolom vet
    sheet.getRange(rij, 12).setFontWeight('bold').setBackground(C.SUBTOTAAL);
    // Pensioen rood
    sheet.getRange(rij, 13).setFontColor('#c5221f');
    // Netto groen + vet
    sheet.getRange(rij, 14).setFontWeight('bold').setBackground(C.POSITIEF);

    // ORT detail als tooltips via note
    const ortNote = Object.entries(r.ortTotalen)
      .filter(([, v]) => v > 0)
      .map(([k, v]) => `${SALARIS_CONFIG.ORT[k].label}: €${v.toFixed(2)} (${r.ortUren[k].toFixed(1)} uur)`)
      .join('\n');
    if (ortNote) sheet.getRange(rij, 7).setNote(ortNote);

    // Eenmalig detail als note
    if (r.eenmalig.length > 0) {
      const eenmaligNote = r.eenmalig.map(e => `${e.label}: €${e.bedrag.toFixed(2)}`).join('\n');
      sheet.getRange(rij, 11).setNote(eenmaligNote);
    }

    // Jaaraccumulatie
    jaarTotalen[r.jaar].bruto    += r.brutoBetaling;
    jaarTotalen[r.jaar].pensioen += r.pensioenpremie;
    jaarTotalen[r.jaar].eenmalig += r.eenmaligTotaal;
    jaarTotalen[r.jaar].netto    += r.nettoPrognose;

    rij++;
  });

  // Laatste jaar-subtotaal
  if (huidigJaar) {
    rij = _schrijfJaarSubtotaal(sheet, rij, huidigJaar, jaarTotalen[huidigJaar], kolommen.length);
  }

  // ── LEGENDA ──────────────────────────────────────────────────────────────
  rij++;
  sheet.getRange(rij, 1, 1, kolommen.length).merge()
    .setValue('ℹ️  Netto prognose is een schatting. Loonheffing is berekend op basis van de 2026-tabel (schijf 1). De exacte verrekening door \'s Heeren Loo kan afwijken door tijdvakfactor en persoonlijke heffingskortingen. | ORT-detail: hover over ORT-cel.')
    .setBackground('#fff3cd').setFontColor('#664d03').setFontSize(9)
    .setWrap(true);
  sheet.setRowHeight(rij, 35);

  // ── KOLOMBREEDTE ─────────────────────────────────────────────────────────
  const breedtes = [85, 65, 70, 90, 90, 100, 85, 85, 85, 90, 90, 110, 100, 115];
  breedtes.forEach((b, i) => sheet.setColumnWidth(i + 1, b));

  Logger.log(`✅ Sheet '${SALARIS_CONFIG.SHEET_NAAM}' geschreven, ${rij} rijen`);
}

function _schrijfJaarSubtotaal(sheet, rij, jaar, totalen, aantalKolommen) {
  const C     = SALARIS_CONFIG.KLEUREN;
  const data  = new Array(aantalKolommen).fill('');
  data[0]     = `📊 ${jaar} TOTAAL`;
  data[11]    = totalen.bruto;
  data[12]    = -totalen.pensioen;
  data[13]    = totalen.netto;

  sheet.getRange(rij, 1, 1, aantalKolommen)
    .setValues([data])
    .setBackground(C.TOTAAL_BG)
    .setFontColor(C.TOTAAL_FG)
    .setFontWeight('bold');

  sheet.getRange(rij, 12, 1, 3).setNumberFormat('€ #,##0.00');
  sheet.getRange(rij, 13).setFontColor('#ffcdd2');
  return rij + 1;
}

function _fmtDate(d) {
  return Utilities.formatDate(d, Session.getScriptTimeZone(), 'yyyy-MM-dd HH:mm');
}

// ============================================================================
// MENU UITBREIDING
// ============================================================================

/**
 * Voeg salaris menu toe. Roep dit aan vanuit onOpen() in main.gs,
 * OF registreer als standalone onOpen trigger in het GAS project.
 */
function addSalarisMenu() {
  SpreadsheetApp.getUi()
    .createMenu('💰 Salaris Tools')
    .addItem('📊 Bouw Salaris Dashboard', 'buildSalarisSheet')
    .addItem('📈 Prognose Huidige Maand', 'berekenPrognose')
    .addToUi();
}
