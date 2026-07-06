# Globale Contacten / Relaties — moduleontwerp

**Status:** ontwerp (research afgerond, nog niet gebouwd) · **Datum:** 2026-07-06
**Repos:** `JeffriesBackend` (Go/Postgres/Grok-AI) + `JeffriesHomeapp` (Next.js)

Eén nieuwe, eigenstandige module die álle relaties beheert — familie, vrienden/kennissen,
collega's/netwerk én zakelijk (LaventeCare) — met WhatsApp-context en volledige koppeling
aan de Telegram AI-bot.

---

## 1. Doel & keuzes (bevestigd)

| Onderwerp | Keuze |
|---|---|
| Architectuur | **Eén unified module.** Zakelijk (LaventeCare) wordt één relatie-*type*; de LaventeCare-schermen worden een gefilterde *lens* op dezelfde contacten. |
| Relatietypes | familie · vrienden/kennissen · zakelijk (LaventeCare) · collega's/netwerk (uitbreidbaar) |
| Velden per contact | licht: **belangrijke datums** (verjaardag…) + **vrije notities**. Zakelijke contacten houden hun rijke CRM-velden. Telefoon/e-mail/adres optioneel. |
| AI-bot (Grok) | opzoeken & vertellen · toevoegen/bijwerken · **feiten onthouden** · **proactieve reminders** |
| WhatsApp-bron | **chat-export (.txt/.zip)** — binnen de regels, geen ban-risico. (Live-bridge bewust afgewezen.) |
| WhatsApp → AI | **alleen samenvattingen/feiten** naar Grok, niet de rauwe berichten |

---

## 2. Huidige situatie (relevant, geverifieerd)

- **DB:** PostgreSQL via `pgx`. **Schema wordt NIET via `migrations/` beheerd** (dode code) maar
  idempotent in `internal/store/runtime_schema.go` → `EnsureRuntimeSchema()` (base tables +
  `ensure*Schema`-repairs, bij startup). **Nieuwe tabellen komen hier.**
- **Single-tenant:** elke tabel heeft `user_id`; elke query filtert daarop.
- **LaventeCare-CRM** (`internal/model/laventecare.go`, `internal/store/laventecare.go`,
  `internal/handler/laventecare.go`, routes `/api/v1/laventecare/*`): `lc_companies` →
  `lc_contacts` (N:1) → leads/projecten/workstreams/acties + billing + `lc_activity_events`
  (generieke tijdlijn) + mailbox. `lc_contacts` heeft al: naam, email, telefoon, rol, is_primary,
  notities, preferred_channel, decision_role, company_id.
- **Context-koppeling:** notes én afspraken dragen `business_context_type/id/title`.
  Types nu: `laventecare`, `laventecare_company/_lead/_project/_workstream`. Server-side inferentie
  (`internal/engine/business_context.go`) kijkt **hardcoded** naar `lc_companies`. Frontend:
  `components/laventecare/BusinessContextPicker.tsx` + `lib/workspace-context.ts`. **Dit is dé plek
  om te generaliseren.**
- **AI = xAI Grok 4.3** (`internal/ai/grok.go`), **72 tools / 11 agents**. Tools declareren:
  `internal/ai/tools.go` → policy `internal/ai/agents.go` → handler-case `internal/engine/executor.go`
  → store-methode → model. Mutaties krijgen automatisch de bevestigingsflow
  (`internal/engine/pending_actions.go`). De bot kan nu al klanten/contacten *lezen*.
- **Frontend module-patroon:** `app/<route>/page.tsx` + `hooks/use<X>.ts` + `<x>Api` in
  `lib/api.ts` + één regel in `components/layout/navigation.ts` (desktop-sidebar + mobiele
  bottom-nav pakken het automatisch op).

---

## 3. Doelarchitectuur — unified Contacts

### 3.1 Datamodel (nieuw, in `runtime_schema.go`)

**`contacts`** — het masterregister van alle personen.
```
contacts(
  id uuid pk,
  user_id text not null,
  display_name text not null,
  relationship_types text[] not null default '{}',   -- ['family','friend','colleague','business']
  notes text,                                         -- vrije notities
  email text, phone text, address text,               -- allemaal optioneel
  organization_id uuid null,                          -- FK → lc_companies (werkt-bij / zakelijk)
  business_role text null,                            -- rol binnen de organisatie
  last_contacted_at timestamptz null,                 -- voor "keep in touch"-reminders
  archived bool not null default false,
  created_at timestamptz, updated_at timestamptz
)
-- index: (user_id), (user_id, organization_id), GIN(relationship_types)
```

**`contact_important_dates`** — verjaardagen e.d. (jaar optioneel; recurring voor reminders).
```
contact_important_dates(
  id uuid pk, user_id text, contact_id uuid fk,
  kind text,                 -- 'birthday' | 'anniversary' | 'other'
  label text,
  month int, day int, year int null,
  recurring bool default true,
  created_at timestamptz
)
```

**`contact_facts`** — losse feiten die je (of de AI) aan een persoon koppelt = "feiten onthouden".
```
contact_facts(
  id uuid pk, user_id text, contact_id uuid fk,
  fact text,                 -- "houdt van hardlopen", "verhuisd naar Amsterdam"
  source text,               -- 'manual' | 'telegram' | 'whatsapp_summary'
  occurred_at timestamptz null,
  created_at timestamptz
)
```

### 3.2 Hoe LaventeCare erin past (unified, gefaseerd)

- **`lc_companies` = organisaties.** Blijven bestaan; `contacts.organization_id` verwijst ernaar.
- **`lc_contacts` → migreren naar `contacts`** met `relationship_types=['business']`,
  `organization_id = oud company_id`, `business_role = rol`, en email/telefoon/notities mee.
  Eenmalige idempotente backfill (in `runtime_schema.go` of los script).
- **LaventeCare-UI (Customers/Dossier) wordt een lens:** leest de unified `contacts` gefilterd op
  `business`. De rest van LaventeCare (leads/projecten/workstreams/billing/activity) blijft
  ongewijzigd; hun `contact_id` gaat later naar de unified `contacts` wijzen.
- **Risico-isolatie:** dit is de grootste ingreep → **laatste fase** (zie §7). In fase 0–2 blijft
  LaventeCare volledig ongemoeid en leeft de nieuwe module ernaast.

### 3.3 Generieke koppeling (resolver-registry)

Vandaag koppelen notes/afspraken alleen aan LaventeCare-entiteiten. We generaliseren zó dat elk
record aan **elke** relatie kan hangen:
- **Kolommen ongewijzigd** (`business_context_type/id/title` blijven — geen risicovolle rename),
  maar we breiden de toegestane `type`-waarden uit met **`contact`** (id = `contacts.id`,
  title = `display_name`).
- **Backend:** `internal/engine/business_context.go` wordt van één hardcoded `lc_companies`-lookup
  naar een **resolver-registry** (`type -> Resolver{ InferFromText, ListOptions }`): een
  `CompanyResolver` (bestaand) + een nieuwe `ContactResolver`.
- **Frontend:** `BusinessContextPicker` krijgt een groep **"Contacten/Relaties"** naast
  Klantdossiers/Leads/Projecten. Label wordt neutraler ("Koppeling/Relatie").
- **Resultaat:** een notitie of afspraak "Verjaardag mama" koppelt aan contact *Mama*; de AI en de
  agenda kunnen die koppeling gebruiken.

---

## 4. WhatsApp-ingestie (export-gebaseerd)

Flow: **upload → parse → opslaan → samenvatten → feiten**.

- **Upload-UI** (frontend): `.txt` of `.zip` (WhatsApp "Exporteer chat"). Preview + **koppel aan een
  contact** (handmatig: WA-chatnaam → contact).
- **Parser** (backend, `internal/whatsapp/` nieuw): WhatsApp-exportregels
  `[dd-mm-jj hh:mm:ss] Naam: bericht` (locale-varianten), media-regels ("‹media weggelaten›"),
  systeemregels. Robuust tegen NL/EN-formaten.
- **Opslag** (lokaal in Postgres, voor app-context/zoeken — NIET naar de AI):
  ```
  whatsapp_conversations(id, user_id, contact_id, chat_name, is_group, message_count,
                         first_message_at, last_message_at, source_filename, imported_at)
  whatsapp_messages(id, user_id, conversation_id, sender, from_me, sent_at, body, kind)
  ```
- **Samenvatter** → produceert per contact/gesprek een **samenvatting + feiten**:
  ```
  whatsapp_summaries(id, user_id, contact_id, conversation_id, summary, fact_count, generated_at)
  ```
  De samenvatting kan (a) lokaal/regelgebaseerd of (b) via Grok in een aparte, expliciete stap.
  **Alleen deze samenvattingen** zijn zichtbaar voor de gewone AI-tools (privacykeuze).
- Belangrijke feiten kunnen als `contact_facts(source='whatsapp_summary')` worden weggeschreven.

---

## 5. AI-koppeling (Grok) — nieuwe tools

Volg exact het bestaande patroon (tools.go → agents.go → executor.go → store → model). Nieuwe agent
`contacten` (of onderbrengen bij `brain`/`dashboard`).

**Lezen (geen bevestiging):**
- `contactenOpvragen` — lijst/zoek contacten, filter op relatie-type.
- `contactOpvragen` — één contact incl. datums, feiten, WhatsApp-samenvatting, gekoppelde notes/afspraken.
- `belangrijkeDatumsOpvragen` — aankomende verjaardagen/jubilea ("wanneer is mama jarig?").
- `whatsappSamenvattingOpvragen` — **alleen** `whatsapp_summaries` (nooit rauwe berichten → privacy).

**Schrijven (bevestiging via bestaande flow):**
- `contactMaken`, `contactBijwerken`.
- `contactFeitOnthouden` — voegt toe aan `contact_facts` (= "onthou dat Piet is verhuisd…").

**Proactieve reminders** (bestaande engine/cron + Telegram-push, zoals de dienst-wekker):
- Dagelijkse job: aankomende **verjaardagen/jubilea** (uit `contact_important_dates`, recurring) +
  **"even contact opnemen met…"** (contacten met oude `last_contacted_at`) → Telegram-bericht.
- Contacten ook toevoegen aan de live-context/briefing van `brain` voor spontane hulp.

---

## 6. Frontend-module

- **Route:** `app/contacten/page.tsx` (kopieer structuur van `app/habits/page.tsx`).
- **Hook:** `hooks/useContacten.ts` (react-query, patroon van `hooks/useLaventeCare.ts`).
- **API:** `contactenApi` in `lib/api.ts` (`list/get/create/update/delete`, `apiFetch<T>`).
- **Nav:** één item in `components/layout/navigation.ts` (bv. sectie *Persoonlijk* of nieuwe sectie
  *Relaties*). Sidebar + bottom-nav pakken het automatisch op.
- **Schermen:** contactenlijst (filter per relatie-type) · contactdetail (datums, feiten, notities,
  WhatsApp-samenvattingen, gekoppelde notes/afspraken) · contactformulier · WhatsApp-import
  (upload + koppel).
- **LaventeCare Customers/Dossier** → later een gefilterde weergave over dezelfde contacten (fase 3).

---

## 7. Gefaseerd plan (risico van laag → hoog)

**Fase 0 — Fundament (persoonlijke contacten):**
`contacts` + `contact_important_dates` + `contact_facts` in `runtime_schema.go`; backend CRUD + API;
frontend module (lijst/detail/form + nav); AI-leestool `contactenOpvragen` + `belangrijkeDatumsOpvragen`.
*LaventeCare blijft volledig ongemoeid.*

**Fase 1 — AI-diepte:**
schrijf-tools `contactMaken/Bijwerken/contactFeitOnthouden`; proactieve reminders
(verjaardagen + keep-in-touch) via engine-cron + Telegram-push; generieke koppeling (resolver-registry
+ picker-groep) zodat notes/afspraken aan een contact kunnen hangen.

**Fase 2 — WhatsApp:**
whatsapp-tabellen; export-parser (.txt/.zip); import-UI; samenvatter → `whatsapp_summaries`;
AI-tool `whatsappSamenvattingOpvragen` (alleen samenvattingen).

**Fase 3 — LaventeCare-unificatie (grootste risico, als laatste):**
migreer `lc_contacts` → `contacts` (business-type + org-link); refactor LaventeCare Customers/Dossier
naar een lens op de unified contacten; laat lc-leads/activity `contact_id` naar de unified contacten wijzen.

Elke fase is los deploybaar en laat de bestaande app werkend.

---

## 8. Privacy & juridisch

- **WhatsApp-export = binnen de voorwaarden** (geen scraping/bridge → geen ban-risico).
- **Rauwe WhatsApp-berichten blijven lokaal** (Postgres) voor app-context/zoeken; **alleen
  samenvattingen** gaan naar Grok (xAI). Sluit aan op de bestaande "UNTRUSTED TOOL DATA"-omkadering.
- Contacten bevatten gevoelige PII → respecteer de bestaande privacy-toggle (`usePrivacy`) op gedeelde
  schermen (masking), en het single-tenant-model.

---

## 9. Open beslissingen (voor de bouw)

1. **Plek in de navigatie:** eigen sectie "Relaties", of onder "Persoonlijk"? Vervangt/absorbeert het
   het huidige LaventeCare-nav-item op termijn?
2. **Reminder-cadans:** hoeveel dagen vooraf voor verjaardagen; na hoeveel dagen "even contact opnemen"?
3. **Groepen:** hebben persoonlijke contacten ook een "groep/kring"-concept nodig (bv. "familie" als
   groep), of volstaan `relationship_types` + notities voorlopig?
4. **Samenvatter:** lokaal/regelgebaseerd of via een expliciete Grok-stap (kost tokens, wel betere feiten)?
5. **WhatsApp-koppeling:** hoe map je een geëxporteerde chat op het juiste contact (handmatig kiezen bij
   import lijkt het veiligst)?

---

*Bouwvolgorde-aanbeveling: begin met Fase 0 (klein, geïsoleerd, direct bruikbaar), dan 1 → 2 → 3.*
