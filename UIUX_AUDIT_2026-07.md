# UI/UX & Gebruiksvriendelijkheidsaudit — JeffriesHomeapp + JeffriesBackend

**Datum:** 2026-07-01
**Status (2026-07-01, later dezelfde dag):** ✅ **Volledige lijst gefixt** — alle 11 highs, alle mediums en de lows zijn doorgevoerd in beide werkbomen (7 parallelle fix-agents + handmatige eindronde). Frontend: `tsc`, ESLint (0 errors) en `next build` schoon. Backend: `go build`, `go vet` en alle tests groen. Bewust resterend: notes-lijstvirtualisatie (gemitigeerd met zoek-debounce + animatie-cap >60 items), sync-endpoints op WriteTimeout-route i.p.v. 202-async, K1 shadow-dedup blijft een (aangescherpte) heuristiek zolang er geen backend-eventId-link is. De Settings-backupknop bleek in een eerdere sessie al aan een echt endpoint gekoppeld en is behouden. Wijzigingen zijn nog niet gecommit.
**Scope:** Volledige frontend (`JeffriesHomeapp`: shell/navigatie, agenda, rooster, focus, habits, notities, lampen, automations, finance, LaventeCare, settings, auth) + backend-bijdrage aan UX (`JeffriesBackend`: foutmeldingen, statuscodes, timeouts, paginering, Telegram-bot).
**Methode:** 8 parallelle audit-agents (6 frontend-domeinen, 1 cross-cutting patronen-sweep, 1 backend), alle bevindingen op basis van gelezen code met file:line-referenties. De zwaarste single-source-bevindingen zijn handmatig geverifieerd; één bevinding bleek daarbij een false positive en is geschrapt (zie onderaan).

---

## Eindoordeel

De app is **beter dan de gemiddelde personal dashboard**: één consistent toastsysteem, nul `alert()`/`window.confirm()`, vrijwel overal Nederlandse copy, ConfirmDialogs op alle destructieve acties, doordachte mobile-navigatie met safe-areas, en een notes-mutatielaag met optimistic updates + rollback + optimistic-concurrency die productiekwaliteit heeft. De eerdere UX-audits (notes, agenda-redesign) blijken grotendeels écht gefixt, niet cosmetisch.

De problemen clusteren in vier thema's:

1. **Dode/nep-UI op de Settings-pagina** — de backup-knop is een stub met een oneindige spinner, het audit-log-paneel is hardcoded leeg.
2. **"Failed ≠ empty" wordt maar op 2 van ~10 pagina's gehandhaafd** — bij een backend-storing tonen home, agenda, rooster, lampen, focus en LaventeCare vrolijk "geen data"-states alsof je leven leeg is.
3. **Stille afkapping** — LaventeCare maakt oudere facturen onbereikbaar (top-5), factureerbare uren onselecteerbaar (top-8), en de CSV-export exporteert stilletjes alleen de geladen 50 rijen.
4. **Backend lekt rauwe interne fouten** — 144 plekken sturen pgx/SQL-fouttekst rechtstreeks naar de Nederlandse UI, en de server-WriteTimeout (30s) is korter dan de eigen budgetten van sync/AI-endpoints (55–90s).

---

## HIGH — direct oppakken (11)

### Frontend

**FH1. Spatiebalk toggle't álle lampen, ook op knoppen en in modals** *(geverifieerd)*
`hooks/useGlobalShortcuts.ts:21-29` (gebruikt op `/lampen`). Alleen INPUT/TEXTAREA/SELECT worden uitgesloten — BUTTON niet, en er is geen dialog-check. Tab naar een willekeurige knop (filterchip, scene, de close-knop van het detailpaneel dat auto-focus krijgt) + spatie = alle lampen in huis flippen, en de gefocuste knop wordt nooit geactiveerd (`preventDefault`). Vuurt ook in de open BottomSheet.
→ Ook bailen op `tag === "BUTTON"`, `isContentEditable`, en wanneer een `[role="dialog"]` gemount is; of scopen op `e.target === document.body`.

**FH2. Settings "JSON export" is een stub met permanente spinner** *(door 2 agents onafhankelijk gevonden)*
`app/settings/page.tsx:130` — `const backupData = undefined as any; // TODO`. Klik → `backupRequested=true`, knop disabled met Loader2, download-effect (`:169-181`) vereist `backupData` en draait nooit → spinner tot reload, geen download, geen fout. En dit is nota bene de enige knop die zichzelf als jouw backup-pad presenteert.
→ Koppelen aan een echt export-endpoint of paneel verwijderen; minimaal een "nog niet beschikbaar"-toast.

**FH3. Geen `error.tsx`, `not-found.tsx`, `global-error.tsx` of `loading.tsx` in heel `app/`**
Een verkeerde/oude URL geeft Next's Engelse licht-thema-404 zonder weg terug; server-renderfouten vallen door naar het generieke Next-scherm (de client-ErrorBoundary vangt alleen client-fouten).
→ Themed Nederlandse `not-found.tsx` + `error.tsx`/`global-error.tsx` + minimale `loading.tsx` toevoegen.

**FH4. Sessie-verloop midden in gebruik → cryptische JSON-parse-fouten i.p.v. login-prompt**
`proxy.ts:11-26` redirect óók `/api/backend/...`-fetches met 307 naar `/sign-in`; `lib/api.ts:51-66` volgt de redirect, krijgt sign-in-HTML met status 200 en gooit `SyntaxError: Unexpected token '<'`. Elke panel faalt met onvertaalde parse-fouten.
→ In `proxy.ts` voor `/api`-paden `Response.json({detail:"Niet ingelogd."},{status:401})` teruggeven; in `apiFetchWithStatus` op 401 doorsturen naar `/sign-in?redirect_url=…`.

**FH5. LaventeCare-cockpit bij API-storing = lege CRM**
`hooks/useLaventeCare.ts:47-51` levert `cockpitError`, maar `app/laventecare/page.tsx` gebruikt hem nergens. Backend down → "Nog geen klantenbasis", alle tellers 0, en de gebruiker kan "Docs initialiseren" klikken of klanten opnieuw gaan invoeren.
→ Bij `cockpitError` (en billing/mailbox-errors) een foutbanner met retry i.p.v. de normale views.

**FH6. Factuurformulier: maar 8 open urenregels selecteerbaar, stilzwijgend**
`components/laventecare/LaventeCareBillingView.tsx:649` — `uninvoicedEntries.slice(0, 8)`, terwijl de teller "12 van 15 regels" zegt. Regels 9+ zijn vanuit de UI onfactureerbaar zonder enige hint.
→ Cap weghalen of "Toon alle X regels" + select-all.

**FH7. Alleen de 5 recentste offertes/facturen zijn benaderbaar**
`LaventeCareBillingView.tsx:215-216` — alle factuuracties (Document, UBL, Betaalverzoek, Check betaling, Betaald, Mail) bestaan alléén op die 5 rijen. Vanaf factuur 6 kan een oudere openstaande factuur nooit meer op betaald gezet of opnieuw gemaild worden.
→ Volledige factuur-/offertelijst toevoegen — dit is het kernoppervlak van de billing.

**FH8. CSV-export exporteert alleen de geladen pagina's** *(geverifieerd)*
`app/finance/page.tsx:476` geeft de gepagineerde in-memory lijst (`PAGE_SIZE = 50`, `hooks/useTransactions.ts:31`) door aan `exportCsv`, terwijl de tooltip "Exporteer gefilterde transacties" belooft. Een jaarfilter met 900 hits levert 50 rijen — plausibel ogende dataverlies voor boekhouding/belasting.
→ Alle gefilterde rijen server-side ophalen voor export, of minstens hernoemen naar "Exporteer N zichtbare transacties" + waarschuwen bij `transactions.length < totalCount`.

**FH9. Notitie-editor: draft-verlies bij reload/Android-terugknop**
`components/notes/NoteEditor.tsx:755-766` — de dirty-confirm dekt alleen in-app sluiten (Esc, X, backdrop). Geen `beforeunload`, geen history/popstate-integratie: tab-reload, PWA-kill of de Android-terugknop vernietigt een lange ongesavede notitie zonder waarschuwing.
→ `beforeunload` bij `isDirty`, history-entry pushen bij openen zodat "terug" `handleCloseAttempt` triggert, en/of draft-snapshot in localStorage per note-id.

### Backend

**BH1. 144 plekken lekken rauwe interne fouten naar de client**
`Error(w, 500, err.Error())` in 15 handler-files (o.a. `handler/habit.go`, `note.go`, `laventecare.go` 68×, `transaction.go`). De Nederlandse UI kan toasts tonen als `ERROR: value too long for type character varying(50) (SQLSTATE 22001)`.
→ Eén `InternalError(w, err)`-helper in `respond.go`: logt server-side (request-ID is er al), retourneert vast `{"detail":"Er ging iets mis aan de serverkant. Probeer het opnieuw."}`. Mechanische find/replace. De Telegram-laag heeft dit patroon al (`classifyUserFacingError`) — porteer de filosofie.

**BH2. WriteTimeout (30s) < eigen handler-budgetten (55–90s) → dode verbinding na lange spinner**
`server/server.go:92` vs `sync.go:50` (60s), `sync.go:337` (60s), `sync.go:383` (90s), `settings.go:346` (55s), Grok-client 60s zonder request-cap. De server kapt de verbinding op 30s terwijl de sync server-side gewoon doorloopt en slaagt → gebruiker ziet netwerkfout, retryt, dubbel werk.
→ Sync-endpoints `202 Accepted` + job-handle laten teruggeven (`/sync/status` bestaat al), óf WriteTimeout naar 120s; AI-calls cappen op ~25s via `context.WithTimeout(r.Context(), …)`.

---

## MEDIUM — hoogste impact eerst

### Dataverlies- & feedbackrisico's
| # | Locatie | Probleem |
|---|---------|----------|
| M1 | `app/automations/page.tsx:68-76` + `hooks/useAutomations.ts:100-128` | **Dienst-wekker "Opslaan" slikt deelfouten in**: throw uit `addDienstWekkerPack` wordt door `try/finally` zonder catch een unhandled rejection — geen toast, oud wekkerpakket al verwijderd. Half-geïnstalleerd alarm = "niet wakker geworden"-risico. Catch + error-toast; overweeg create-before-delete. |
| M2 | `app/automations/page.tsx:27-42` + `AutomationForm.tsx:74-82` | Mislukte automation-save gooit alle formulierinput weg (modal sluit vóór de await). Form open houden tot resolve; alleen sluiten bij succes. |
| M3 | `components/ui/Modal.tsx:66-72` + alle LaventeCare/CreateEvent/Habit-modals | Escape/backdrop sluit direct zonder dirty-check; company-form reset zelfs on-close (typwerk weg met één misklik), lead/project bewaren juist stale drafts. Eén dirty-confirm-gedrag voor alle modals. |
| M4 | `hooks/useHabits.ts:378-392` + `:413-425` | **Habits: globale single-flight-lock dropt taps stil** (habit B afvinken terwijl A in flight is doet niets) én check-off is niet optimistic (wacht op POST + 5 query-invalidaties). Per-habit lock + optimistic flip met rollback — één PR die het dagelijkse afvinkgevoel transformeert. |
| M5 | `components/habits/HabitCard.tsx:159-176` + `hooks/useHabits.ts:443-455` | **Incidenten**: (a) kunnen niet ongedaan gemaakt worden — één misklik doodt een maandenlange streak permanent; (b) negeren de geselecteerde datum — incident loggen terwijl je gisteren bekijkt, schrijft stil naar vandaag. Undo-actie + `datum` meesturen (of knop disablen bij `!isToday`). |
| M6 | `app/habits/page.tsx:44-47` | "Vandaag" verstaalt na middernacht (PWA open laten staan → check-offs schrijven naar gisteren). Herbereken op interval of `visibilitychange` (useFocusData doet dit al goed). **Let op: de UTC→Amsterdam `todayStr()`-fix in `hooks/useHabits.ts` staat alleen als uncommitted working-tree-wijziging — committen/deployen.** |
| M7 | `LaventeCareMailboxView.tsx:224-239` + `page.tsx:193` | Composer reset niet na versturen (tweede confirm = dubbele mail) en de invoice-prefill (`mailboxInvoiceId`) wordt nooit gecleard — elke latere Mailbox-visite heeft de oude factuur voorgekoppeld. |

### Fout- & statuscommunicatie
| # | Locatie | Probleem |
|---|---------|----------|
| M8 | `app/page.tsx` (0 error-branches), agenda, rooster, lampen, focus, laventecare | **`ErrorState`-primitive ("failed ≠ empty") bestaat maar wordt op maar 2 van ~10 pagina's gebruikt** (automations, finance). Adoptiegat, geen infrastructuurgat — uitrollen. |
| M9 | `app/focus/page.tsx:491-541` | Focus-kiosk gebruikt `isLoading` nérgens: koude start toont zelfverzekerd "Geen blokkades / Geen habits gepland / Geen agenda-items" — op een wandkiosk onherscheidbaar van een écht lege dag. |
| M10 | `app/automations/page.tsx:150-160` | Permanent groene "Automation Engine actief · Draait 24/7"-banner rendert onconditioneel, ook bij `isError` — gebruiker wordt actief gerustgesteld dat wekkers afgaan terwijl de engine down kan zijn. `lastCheck` is dead code (hardcoded `null`). |
| M11 | `hooks/useDevices.ts:9-20` | Lampen-status wordt nooit gepolld (geen `refetchInterval`, geen websocket) terwijl de sidebar "Live status … actueel" zegt. Automation/Telegram/fysieke schakeling blijft onzichtbaar. Settings pollt hetzelfde al elke 6–10s. |
| M12 | `handler/scene.go:249-265` (backend) | Scene-activatie geeft altijd 204, ook als élke lamp faalt (fouten alleen geslogd). Tel successen/fouten, retourneer `{activated, failed}`, 502 bij totaalfalen — de Telegram-variant doet dit al goed. |
| M13 | `handler/pending.go:70,84`, `habit.go:143-213`, `personal_event.go:172` (backend) | User-fixable states als 500: verlopen pending-actie → 500 i.p.v. 409; `pgx.ErrNoRows` bij update van verwijderde habit → 500 "no rows in result set" i.p.v. 404. `laventecare.go` doet het op 40+ plekken wél goed — patroon kopiëren. |
| M14 | backend breed | Taal-split: LaventeCare/smart-home spreken Nederlands, notes/habits/sync Engels ("note modified elsewhere", "userId required"). Standaardiseer op Nederlands voor alles wat een toast kan bereiken; vooral de notes-409. |
| M15 | `LaventeCareMailboxView.tsx:760` | Mailbox communiceert nergens dat inbound mail wacht op de Azure Mail.Read-grant — inbox lijkt zich te gaan vullen; de echte blokkade zit alleen in een toast ná klikken op Sync. Persistente inline notice tonen. |

### Interactie & flow
| # | Locatie | Probleem |
|---|---------|----------|
| M16 | `app/agenda/page.tsx:689`, `PersonalEventItem.tsx:52-79` | Geen optimistic updates op afspraak create/edit/delete — verwijderde rijen blijven staan tot de refetch, nieuwe afspraken verschijnen niet bij modal-close. |
| M17 | `AgendaCalendar.tsx:355-358, 917-921` | Mobiel Maand→Agenda-tap verliest de getapte dag: geen scroll-anchor, selectie niet gehighlight, en een lege dag bestaat niet in de lijst — tap lijkt niets te doen. |
| M18 | `app/rooster/page.tsx:393-406` | Rooster flasht de volledige "Rooster ophalen / CSV uploaden"-empty-state tijdens elke koude load (`hasScheduleData` is false tijdens laden). `&& !isLoading` + skeleton. |
| M19 | `page.tsx:1326-1344` (laventecare) | Factuurpreview: `window.open` ná de await → popup-blocker slikt het tabblad op trage backend. Venster synchroon openen in de click-handler, daarna vullen. |
| M20 | `LaventeCareBillingView.tsx:803-823, 861-884` | Geldstatus-flips ("Betaald", "Akkoord") zijn one-click, zonder confirm én onomkeerbaar; en een handmatig verstuurde factuur kan nooit "verstuurd" worden (alleen het bunq-pad zet die status). Confirm hergebruiken + handmatige "Verstuurd"-actie. |
| M21 | `LaventeCareBusinessCommandCenter.tsx:166-178` | PDF-dossiercontext: picker gecapt op 5 bedrijven zonder indicatie + auto-select van het éérste bedrijf → haastklik archiveert het document onder de verkeerde klant. Searchable select + default "Geen context". |
| M22 | `components/habits/HabitForm.tsx:180`, `components/lamp/LampDetailPanel.tsx:79` | Enige twee modals zonder focus trap (rollen zelf `role="dialog"` zonder `useFocusTrap`); HabitForm mist ook Escape. |
| M23 | `hooks/usePrivacy.ts:43-46` + `app/settings/page.tsx:301-321` | Privacy: Settings schrijft server-side via react-query, `usePrivacy` leest server-settings één keer buiten react-query om → scope-toggle in Settings doet niets op gemounte pagina's, en een lokale eye-toggle-override **schaduwt de serverwaarde permanent**. Bovendien tegengestelde defaults (Settings `?? true` verborgen, usePrivacy `?? false` zichtbaar). Serverstate in gedeelde query-key + override clearen bij serverwijziging + één default. Ook: `usePrivacy.ts:44` faalt stil open (masking valt bij fetch-fout terug op "zichtbaar"). |
| M24 | `app/finance/page.tsx:276` + `useTransactions.ts:122-131` | Maandfilter overleeft jaarwissel: header/grafieken tonen 2025, transactielijst blijft mei 2026 tonen. Maand/datumfilters clearen bij jaarswitch (zoals `handleIbanTab` al doet). |
| M25 | `CsvUploader.tsx:60-103` + `:31` | CSV-import: "Stoppen" eindigt op "Import geslaagd!" (staat kan zelfs een nieuwe flow clobberen); en de uploader mount een tweede `useTransactions()` die de hele pagina-fetch + zware stats-aggregatie dupliceert bij elk bezoek. |
| M26 | `useTransactions.ts:201-209` | Categorie-wijziging: geen error-feedback (unhandled rejection) en stats/grafieken blijven de oude categorie tonen tot volledige refresh. |
| M27 | `store/note.go:89-93` (backend) | `GET /notes` retourneert álle notities inclusief volledige inhoud, ongelimiteerd — notitiescherm wordt lineair trager. Limit/offset + summary-mode; sluit aan op het open frontend-item lijstvirtualisatie. |
| M28 | Diverse LaventeCare-forms | Verplichte velden niet gemarkeerd, validatie is toast-only (veld niet gehighlight/gefocust); Ontvanger-veld accepteert elke string (`type="email"` ontbreekt). Backend-kant idem: `"invalid JSON"` zonder veldcontext (BM4) — veldnaam uit `json.UnmarshalTypeError` meenemen. |

---

## LOW — verzameld (selectie, per domein)

**Shell/PWA:** Engelse metadata "Homeapp — Smart Home Control" dekt alleen lampen; manifest-iconen alleen `maskable` + geen apple-touch-icon; `color-scheme: dark` nergens gedeclareerd (lichte native controls/scrollbars op `#0a0a0f`); SW-registratie racet het `load`-event; sidebar popt na hydration in (lege 256px-goot); settings-overview heeft geen error-state en een dode "vergrendeld"-branch; niet-owner-signup is een doodlopende 403 zonder uitlegscherm; "Werkgebieden"-tegels missen `/automations` en `/laventecare`; `app/note-modal-preview/` is een lege map (3 agents zagen hem — verwijderen).

**Agenda/rooster:** shift-shadow-dedup is nog een titelheuristiek (echte afspraak kan stil verdwijnen); kalendergrid zonder ARIA-grid/pijltjesnavigatie; agenda-tabs half ARIA (rooster-TabBar is wél textbook — extraheren en hergebruiken); "Wachtrij"-vocabulaire drieregisterig incl. "Render-wachtrij" (hostingnaam) in gebruikerscopy; NextShiftCard-fallback vergelijkt HH:MM-strings over dagnachtgrens; compact-rows op mobiel kondigen edit aan maar verbergen alle acties (mobiel niet kunnen verwijderen); dienst-rijen in mobiele lijst zijn focusbare dode buttons; rooster-history toont 8 van N zonder "meer"; dubbele bottom-padding (~112px dode scroll); NextShiftCard-variant flipt na hydration; sync-timestamp in device-TZ i.p.v. Amsterdam; brutalist-eilandjes met 8-9px sub-AA-labels in RoosterCards/StatsView.

**Focus/habits/notities:** klok kan tot 59s achterlopen (tick niet op minuutgrens); `formatEuroCents(undefined)` → "€0" (onbekend als nul); journal-quick-add stempelt 09:00-deadline → inflateert "Aandacht"; WeekJournal krijgt geen isLoading/isError (storing = 7 lege kolommen); dashboard-checklist flipt kwantitatieve habits met één tap zonder stepper; heatmap-details title-only (onzichtbaar op touch); QuickNote negeert de notes-privacy-scope op het home-dashboard; dubbele fout-toasts (page + hook); "Zoeken uit in privacymodus" → "Zoeken staat uit…"; geen retry-knop bij notes-fout.

**Lampen/automations:** per-lamp refresh-knop 11px zonder aria-label en zonder catch; sliders zonder accessible name; sliderwaarde kan terugspringen tijdens draggen; save-knop disabled zonder uitleg waarom; geen timezone-indicatie op tijdinputs (server-side uitgevoerd); device-fetch-fout toont "Filters resetten"-empty-state (helpt niet, geen retry); WiZ-scenes tonen nooit active state; "Alles aan/uit" vuurt N parallelle commands met N refetches; offline lampkaarten volledig inert; play/pauze-iconen omgekeerd t.o.v. conventie + geen pending-state (dubbeltap = dubbele toggle).

**LaventeCare:** rauwe Engelse enum-waarden op follow-up-kaarten; één bezig item bevriest alle sibling-knoppen (workstreams/operations); funnel highlight de huidige status-knop niet; "0% fit" voor ongescoorde leads; klantnaam/website-velden editable maar genegeerd bij gekozen klantdossier; toegangscredentials create-only (update-mutatie bestaat maar is niet aangesloten); verborgen afkapping overal (signalen 6, acties 5, follow-ups 3, timeline 40, outbox 6) zonder "toon meer"; klantenlijst zonder zoeken/sorteren; Document+UBL delen één busy-flag; PDF-links zonder generatie-feedback; HenkeWonen-copy hardcoded in composer.

**Finance:** grafiek-assen "2026-01" + "€0k"; pie toont top-12, legenda top-7, center-totaal álles (som klopt visueel niet); "Grootste tegenpartij" toont bedrag zonder wie; "categorieen" zonder trema; dubbele "Geldautomaat"-optie; categorie-dropdown `role="menu"` zonder pijltjesnavigatie; grafieken zonder tekstalternatief; "Netto salaris"-metric zonder periode (stale loonstrook onzichtbaar); `€12.50` (EN-decimaal) in SalaryCards/SettingsIntegrations vs "€ 12,50" elders; filter-badge telt een filter zonder chip.

**Backend:** rate-limiter breekt de envelope (`error`-key, Engels, geen Retry-After); te grote upload meldt zich als "invalid JSON" i.p.v. 413; enkele Telegram-dashboards lekken nog 180 tekens rauwe fouttekst; `/schedule` en `/personal-events` ongelimiteerd (groeit met sync-history); 4 parallelle euro-formatters + gedupliceerde datumhelpers + duale fetch-architectuur (react-query vs hand-rolled `useTransactions`/`useEmails`/`usePrivacy`) — driftrisico.

---

## Wat er goed is (behouden zo)

- **Gedeelde primitives zijn uitstekend**: één ToastProvider (a11y-correct, safe-area-aware), één ConfirmDialog met focus trap op elke harde delete, `EmptyState`/`ErrorState` met gedocumenteerde "failed ≠ empty"-regel — het probleem is adoptie, niet infrastructuur.
- **Notes-mutatielaag** (`hooks/useNotes.ts`): optimistic create/update/delete met snapshot-rollback, temp-id-guards, optimistic-concurrency-token, revisiegeschiedenis met restore.
- **Telegram-bot is het sterkste UX-oppervlak van de backend**: 29-commando's-menu, /help, typo-suggesties ("Bedoelde je /x?"), typing-keep-alive, in-place message-edits, `classifyUserFacingError`, 4096-chunk-splitting — het model voor de HTTP-laag.
- **LaventeCare mail-pipeline**: sandboxed preview die exact het te versturen artefact rendert, interne verzendcheck-panel, expliciete confirm met ontvanger/onderwerp/bijlagen — beter dan menig commercieel CRM.
- **Slider-latency in LampControl**: lokale state + debounce (120–200ms), fouten nooit stil (toast + reconverge); SceneBar's optimistic cache-patch met getrouwe command-simulatie.
- **Eerdere audit-remediatie was echt**: 20+ van 27 notes-bevindingen en vrijwel het hele agenda-mobile-redesign verifieerbaar gefixt, incl. verklarende comments; privacy-mask-reactiviteit (binnen één tab) gefixt via `useSyncExternalStore`.
- **Datum/tijd-discipline frontend**: Amsterdam-gepinde "vandaag" (sv-SE + timeZone), maandag-weekstart, nl-NL overal, noon-anchored ISO-math tegen DST-randen; backend-timeouts op externe calls netjes begrensd (WiZ 3s, bunq 20s, Graph 20s).
- **Rooster-TabBar en habits-tabs**: textbook WAI-ARIA-tabs met roving tabindex en pijltjestoetsen.

---

## Geschrapte bevinding (false positive)

De finance-agent rapporteerde de CSV-export als "structureel kapot" omdat het bedrag met decimale komma **ongequoot** zou worden weggeschreven. Verificatie van `components/finance/FinanceUtils.ts:16` toont dat het bedrag wél gequoot wordt (`"${tx.bedrag.toFixed(2).replace(".", ",")}"`) — standaard-parsers verwerken dit correct. Enige restpunt: Nederlandse Excel opent `,`-gescheiden bestanden standaard in één kolom (geldt voor elke komma-CSV); een `sep=;`-hint of `;`-variant zou dat verhelpen, maar dit is low, geen high.

---

## Aanbevolen fix-volgorde

1. **Quick wins met groot effect (uren):** FH1 spatiebalk-guard · FH2 backup-stub → toast of verwijderen (+ audit-log-paneel) · BH1 centrale `InternalError`-helper (één helper, 144 sites mechanisch) · M6-commit (uncommitted `todayStr()`-fix in useHabits) · FH4 proxy-401-fix.
2. **Dataverlies-preventie:** FH9 editor-guards · M1 wekker-catch · M2/M3 modal-dirty-checks · M5 incident-undo+datum.
3. **"Failed ≠ empty" uitrollen:** FH5 LaventeCare-error-banner · M8 ErrorState op alle pagina's · M9 focus-loading · M18 rooster-flash · app-level `error.tsx`/`not-found.tsx` (FH3).
4. **Stille afkapping:** FH6/FH7 billing-lijsten · FH8 CSV-export-scope · M21 contextpicker.
5. **Voelbaarheid:** M4 habits optimistic+per-habit-lock · M16 agenda optimistic · M11 devices-polling · M10 engine-status echt maken · BH2 sync-202/timeout.
6. **Backend-taal & statuscodes:** M13/M14 · scene-resultaten M12 · paginering M27.
