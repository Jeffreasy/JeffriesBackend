# UI/UX & Gebruiksvriendelijkheidsaudit — Ronde 2 (na de fix-pass)

**Datum:** 2026-07-01 (zelfde dag als ronde 1 + fix-pass; alles betreft de uncommitted working trees op `audit-fixes-2026-07`)
**Status (2026-07-02):** ✅ **Volledige R2-lijst gefixt** — alle 14 highs, alle regressies (R1-R13), de tussen-wal-en-schip-items en de mediums/lows zijn doorgevoerd (7 parallelle fix-agents + handmatige eindronde). Frontend: `tsc`, ESLint (0 errors) en `next build` schoon. Backend: `go build`, `go vet` en alle tests groen (incl. nieuwe unit-tests voor weekly/monthly-streaks en whole-word lamp-matching). Nieuwe API's: untoggle (`voltooid:false`), `DELETE /schedule`, urenregel-PATCH/DELETE, `period_voltooid_count`, stats-datumrange, mail-`subject`/`conversation_id`. Bewust resterend: statusvocabulaire-/casing-unificatie (te invasief), cockpit-signals-caching, mail-reply is stored-for-grouping (geen echte Graph-reply), outbox bewaart geen variabelen/bijlagen (restore in opsteller onmogelijk). Correctie op één R2-claim: de `roomsApi`-backslash-melding van een fix-agent bleek vals alarm (paden zijn correct). Wijzigingen nog niet gecommit.
**Scope:** Beide repo's, volledige diepte. 9 parallelle agents: 2 diff-reviewers die specifiek de fix-pass op regressies joegen, 6 domein-herauditors met nieuwe lenzen (offline/PWA, DST/weekgrenzen, businessflows, kiosk-uptime, kleurechtheid, home-dashboard — dat in ronde 1 nooit als feature is geauditeerd), en 1 backend-herauditor. De zwaarste single-source-claims (rooster-wissen, toast-z-index, billing-metric) zijn handmatig geverifieerd.

---

## Eindoordeel ronde 2

**De fix-pass is degelijk gebleken.** Vrijwel alle ronde-1-fixes zijn per stuk geverifieerd als correct en als systeem coherent — inclusief de riskantste lagen: het 401-contract (geen redirect-loops), de optimistic cache-layers (juiste keys, juiste shapes, correcte rollbacks), het incident-API-contract end-to-end, de wekker-rollback en de export-filterpariteit. De diff-reviewers vonden **geen enkele high-severity regressie in de kern van de fixes zelf** — wel een handvol randbugs in nieuw gebouwde mechanieken (history-sentinel, optimistic tokens) en drie plekken waar ronde-1-items **tussen de bestandssets van de fix-agents door zijn gevallen**: het home-dashboard (M8 nooit toegepast), de LampDetailPanel-focus-trap (M22 half af), en de rooster-error-tak (M8 idem).

De verse dieptepass vond daarnaast een nieuwe laag échte problemen die ronde 1 miste — waaronder twee functioneel kapotte features die er waarschijnlijk al lang zijn ("Rooster wissen" heeft vermoedelijk nooit gewerkt; wekelijkse habits zijn structureel kapot) en één venijnige one-liner met grote impact (toasts onder modals).

**Telling ronde 2: 14 highs · ~45 mediums · ~60 lows.** Kwalitatief ander profiel dan ronde 1: minder "vergeten states", meer diepte-correctheid (contracten tussen lagen, edge-dagen, half-uitgerolde patronen).

---

## DEEL 1 — Regressies & bugs uit de fix-pass zelf

### Hoog

**R1. Habits "Heropenen" is een zichtbare leugen geworden** *(diff-review)*
`hooks/useHabits.ts:493-497` + `HabitCard.tsx:202-217` vs backend `handler/habit.go:311-319`. De nieuwe optimistic toggle flipt `voltooid` beide kanten op, maar de backend-Toggle zet hard `Voltooid: true` — de server kan alleen vóltooien, nooit ont-vinken. "Heropenen" laat het vinkje nu zichtbaar verdwijnen en ~0,5s later terugspringen (voorheen: stille no-op). Hangt samen met N3 hieronder (backend-untoggle ontbreekt überhaupt).
→ Backend een echte flip laten doen (expliciet `voltooid` in de body accepteren) — lost R1 én N3 in één keer op.

**R2. Incident-undo kan een bestaande completion permanent vernietigen** *(backend-heraudit)*
`store/habit.go:348-367` (upsert: `ON CONFLICT DO UPDATE SET voltooid = EXCLUDED.voltooid`) + `:396-407` (undo-DELETE wist de héle rij). Incident loggen op een dag met een bestaande afvink overschrijft `voltooid=true → false, xp=0`; de undo verwijdert daarna de rij compleet — de oorspronkelijke completion komt nooit terug, streak alsnog stuk, zonder melding.
→ Incident-upsert moet `voltooid/waarde/xp` behouden (alleen incident-velden zetten); de DELETE moet alleen de incident-vlag terugdraaien.

### Middel

| # | Locatie | Bug |
|---|---------|-----|
| R3 | `hooks/useNotes.ts:340` + `NoteCard.tsx:93` | Optimistic patch fabriceert een client-`gewijzigd`; dat gaat als concurrency-token mee → **snel twee checkboxes aanvinken = onterechte 409** + rollback van een geldige tik. Token uit de PATCH-response in de cache zetten, of `gewijzigd` niet aanraken in onMutate. *(door 2 agents onafhankelijk gevonden)* |
| R4 | `NoteEditor.tsx:846-873` | History-sentinel: (a) in-app wegnavigeren met open editor lekt een verdwaalde history-entry (terug = 2× drukken); (b) close→reopen binnen het ~200ms AnimatePresence-venster: de oude cleanup-`back()` popt de sentinel van de nieuwe editor en sluit hem direct. Compensatie-`back()` vóór unmount doen + mount-grace op de popstate-handler. |
| R5 | `hooks/useHabits.ts:512` vs `:527` | Kwantitatieve minus clamp't optimistisch op 0 maar stuurt de **ongeclampte** waarde → server kan −2 opslaan, refetch toont "-2/8". Clamp ook in het request (en backend). |
| R6 | `app/providers.tsx:24` | Persist-denylist-entry `"/laventecare"` matcht nooit — LaventeCare-keys zijn `["laventecare", …]` zónder slash → klantdossiers/facturen/mailbodies belanden tóch in IndexedDB. `"laventecare"` toevoegen; `"/salary"` is eveneens een no-op. **Privacygevoelig.** |
| R7 | `app/laventecare/page.tsx:1585` e.v. | De nieuwe foutschermen checken niet op gecachte data: één mislukte background-refetch (tab-refocus + netwerk-blip) vervangt een wérkende CRM door "kon niet laden". `&& !cockpit`/`!billing`/`!mailbox` toevoegen (settings doet het al goed). |
| R8 | `LaventeCareMailboxView.tsx:836` | Mail.Read-banner toont ook bij een gewone lege inbox (`\|\| inbox.length === 0`) — blijft de machtiging "pending" noemen nadat die verleend is. Alleen op `inboundBlocked` tonen. *(2 agents)* |
| R9 | `AgendaCalendar.tsx:169-186` | K4-regressie: Enter/Space op een event-balkje in het grid wordt door de gridcell-handler onderschept → selecteert de dag i.p.v. de afspraak te openen. `if (event.target !== event.currentTarget) return;` — one-liner. |
| R10 | `IncidentUndoSnackbar` + `habits/page.tsx:427-428` | 8s-dismiss-timer reset bij elke parent-render (inline `onDismiss` in effect-deps). Memoizen of op incident-identiteit keyen. *(2 agents)* |
| R11 | 5× LaventeCare-modals | "Annuleren"-knoppen roepen `onClose` direct aan en omzeilen de nieuwe dirty-guard die Escape/backdrop/X wél hebben. Door dezelfde guarded close routeren. *(2 agents)* |
| R12 | `LampControl.tsx:41-48` | Anti-snap-back-teller decrement alleen op pointerup op het element zelf; een verloren pointerup (alt-tab mid-drag) blokkeert de server-sync permanent tot remount. Window-level pointerup of reset op visibilitychange. |
| R13 | `useGlobalShortcuts.ts:24-31` | Guard mist `[aria-modal="true"]`/`alertdialog` (FocusModeControl gebruikt al de bredere selector) én links: Tab naar de Instellingen-link + spatie flipt nog steeds alle lampen. Selector verbreden + `tag === "A"` bailen. |

Kleiner (lows uit de diff-review): dubbele vraag in de Modal-discard-overlay; dubbele foutfeedback automation-save (toast + inline); `app/error.tsx` toont rauwe Engelse `error.message` als kop; `justSent`-bevestiging gaat verloren door de mailbox-key-remount; "Toon alle 250 facturen" is vanaf factuur 251 opnieuw stille afkapping; CardsPresence-remount op de 60-kaarten-grens; cross-habit rollback-clobber (transiënt, zelfherstellend).

---

## DEEL 2 — Ronde-1-items die tussen wal en schip vielen

1. **Home-dashboard heeft nog steeds nul error-branches** — `app/page.tsx` zat in géén enkele fix-agent-bestandsset. Backend down = "Geen dienst / Rustig / Geen lampen / Geen data" op de pagina die je 's ochtends als eerste opent. Vier van de vijf hooks exposen niet eens een error. *(= ronde-1 M8, onafgemaakt)*
2. **Rooster idem**: `useSchedule` slikt fouten volledig in; een 500 toont de uitnodigende "Rooster ophalen / CSV uploaden"-empty-state. Op agenda is een dienst-fout eveneens onzichtbaar (alleen de events-fout wordt getoond).
3. **LampDetailPanel mist zijn focus trap** — ronde-1 M22 noemde HabitForm én LampDetailPanel; alleen HabitForm kreeg hem.
4. **Home "Alles aan/uit" gebruikt `sendBatch` niet** (`app/page.tsx:118-124` doet nog `forEach(mutate)` → N refetches + N fout-toasts) en de **Werkgebieden-tegels op home** missen nog steeds /automations en /laventecare (alleen de settings-kopie is gefixt).
5. **De sub-AA-microtekst-opschoning sloeg RoosterCards, DienstItem-full en delen van SalaryCards over** — het desktop-rooster blijft visueel twee producten (brutalistische vierkante eilanden naast rounded-glass).

---

## DEEL 3 — Nieuwe highs (verse dieptepass)

**N1. Toasts renderen ónder open modals** *(geverifieerd)*
`Toast.tsx:66` = `z-[70]`; Modal `z-[100]`, ConfirmDialog `z-[101]`, BottomSheet `z-[81]`, offline-banner `z-[100]`. Juist nu formulieren bij een save-fout open blijven (ronde-1-fix), verschijnt de fout-toast gedimd en geblurd achter de backdrop. Eén regel: toast-container naar `z-[110]`.

**N2. "Rooster wissen" is functioneel kapot — waarschijnlijk altijd al geweest** *(geverifieerd)*
`useSchedule.ts:107-112` POST `rows: []` (met een `as unknown as`-cast die de type-fout verhult); backend `schedule.go:139-142` weigert lege rows hard met 400 "userId en rows verplicht" — elke klik faalt met developer-jargon als toast. Er bestaat geen DELETE-endpoint en import is upsert-only, dus **een verkeerd geïmporteerde dienst is vanuit de UI onverwijderbaar** en blijft meetellen in uren, contractbalans en salarisprognose.

**N3. Habit-check-offs kunnen nooit ont-vinkt worden (backend)**
`handler/habit.go:311-319` hardcodet `Voltooid: true`; er is geen DELETE/untoggle voor gewone logs. Een misklik op de verkeerde habit = permanent onterecht streak/XP. Samen met R1 in één backend-fix op te lossen.

**N4. `useHabits()` bevriest "vandaag" op mount → home-checklist schrijft na middernacht naar gisteren**
`useHabits.ts:304-307` — `datum ?? todayStr()` wordt éénmalig in een useMemo geëvalueerd; DailyChecklist (home), HabitStats en BadgeShowcase gebruiken die default. Exact de bugklasse die op /habits gefixt is (M6), maar in de hook zelf. Rollover in de hook leggen.

**N5. Wekelijkse/maandelijkse habits zijn functioneel kapot**
`store/habit.go:273` — `x_per_week`/`x_per_maand` zijn élke dag "due"; `doel_aantal` wordt in het hele webpad genegeerd. Gevolg: een 3×/week-habit drukt elke dag het dagpercentage, streaks zijn voor weekly habits praktisch onhaalbaar (elke overgeslagen dag reset), en nergens een "2/3 deze week"-indicator. Grootste ontwerp-item van deze ronde (backend + kaart-UI).

**N6. Sneltoets "n" op /notities vernietigt een open editor-draft**
`app/notities/page.tsx:94-115` — bailt alleen op INPUT/TEXTAREA, checkt `editorOpen` niet. Focus op een toolbar-knop + een woord typen dat met "n" begint = editor-remount, in-memory draft weg (localStorage-draft dekt alleen wat ouder is dan de 1s-debounce). Bailen op `editorOpen`/BUTTON/contentEditable.

**N7. Lampen queue-modus (productie): gefaalde commando's zijn tot 5 minuten onzichtbaar nep-succes**
`handler/device.go:427-445` antwoordt 204 vóór uitvoering en patcht de DB-state alvast; een terminaal gefaald command draait die patch nooit terug (`bridge.go:146-152`), en de echte lampstatus reconvergeert pas elke ~5 min (`cloud_bridge.go:21`). Bridge down → "Alles aan" gloeit in de UI, huis blijft donker; de "X van Y lampen reageerde niet"-toast kán in queue-modus nooit vuren. → Bridge-offline-banner op /lampen (data zit al in de settings-overview) + failed command de DB-patch laten terugdraaien.

**N8. Server-statePatch spiegelt de frontend-simulatie niet → kleuren "springen terug"**
`handler/device.go:397-401` nult r/g/b niet bij een color_temp-commando (jsonb-merge) en persisteert `scene_id` nooit — terwijl `lib/deviceCommands.ts` beide wél simuleert. Rood → warm wit schuiven: lamp wordt wit, kaart springt binnen een seconde terug naar rood; WiZ-scene-highlight dooft na één refetch. → statePatch exact `applyCommandToDevice` laten spiegelen + `scene_id` in beide statuspolls meenemen. *(2 agents onafhankelijk)*

**N9. LaventeCare: vervaldatum en "overdue" bestaan nergens in de UI**
Je voert bij aanmaak een vervaldatum in en ziet hem nooit meer terug; geen rood op verlopen facturen, geen aging, geen "waarvan N te laat" op de cockpit. Voor een facturatie-CRM het grootste informatiegeurgat.

**N10. LaventeCare: urenregels zijn onbewerkbaar en onverwijderbaar**
Alleen `POST /time-entries` bestaat; geen PUT/DELETE, geen zelfstandige urenlijst. Een typfout (600 i.p.v. 60 min) staat permanent in "Niet gefactureerd"; status "afgeschreven" bestaat in de filter maar is onbereikbaar.

**N11. Cockpit-metric "Niet gefactureerd" toont altijd "0 open urenregels"** *(geverifieerd)*
`LaventeCareBillingView.tsx:395` gebruikt `uninvoicedEntries` (gefilterd op het factuurformulier, leeg zolang geen klant gekozen) i.p.v. `openUninvoicedEntries`. Eén woord.

**N12. Focus-summary lekt rauwe pgx-fouttekst in zijn 200-payload**
`handler/focus.go:190,256,265` — `errors`-array krijgt `label+": "+err.Error()` en het kiosk-scherm rendert dat. Omzeilt de hele InternalError-filosofie van ronde 1. Zelfde klasse: `personal_event.go:218,229` (`syncError` = rauwe Google-tekst) en twee resterende Telegram-lekken (`telegram_notes.go:798`, `telegram_dashboards.go:298`).

---

## DEEL 4 — Nieuwe mediums (per domein, gecondenseerd)

**Shell/PWA:** provider-swap remount de hele app één frame na first paint (hydration-flikker; persister synchroon maken); "Sync alles" rapporteert altijd succes (`allSettled` nooit geïnspecteerd); "Device discovery"-paneel is een structurele no-op (vergelijkt de devicelijst met zichzelf; fouttekst verwijst naar localhost:8000); 401 tijdens een mutatie-POST gooit formulierinvoer weg (redirect alleen voor GET's houden, mutaties laten bubbelen naar een toast); offline: deny-listed pagina's hangen eeuwig in skeletons + geen offline-fallbackdocument; geen SW-updateflow en ChunkLoadError na deploy is onherstelbaar via de "Opnieuw proberen"-knop (kiosk-relevant); More-sheet zonder focus trap/Escape én verkeerd in de tab-volgorde; geen skip-link.

**Agenda/rooster:** actieknoppen onzichtbaar bij toetsenbordfocus (`group-hover` zonder `focus-within`); weeknummer-berekening niet-ISO → ContractWidget toont rond de jaarwissel de verkeerde week ("2027-00"), en CSV-kolom `Weeknr` wordt letterlijk vertrouwd; "Totaalbalans" telt toekomstige lege weken als min-uren; mobiele agenda-lijst opent op dag 1 van de maand i.p.v. vandaag; create-modal blokkeert tot 20s op de synchrone Google-push terwijl de rij al optimistisch staat (modal direct sluiten); dubbele gelijktijdige sync mogelijk (twee losse busy-vlaggen); persistent wachtrij-foutpaneel bestaat alleen op /agenda; Beheer-tab telt afspraken maar rendert de lijst niet; Google-id-swap breekt notitie↔afspraak-links stil; conflictdetectie telt op /agenda ook VERWIJDERDE diensten mee (andere aantallen dan /rooster).

**Focus/habits/notities:** Escape-loop in de editor-dirty-confirm (geen reentrancy-guard; HabitForm heeft hem wél); Overzicht-tab-toggle op niet-due habits geeft nul feedback (patch is no-op, server kent wel XP toe); heatmap-weekdagrijen kloppen 6/7 van de tijd niet én historie wordt retroactief herschreven bij aanmaken/archiveren; heatmap = 365 tabstops zonder grid-navigatie; kwantitatieve stepper: één server-roundtrip per tap, geen typen, geen hold-repeat; kiosk: geen burn-in-mitigatie/nachtdimming + dubbele summary-poll (2×/30s) + 120s-invalidatie hertrekt het volledige notitiecorpus (de nieuwe `limit`/`fields=summary`-params worden nog nergens gebruikt); tagbeheer bestaat niet als flow; revisiegeschiedenis zonder diff; DailyChecklist/QuickNote bestaan niet op mobiel home.

**LaventeCare:** formulieren resetten óók bij mislukte save (credentials/besluiten/changes/incidenten — rethrow ontbreekt); tabwissel unmount half ingevulde billing/mail-formulieren stil; "Beantwoorden" is geen echte reply (geen conversation-id → threads fragmenteren; "Bewerken in opsteller" herstelt variabelen/bijlagen niet); btw onzichtbaar (invoer excl., lijsten incl., nergens gelabeld; subtotal/vat-velden bestaan in de API maar worden nooit gerenderd); 250 facturen = één platte lijst zonder status-/klantfilter of zoek; offerte kent geen afwijs/verloop-uitgang (`valid_until` wordt ingevoerd en nooit getoond); dossier-timeline mist gesloten projecten/leads én uren/facturen ("wat deed ik vorige maand voor X?" onbeantwoordbaar); dossier-dirty-guard dekt het credential-formulier niet; `isDossierDocumentForCompany` is gevorkt en de kopieën wijken al af; moment↔actie-links zijn dode tekst.

**Finance/salaris/home:** stats & inzichten negeren maand/categorie/zoek-filters terwijl de copy "huidige selectie" claimt (backend aggregeert alleen op iban+jaar); **Salaris-tab omzeilt de complete privacy-masking** (netto/bruto/pensioen/uurloon + title-tooltips ongemaskeerd — het echte gat in de maskingregeling); geen transactiedetail (tegenrekening/saldo-na/referentie zit in de API, onbereikbaar op touch); sectie heet "Abonnementen & Inzichten" maar recurring-detectie bestaat niet; forecast-aannames (16u-contract, 33 km, 32% heffing, cao-tabel) grotendeels onzichtbaar; `deltaOrt` wordt berekend maar nooit gerenderd; home: zelfde vier metrics twee keer onder elkaar, dode maar tappable-ogende cellen, laden ≠ leeg in de tiles (incl. gemaskeerd "••••" voor niet-bestaande data).

**Backend:** `null` vs `[]` in cockpit/mailbox-payloads (frontend-`.map`-risico; lege CRM serialiseert `activeLeads: null`); store-fouten als 400 met rauwe tekst in het mail-verzendpad (verkeerde statusklasse); 13 handlers decoden nog zonder `RespondDecodeError` (te groot bestand = "Ongeldige JSON" i.p.v. 413); ~50 resterende Engelse 400-teksten (vooral "Invalid … ID" in laventecare.go); blanket-404 verhult serverfouten in habit/note-Get (DB-timeout = "niet gevonden"); focus/cockpit doen 25+ sequentiële DB-roundtrips per poll; habit-progress niet transactioneel (refresh-fout na geslaagde delete → 500 → retry → verwarrende 404; dubbeltap-race); incident/toggle accepteert gearchiveerde/gepauzeerde habits.

**Telegram (round 2):** fout-replies na knoptappen zijn keyboard-loze dead-ends met stale knoppen op het origin-bericht; `/status` zegt statisch "actief" zonder echte check; `detectLampCommand` substring-matcht "aan" ("lampen aanpassen naar blauw" → alles aan). Chunking, /help-dekking, callback-data-persistentie en dubbeltap-afhandeling zijn expliciet geverifieerd als goed.

---

## DEEL 5 — Lows (selectie, volledig in agent-details)

Toast-duur vast op 4s ongeacht tekstlengte; `persistOptions` zonder `buster` (stale cache na deploy); sign-in-branding zegt nog "Homeapp · Smart Home Control"; Engelse privacy-scope-keys als knoplabels in settings; CollapsibleSection zonder `aria-expanded`; dev registreert de productie-SW; "Engine elke 15s"-copy vs werkelijk 30s; "Laatste uitvoering" toont tijd-zonder-datum; stale kelvin naast een RGB-gloed; slider-thumb 18px + geen Firefox-styling; modus-tab "Wit/Kleur" stuurt niets; historieknop "Toon alle N" is stilzwijgend op 20 gecapt; sync-status-poll elke 10s zolang /agenda open is (kiosk = 8.640 calls/dag); geen `touch-action: manipulation` op kalender-taps; hele-dag-events sorteren per view anders; loonstrook-file-input reset nooit (zelfde PDF herkiezen doet stil niets); totalen-banner mengt prognose+werkelijk over alle jaren; QuickNote "recent" is pinned-first (niet recent); dagen-picker start op zondag; milestones/badges zijn stil (geen beloningsmoment); heatmap-cellen zonder aria-context; `signedEuro(-0)` → "+€ -0,00"; LIKE-wildcards niet ge-escaped in zoek; pie "Overige categorieën" aggregeert bij exact 8 categorieën één echte categorie weg; rood/groen-bars niet kleurenblind-veilig; CSV-export met privacy aan zonder confirm; UTC-heartbeat naast Amsterdam-tijden in de focus-payload; statusvocabulaire-drieluik (VERWIJDERD / PendingCreate / lowercase-NL) in één kolomfamilie.

---

## DEEL 6 — Expliciet geverifieerd als correct (uit de fix-pass)

- 401-flow: geen redirect-loop mogelijk; backend stuurt zelf nooit 401; PDF-route en SW-caching kloppen; multi-tab logout werkt.
- Optimistic layers habits/notes/personal-events: juiste query-keys, juiste cache-shapes, immutabele pending-Set, `voltooid`-afleiding identiek aan de backend, client-UUID maakt create idempotent.
- Incident-datum-contract end-to-end (POST-bodyveld / DELETE-queryparam, Amsterdam-gepind, 30-dagen-floor gespiegeld) — op de upsert-overwrite (R2) na.
- Wekker create-before-delete: snapshot per id, echte rollback-ids, oude pack intact bij falen.
- Export-`fetchAll`: structurele filterpariteit, loop-proof, nette truncatiemelding.
- Modal-dirty-guard: succesvol opslaan sluit zonder discard-prompt in alle vijf modals.
- TabBar-extractie, ARIA-grid, maand→agenda-anchor, dead-code-verwijdering (nul resterende importers), DST/weekgrens-datumhelpers, focus-`isLoading`-gating, HabitForm-dirty-snapshot, draft-restore-banner-logica: allemaal correct.

---

## Aanbevolen fixvolgorde ronde 2

1. **One-liners met grote impact:** N1 toast-z-index · N11 billing-metric · R9 grid-Enter · R6 denylist-key · R7 `&& !data`-guards · R8 mailbox-banner-conditie.
2. **Dataverlies/-integriteit:** R2 incident-upsert/undo (backend, chirurgisch) · R1+N3 echte untoggle (één backend-fix) · N4 hook-datum-rollover · N6 "n"-guard · R3 concurrency-token · R5 minus-clamp · R11 Annuleren-guard · LaventeCare rethrows.
3. **Kapotte features:** N2 rooster-wissen (backend-endpoint + frontend) · N5 weekly habits (grootste ontwerp-item) · N10 urenregel-edit/delete · discovery-paneel (fixen of verwijderen).
4. **"Staat liegt"-cluster lampen:** N7 queue-zichtbaarheid + N8 statePatch-spiegeling (samen) · R12/R13.
5. **Failed ≠ empty, laatste ronde:** home-dashboard · rooster/useSchedule · N12 focus-errors + resterende rauwe-tekstlekken.
6. **Informatiegeur:** N9 vervaldatum/overdue · salaris-privacy-masking · stats-scope-eerlijkheid · btw-labels.
7. Rest van de mediums per domein, lows als veegronde.
