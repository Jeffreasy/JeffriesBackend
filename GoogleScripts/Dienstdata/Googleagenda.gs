/**
 * ============================================================================
 * GOOGLEAGENDA.GS — Google Calendar Archief voor Gedraaide Diensten
 * ============================================================================
 * Verantwoordelijk voor:
 *   - Aanmaken van een native Google Calendar "Diensten Archief"
 *   - Schrijven van gedraaide diensten als permanente events naar de archief-kalender
 *
 * Waarom een aparte kalender?
 *   De SDB Planning iCal-feed verwijdert events nadat ze zijn afgelopen.
 *   Door ze in een native (niet-geïmporteerde) kalender op te slaan blijven ze
 *   altijd zichtbaar in Google Calendar, ongeacht wat SDB doet.
 *
 * Deduplicatie:
 *   De 'Archief ID' kolom in de DienstenData sheet slaat het Google Calendar
 *   event ID op. Zodra die kolom gevuld is, wordt het event nooit opnieuw aangemaakt.
 * ============================================================================
 */

/**
 * Haal de native archief-kalender op, of maak hem aan als hij niet bestaat.
 * Naam is instelbaar via CONFIG.ARCHIVE_CALENDAR_NAME.
 * @returns {GoogleAppsScript.Calendar.Calendar}
 */
function _getOrCreateArchiveCalendar() {
  const name     = CONFIG.ARCHIVE_CALENDAR_NAME;
  const existing = CalendarApp.getCalendarsByName(name);
  if (existing.length > 0) return existing[0];

  Logger.log(`📅 Archief-kalender '${name}' niet gevonden — aanmaken...`);
  const cal = CalendarApp.createCalendar(name, {
    color:   CalendarApp.Color.TEAL,
    summary: 'Permanente geschiedenis van gedraaide diensten (bijgehouden door het GAS sync-script).'
  });
  Logger.log(`✅ Archief-kalender aangemaakt: ${cal.getId()}`);
  return cal;
}

/**
 * Schrijft een gedraaide dienst als permanent event naar de archief-kalender.
 * Wordt aangeroepen vanuit syncCalendarToSheet in main.gs.
 *
 * @param {GoogleAppsScript.Calendar.CalendarEvent} event - Originele agenda-event
 * @returns {string|null} Calendar event ID (gebruikt als dedup-sleutel in de sheet)
 */
function _archiveShiftToCalendar(event) {
  try {
    const cal   = _getOrCreateArchiveCalendar();
    const title = `✅ ${event.getTitle()}`;
    const desc  = `Gedraaid op ${_formatDate(new Date())}\n` +
                  `Origineel agenda-event: ${event.getId()}\n\n` +
                  (event.getDescription() || '');
    const loc   = event.getLocation() || '';

    let archived;
    if (event.isAllDayEvent()) {
      archived = cal.createAllDayEvent(title, event.getStartTime(), { description: desc, location: loc });
    } else {
      archived = cal.createEvent(title, event.getStartTime(), event.getEndTime(), { description: desc, location: loc });
    }

    return archived.getId();
  } catch (e) {
    Logger.log(`❌ _archiveShiftToCalendar fout voor "${event.getTitle()}": ${e.message}`);
    return null;
  }
}
