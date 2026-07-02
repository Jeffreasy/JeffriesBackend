# UI/UX & Gebruiksvriendelijkheidsaudit — Ronde 3 (na de R2-fix-pass)

**Datum:** 2026-07-02
**Scope:** Beide repo's, diepste pass. 9 agents: 2 diff-reviewers op de R2-fixmechanieken, 6 domein-herauditors met ronde-3-lenzen (volledige user-journeys, first-run/lege-DB, WCAG-contrastberekeningen, perf-by-inspection, cross-page-consistentie, gamification-coherentie, schaalbaarheid, state-truth), 1 backend-auditor. *Procesnotitie:* de backend-hoofdagent sneuvelde op een sessielimiet; zijn twee sub-agents leverden de kernverificaties (habit-fixes, endpoint-hygiëne) wél op, en de domein-agents dekten de backend-kruispaden (engine/bridge, schedule/sync, stats, laventecare-store) mee. Niet gedekt: deploy/restart-gedrag — expliciet open lensgat.
**Verificatie door coördinator:** het "202"-maandlabel is handmatig bevestigd; de geclaimde `\sync\status`-backslash-URL bleek een **false positive** (code gebruikt forward slashes) en is geschrapt; de backend-Engels-sweep is her-gegrept (schoon; resterende hits zijn Swagger-comments).

---

## Eindoordeel

**De R2-fix-pass staat overeind.** De diff-reviewers verifieerden de riskantste mechanieken expliciet als correct: de synchrone persister is SSR-veilig (tegen node_modules geverifieerd), het 401-contract dekt óók de orval-route, de Monday-first-daymapping heeft géén off-by-one, de history-sentinel-statemachine is coherent, "Rooster wissen" werkt end-to-end, en de retry-predicates matchen de echte query-keys. Per domein: 16/19 (habits/notes), 12/14 (finance), 11/12 (lampen), 9/9 (laventecare), 9/9 (shell) fixes als systeem correct.

Ronde 3 vond drie soorten restproblemen:

1. **Randen van de eigen fixes** — trade-offs en half-uitgerolde mechanieken: de immediate-close-modal ruilde de 20s-blokkade in voor een dataverlies-pad; de 409-flow eindigt in een onherstelbare retry-lus; de N5-periode-exclusie bestaat alleen frontend (heatmap/backend-stats straffen weekly habits nog); de cache-buster is inert; de bridge-banner geeft een false positive in direct-modus.
2. **Diepte-bugs die pas op journey-/schaalniveau zichtbaar worden** — de 30-klanten-cockpitcap die de hele picker-laag breekt, de betaalverzoek-doodloper, loonstrook-herupload die liegt, `DELETE /schedule` dat Todoist-taken verweest, pauzeren dat streaks vernietigt.
3. **Coherentie-gaten** — drie pagina's met drie "afspraken bij dienst"-contracten, streaks in "dagen" gelabeld waar periodes geteld worden, stille badges naast dubbele milestone-toasts.

**Telling R3: 15 highs · ~55 mediums · ~65 lows.** Geld-integriteit (excl/incl-labels + rounding-pariteit LaventeCare) is als enige lens volledig schoon bevonden.

---

## DEEL 1 — Highs

### Regressies/randen uit de R2-fix-pass

**R3-H1. Achtergrond-falen na de immediate-close create-modal = totaal invoerverlies** — `CreateEventModal.tsx:341-366`: de modal sluit en reset vóór de upsert; bij een fout rolt de optimistic rij terug en meldt een toast — maar titel/beschrijving/locatie/context zijn definitief weg. De F8-fix loste de 20s-blokkade op maar heropende de dataverlies-klasse die R1-M2 elders dichtte. → Row-snapshot bewaren + "Opnieuw proberen"-actie in de fout-toast die de modal gevuld heropent.

**R3-H2. Notitie-409 is een doodloper** — `app/notities/page.tsx:302-311` + `NoteEditor.tsx:827-831`: `expectedGewijzigd` komt van het moment van openen en ververst na een 409 nooit → elke retry faalt eeuwig; geen "herlaad"-knop, geen andere-versie-weergave, geen overschrijf-optie; plus dubbele feedback (hook-toast + inline). → Bij 409 refetchen en kiezen: "Nieuwste laden (draft blijft)" / "Toch overschrijven".

**R3-H3. 401-mutatiebescherming wordt binnen ≤10s ondermijnd door de eigen GET-polls — en er is geen herinlog-affordance** — `lib/api.ts:89-104`: de mutatie-401 bubbelt netjes naar een toast (formulier blijft staan), maar de eerstvolgende achtergrond-GET (devices 10s, settings 6-10s, focus-refetch) krijgt dezelfde 401 en doet de harde redirect — formulier alsnog weg, seconden na de toast die veiligheid impliceerde. De toast is bovendien tekst-only (Toast-API kent geen actieknop). → Persistente sessie-verlopen-overlay met "Opnieuw inloggen"-knop; GET-redirect onderdrukken zolang een dialog/dirty-form open is.

**R3-H4. ConfirmDialog × NoteEditor: Escape en Tab vechten in drie van de vier confirm-paden** — `NoteEditor.tsx:1087-1136`: de reentrancy-guard dekt alleen de close-confirm; tijdens delete/reset/restore-confirms annuleert Escape de confirm én triggert de editor-close (schone note: hele editor sluit onder de confirm vandaan), en de editor-focus-trap trekt bij elke Tab de focus uit de ConfirmDialog terug. → Eén gedeelde `confirmOpenRef` rond álle openConfirm-aanroepen.

### Nieuwe diepte-bevindingen

**R3-H5. Klant #31 verdwijnt uit de héle CRM-UI** — `store/laventecare.go:4111-4115`: cockpit levert max 30 companies/30 contacts (`ORDER BY updated_at DESC LIMIT 30`) en de frontend voedt álle pickers, de klantenlijst én het zoekveld uitsluitend daaruit. Bij de gestelde schaal (30 klanten) valt de minst-recent-aangeraakte klant overal uit: onvindbaar in dropdowns, dossier onbereikbaar, en het zoekveld zoekt client-side over de al afgekapte lijst. → Aparte companies/contacts-query op het bestaande `GET /laventecare/companies` (heeft al `q`+`limit`), zoekveld server-side.

**R3-H6. Betaalverzoek-flow eindigt in een doodloper** — `page.tsx:1538-1543`: de backend geeft expliciet mee wáár je bevestigt ("via Settings of Telegram met /approve X"); de frontend gooit dat bericht weg en toont alleen een verdwijnende toast met de code — geen link, geen vervolgstap, en LaventeCare zelf heeft geen bevestigings-UI. Nogmaals klikken geeft opnieuw een wegtikkende code. → Minimaal `result.message` tonen; beter: persistente "Bevestiging nodig"-badge op de factuurrij met knop naar /settings of inline bevestigen (`pendingActionId` zit al in de response).

**R3-H7. Loonstrook opnieuw uploaden meldt "bijgewerkt" maar doet niets** — `LoonstrookUploader.tsx:96` (`bijgewerkt: total - inserted`) vs `store/loonstrook.go:98` (`ON CONFLICT DO NOTHING`): een gecorrigeerde PDF her-uploaden wordt stil overgeslagen terwijl de UI "1 bijgewerkt · Import geslaagd!" toont — precies op het reconciliatie-moment. → Backend echte upsert (`DO UPDATE`), of eerlijk labelen "al aanwezig (ongewijzigd)".

**R3-H8. Lege Salaris-tab is een dead end: de uploader is onbereikbaar** — `SalarisView.tsx:120-131`: bij nul records returnt de view vóór de uploader rendert, en de empty-copy noemt alleen het rooster. First-run kan zijn loonstroken — de databron van de tab — nergens kwijt. → Uploader/CTA ook in de lege staat renderen.

**R3-H9. Stats-endpoint: full scan + worst-case O(n²), hertriggerd per zoek-toetsaanslag** — `store/transaction_stats.go:78` laadt álle transacties ongeacht de nieuwe datumrange en filtert in Go; `:423-431` insertion-sort op input die de DB in exact omgekeerde volgorde levert; `useTransactions.ts:103-195` herstart lijst- én stats-fetch per (deferred) toetsaanslag terwijl de zoekterm niet eens in de stats-params zit. → Zoek-debounce + stats alleen bij stats-relevante wijzigingen; datumrange de SQL in; `sort.Slice`.

**R3-H10. `DELETE /schedule` laat Todoist-taken (en mogelijk Google-schaduwen) verweesd achter** — `store/schedule.go:165-183` wist schedule+meta netjes, maar niets sluit de naar Todoist gepushte dienst-taken (`[EID:…]`-markers) of de Google-schaduwevents; op /agenda verschijnen eerder-gededupte schaduwkopieën van gewiste diensten bovendien plots als gewone afspraken (dedup hangt aan de dienstenlijst). → Na de wipe een Todoist/Google-cleanup-pad triggeren; frontend-dedup-noot.

**R3-H11. Bridge-banner: false positive in direct-modus** *(2 agents onafhankelijk)* — `hooks/useDevices.ts:58` checkt `bridge !== null`, maar de backend serialiseert het bridge-object áltijd (`handler/settings.go:165-173`) — in direct-modus staat er permanent "Bridge offline — commando's worden uitgesteld" terwijl alles direct werkt. → Gaten op `queueLightCommands`-flag (zit al in de payload) of `lastSeenAt != null`.

**R3-H12. Queue-modus: engine-getriggerde automations patchen geen device-state — wekker-effecten tot 5 min onzichtbaar** — `engine/engine.go:449-463`: het queue-pad enqueue't alleen; de comment claimt ten onrechte dat de handler dit dekt (engine-acties passeren de HTTP-handler nooit). Wekker zet 05:00 het licht aan → app én kiosk tonen tot 5 min "0 lampen aan". → In de queue-branch dezelfde statePatch optimistic wegschrijven als `handler/device.go:449-455`.

**R3-H13. StatsView toont "202" als maandlabel voor elke lege maand** *(geverifieerd)* — `StatsView.tsx:221` vult lege maanden met `label: key` ("2026-08"); `:66` rendert `label.split(" ")[0].slice(0,3)` → "202". → Filler-labels via de maandformatter.

**R3-H14. Nieuwe afspraak is onzichtbaar in de rooster-tijdlijn zolang hij pending is** — `lib/unified.ts:56` filtert `PendingCreate/PendingUpdate` uit de weektijdlijn; de chips tellen hem wél → drift binnen één scherm, en de gebruiker zoekt zijn zojuist aangemaakte afspraak vergeefs op het hoofdoppervlak. → Pending meenemen + het bestaande pending-badge-vocabulaire tonen.

**R3-H15. /agenda: één mislukte background-refetch vervangt een gevulde agenda door een foutscherm zonder retry** — `app/agenda/page.tsx:548` checkt niet op gecachte data (de R7-klasse die in LaventeCare al gefixt is; rooster doet het wél goed). → `&& geen data` + amber stale-banner + retry-knop.

---

## DEEL 2 — Mediums (per domein, gecondenseerd)

**Shell/PWA:** cache-buster inert (`NEXT_PUBLIC_BUILD_ID` bestaat nergens — 2 agents); sub-AA-microtekst shell-breed via rauwe `slate-500/600`-klassen (berekend 4,15:1 / 2,61:1 — BottomNav-labels, Sidebar-koppen, settings-meta); sign-in/sign-up donkerste copy van de app (1,91:1-footer); **transacties + habits worden onversleuteld gepersist** terwijl het privacy-center ze als maskeerbaar behandelt; whole-cache-persist bij elke poll-tick (main-thread-druk op kiosk, geen maxSize); ConfirmDialog autofocust de bevestigknop óók bij danger; multi-tab IDB is last-writer-wins (documenteren); staleTime 10s ook voor zware zelden-veranderende data.

**Lampen/automations:** broadcast-commando's + engine-lokale poller missen de offline-markering (alleen het bridge-HTTP-pad heeft hem); **geen TTL op pending commando's → replay-storm na lange bridge-uitval** (uren-oude toggles worden 's nachts afgespeeld); state-write in goroutine ná de 204 → read-after-write-flikker (≤10s); per-lamp "Staat verversen" is een placebo (leest alleen de DB-rij); focus-kiosk lichtknoppen doen nog `forEach(sendCommand)` i.p.v. `sendBatch`; scene-succes-toast vuurt vóór de uitkomst; leeg tijdveld is opslaanbaar → automation die stil nooit vuurt; **spatie ingedrukt houden = command-storm** (geen `e.repeat`-guard); `last_seen` wordt gebumpt door optimistic patches én offline-markeringen ("Laatste contact: zojuist" op het faal-moment); wekker-opslaan tijdens lijst-load → dubbele packs; wekker-drafts resetten bij elke background-refetch; header-"Annuleren" van het automation-formulier omzeilt de dirty-guard; /lampen pollt de volledige settings-overview (±23 COUNT's) elke 15s voor 4 bridge-velden; DienstWekker-profieltabs zonder aria-selected.

**Agenda/rooster:** first-run: drie van de vier rooster-tabs zijn dode knoppen (panels achter `hasScheduleData`); /rooster negeert events-fetch-fouten volledig (conflictchip vrolijk groen bij een 500); sync-succes-toast liegt wanneer `scheduleWriteError` gezet is (veld ontbreekt in het frontend-type); "Verwerk nu" ververst rooster/meta niet; modal-dirty-check negeert categorie/symbool/tags/context (touched-vlaggen bestaan al); **NextShiftCard heeft op drie pagina's drie verschillende afspraken-contracten** (alleen-conflicten×alle-dagen / alles×startdag / conflicten×startdag); StatsView-weekkoppen tonen de rauwe ISO-key ("Week 2026-27" — L8 hier overgeslagen); StatsView kan met lege jaarselectie blijven hangen; dienst-status "Bezig" bestaat niet op /agenda; mobiel rooster: historie/datakwaliteit/afsprakenlijst onbereikbaar (aside `hidden md:block`); `useSchedule` retourneert per render verse afgeleiden + 3× gemount (perf); `toggleStatus` is dead code met meta-clobber-footgun.

**Focus/habits/notities:** **pauzeren vernietigt stilletjes de streak** (geen pauzevensters; UI wekt de tegenovergestelde verwachting); pauzeren/archiveren herschrijft de heatmap-historie retroactief (R2-guard dekte alleen aanmaak); **weekly streaks tellen periodes maar heel de UI zegt "dagen"** ("4 dagen 🔥" voor 4 weken; milestone-toast idem fout); stepper-taps verdwijnen bij unmount binnen het 400ms-venster + cross-date-write bij datumnavigatie (kaarten alleen op habit-id gekeyed — 2 agents); N5-exclusie alleen frontend: heatmap/backend-day% straffen weekly habits nog elke dag (zelfde dag, twee percentages); Escape in editor-dropdowns lekt naar de editor-close; "Open in editor" op de capture-kaart neemt de getypte tekst niet mee; DayColumn heeft nog de pre-N2 invalide ARIA-nesting + hover-only acties; editor-focus-restore kapot door effect-churn (per toetsaanslag her-registrerend); tweede checkbox-tik stil gedropt tijdens in-flight actie; editform reset bij concurrente serverwijziging (Telegram-afvink + refocus). *Gamification:* badges volledig stil (alleen glow op het stats-tab) naast dubbele 7/30/100-toasts; criteria van vergrendelde badges onvindbaar; incident-streak-effect nergens uitgelegd; "Week Warrior/Onstopbaar"-registerbreuk.

**LaventeCare:** portal-hero omzeilt de tab-switch-dirty-guard (2 agents; `setActiveView` i.p.v. `handleChangeView`); reply koppelt een verkeerde contactpersoon (contactId niet gereset — mail in dossier aan verkeerde persoon gelogd); "Bewerken in opsteller" op een mislukte reply verliest de thread terwijl subject+conversation_id op het item stáán; dirty-detectie mist variabelen/AI-briefing én de urenregel-editor; Enter in een composer-veld maakt stil een outbox-concept; decimalen in minuten/aantal → generieke faal-toast (int-contract; client-side ronden); schaal-cluster: uren-limit 250 over `entry_date DESC` (oudste ongefactureerde onbereikbaar; tellers spreken elkaar tegen), dossier-timeline max 30 events glóbaal (drukke klant verdringt de rest), operations gecapt op 5, mailbox 50 (per-context-endpoints bestaan al in lib/api maar worden niet gebruikt); maandafsluiting mist totaal-preview van de urenselectie, klantfilter op het urenpaneel, "Herinnering sturen"-actie en een dossier→Commercie-brug; first-run-maturity: zeven rows hardcoded `score: 100` → "84% volwassen" naast zeven "Inrichten"-badges; "Vul Render env"-jargon in gebruikers-copy.

**Finance/salaris/home:** `volgnr`-stringvergelijking kiest bij cijferlengte-rollover de verkeerde "laatste" transactie (exact het saldo dat je tegen je bank-app legt); "oorspronkelijk bedrag" in de uitklaprij en de loonstrook-preview omzeilen de privacy-mask (2 agents); recurring-detectie functioneel dood op de eerste 50 rijen; metrics/pie negeren detailfilters zonder label; tooltips tonen rauw "2026-03" naast een geformatteerde as; home-finance-cel mist de fout-tone (hardcoded green); "versus vorige maand" vergelijkt een onvolledige lopende maand; export-cancel op de laatste pagina levert tóch een CSV; filterpanel-a11y (labels niet gekoppeld, aria-expanded ontbreekt).

**Backend (via sub-agents + kruispaden):** Telegram kent geen untoggle (hardcoded `voltooid: true` — web en bot divergeren); jaargrens-tests voor periode-streaks ontbreken (logica zelf handmatig doorgerekend correct); stats-params: geen documentatie van jaar+datumrange-precedentie; endpoint-hygiëne nieuwe routes: volledig in orde (Swagger, auth, envelope, idempotentie); focus-single-SELECT: NULL-discipline goed maar ongedocumenteerd voor toekomstige subqueries; incident-undo-refresh-fout wordt bewust verzwolgen (gedocumenteerde trade-off).

---

## DEEL 3 — Lows (zeer verkort)

Ellipsis-stijl "..." vs "…" door elkaar (±30 strings, soms binnen één bestand); diakrieten-sweep ("Privé/Financiële/geïmporteerd" op ~8 plekken fout); "Clean" vs "Schoon", "Week Journal", heatmap-"Mar", "N pending"/"Shifts"-Engels in datakwaliteit; register "Geschiedenis" vs "Historie"; toast-register per feature verschillend; kiosk "Naamloze notitie" bij summary-mode (inhoud geblankt — backend kan een preview-veld meegeven); kiosk-notes-ranking cap 100 nieuwste (stille afkapping van juist de overdue-zwaargewichten); heatmap-cellen spreken ISO-datums uit + rij/kolom-semantiek omgekeerd + opent op een jaar geleden gescrold; ConfirmDialog-X ~16px (onder 24×24-ondergrens); CollapsibleSection-hoogte-animatie negeert reduced-motion; Modal herstelt body-overflow zonder vorige waarde; ErrorBoundary toont nog rauwe Engelse message; CSP bevat localhost/Tailscale-restanten; context-values Toast/ConfirmDialog niet gememoized (app-brede re-renders); NoteCard zonder memo bij minuut-tick; dead code: `GlobalColorPicker`, `lib/schedule.ts` localStorage-helpers; ColorPill-popover zonder Escape/outside-click; kelvin-microsnap 6536→6500; agenda-Historie 30 vs rooster 500 (badge-drift); kiosk-selectie blijft op gisteren na een nacht; draft-key "new-note" gedeeld tussen sessies; DailyChecklist kwantitatief mist minus op home; slot-dedup verbergt echte dubbele boekingen; mask dekt MaandBalk-hoogtes niet; "Rooster importeren"-knop start in werkelijkheid de Google-sync; filler-parsing "1.000"→€1,00-heuristiek; batchtoasts bij tag-rename (N+1 toasts bij falen); Alt+N/SELECT niet uitgesloten in de "n"-guard; A11y-details (aria-expanded emoji-kiezer, hexcode-kleurnamen, focus na optimistic delete, tabs-zonder-panels aria-controls).

---

## DEEL 4 — Door de reviewers expliciet als CORRECT geverifieerd

Synchrone persister (SSR-veilig, tegen node_modules geverifieerd) · 401-contract incl. orval-route + retry-predicates (forward-slash-keys kloppen) · Monday-first-daymapping end-to-end (géén off-by-one; display-array vs backend-index correct gescheiden) · history-sentinel-statemachine (alle paden getraceerd; twee gedocumenteerde residuen) · Rooster-wissen-keten (transactioneel, meta-reset, confirm-timeout-cleanup) · ISO-week-consolidatie (jaargrens handmatig doorgerekend; geen oude consumenten) · toekomstweek-exclusie Amsterdam-gepind · toast-timer-boekhouding incl. pauze/hervat-wiskunde · wekker-rollback + oude-packs-matching (time-in-name packs matchen nog) · mireds↔kelvin-round-trip + `on:true`-injectie gespiegeld in de sim · urenregel-contract t/m race-safe SQL · due-date/aging-logica (Amsterdam, betaald uitgesloten, stabiele sort, chip=badge) · reply-chip Re:-dedup + conversation_id-bron · `period_voltooid_count`-mapping · heatmap-padding (aria-hidden, datum-math op data-indexen) · night-dim/jitter (geen extra timers; kiosk-timer-audit volledig schoon, geen leaks) · CLEAR_ALL_CACHES-precache-filter matcht de echte cachenaam · offline.html in de precache-manifest bevestigd.

## Geschrapte claim (false positive)

De geclaimde kapotte `\sync\status`-URL (agenda-agent M5) bestaat niet in de huidige code — `lib/api.ts:1784` gebruikt forward slashes. Tweede maal dat een backslash-URL-claim vals alarm blijkt; zulke claims voortaan altijd verifiëren.

---

## Aanbevolen fixvolgorde R3

1. **One-liners/kleine chirurgie:** H13 filler-labels · H11 banner-gate (`queueLightCommands`) · spatie-`e.repeat` · M8-weeklabel · H15 `&& !data`-guard + retry · portal-hero via `handleChangeView` · contactId-reset in reply-paden · N5-metric-tone home-finance-cel.
2. **Dataverlies/doodlopers:** H1 modal-retry-snapshot · H2 409-herstelflow · H4 confirm-guard unificeren · H6 betaalverzoek-vervolgstap · stepper flush-on-unmount + date-key · H3 sessie-verlopen-overlay.
3. **Backend-waarheid:** H12 engine-queue-statePatch (+ broadcast/poller-offline-markering) · H10 schedule-wipe-cleanup (Todoist/Google) · H7 loonstrook-upsert · command-TTL · Telegram-untoggle.
4. **Schaal/perf:** H5 companies/contacts-query + server-side zoeken · H9 stats-SQL + debounce · caps-cluster LaventeCare (uren/timeline/operations/mailbox per context) · persist-scope + buster echt zetten.
5. **Coherentie:** periode-streak-eenheden + badge-toasts uit de toggle-response · NextShiftCard-helper delen · pending in de rooster-tijdlijn · pauze-semantiek (vensters of eerlijke confirm-copy) · heatmap/backend-day%-pariteit met N5.
6. **First-run:** H8 salaris-uploader · dode rooster-tabs · maturity-scores · lampen-koppel-CTA op home.
7. Copy/a11y-veegronde (ellipsis, diakrieten, contrast-tokens, focus-management-details).
