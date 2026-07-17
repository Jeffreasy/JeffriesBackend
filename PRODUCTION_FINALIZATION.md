# Productie afronden

Statusdatum: **17 juli 2026**. Dit document bevat geen geheime waarden en is bedoeld als de korte, uitvoerbare eindcheck voor de vier repositories in `Projecten`.

## Begin hier: één commando

Voer dit uit vanuit `JeffriesBackend`:

```powershell
pwsh -NoProfile -File .\scripts\production-readiness.ps1
```

De standaardmodus is alleen-lezen. De wizard:

- controleert de Git-status en vereiste environment-**namen** van Backend, Homeapp, publieke frontend en Auth;
- vergelijkt lokale trust-koppelingen zonder waarden af te drukken;
- controleert de drie nieuwe Auth-migraties en bekende secret-artifacts;
- controleert Vercel environment-namen, publieke health-endpoints, CORS en TLS;
- wijzigt niets bij Vercel, Render, Clerk, providers of databases.

Gebruik de wizard als herhaalbare inventarisatie, niet als automatische productie-goedkeuring. Onderzoek iedere niet-`PASS` uitkomst en controleer altijd de gedeployde commit; zelfs een volledig groene lokale check bewijst niet dat die lokale commit al op productie staat.

Handige aanvullende modi:

```powershell
# Opnieuw een eenmalige, geauthenticeerde Homeapp E2E uitvoeren.
# De browser-state staat alleen tijdelijk buiten de repositories en wordt standaard verwijderd.
pwsh -NoProfile -File .\scripts\production-readiness.ps1 -Mode HomeappAuthE2E

# Alleen de relevante providerdashboards openen; wijzigt niets.
pwsh -NoProfile -File .\scripts\production-readiness.ps1 -Mode OpenDashboards
```

## Wat al is afgerond

- Vercel-project `jeffries-homeapp` is gekoppeld en bereikbaar via <https://jeffries-homeapp.vercel.app>.
- `HOMEAPP_OWNER_USER_ID` is in Vercel voor zowel **Preview** als **Production** ingesteld; de waarde is niet in dit document of de repository opgenomen. Het toevoegen van deze variabele heeft **geen** productiedeploy gestart en wijzigt een bestaande deployment niet achteraf.
- [Homeapp PR #21](https://github.com/Jeffreasy/JeffriesHomeapp/pull/21) is een open draft-PR met applicatiecommit `a648e07`, CI/service-workerfix `a02b783` en E2E-opvolgcommit `e74955e` voor een geïsoleerde externe testserver.
- Voor de laatste PR-commit `e74955e` zijn `frontend`, `secret-scan`, Vercel en Vercel Preview Comments groen. CodeRabbit is alleen overgeslagen omdat de PR draft is. De laatste Vercel-preview heeft status **READY**.
- De geauthenticeerde browser-smoke tegen de branch-preview is geslaagd voor Dashboard, Contacten, LaventeCare Portal en Instellingen, zonder consolefouten.
- De Homeapp-productie-URL reageerde bij de laatste geslaagde read-only controle nog op de oude productiecommit `fd7ffa5`; PR #21 is niet gemerged of naar productie gepromoveerd. De auditwijzigingen en de nieuwe Production-envvariabele zijn daar dus nog niet door een nieuwe deployment geactiveerd.
- De brede lokale auditverificatie van de vier projecten is uitgevoerd. Auth draait vanaf PR-head `e5c65cb` op Go `1.25.12` en `pgx` `v5.9.2`; de current tree is gesaneerd en GitHub CI heeft secret-scan, verse PostgreSQL-migraties, build, vet, race-tests en `govulncheck` volledig groen afgerond. De wizard is de korte operationele herhaalcheck, niet een vervanging van een productieback-up, staging-restore of expliciete releasebeslissing.

### Release-status per onderdeel

| Onderdeel | Huidige status | Productie-impact tot nu toe |
| --- | --- | --- |
| Homeapp | Draft-PR #21 op `e74955e`; CI groen; preview READY; geauthenticeerde smoke groen | Geen productiedeploy; productie stond bij de laatste controle nog op `fd7ffa5` |
| Backend | [Draft-PR #29](https://github.com/Jeffreasy/JeffriesBackend/pull/29); lokale tests, CI en secret-scan groen | Geen release vanuit deze afrondingsrun |
| Publieke frontend | [Draft-PR #1](https://github.com/Jeffreasy/LaventeCareFrontend/pull/1) op `3a5b642`; lokale checks en 14/14 Playwright groen; alle GitHub-checks groen; Vercel-preview READY | Geen bewuste productpromotie vanuit deze afrondingsrun |
| Auth | [Draft-PR #2](https://github.com/Jeffreasy/LaventeCareAuthSystems/pull/2) op `e5c65cb`; current tree gesaneerd; alle GitHub-checks inclusief verse migraties, race-tests en vulnerability-scan groen | Geen Auth-deploy, productiemigratie, providerrotatie of tenant-keyrotatie uitgevoerd |

## Aanbevolen resterende volgorde

1. Roteer gecompromitteerde providercredentials één provider per keer volgens de tabel hieronder: nieuwe credential maken, alle consumenten bijwerken, redeploy/herstarten, smoke-testen en pas daarna de oude credential intrekken. Dit is nu de meest urgente resterende beveiligingsactie.
2. Review de vier draft-PR's en geef per repository afzonderlijk toestemming voor merge/promotie. De gecontroleerde heads zijn Homeapp `e74955e`, publieke frontend `3a5b642` en Auth `e5c65cb`; verifieer een nieuwere head opnieuw. Houd er rekening mee dat een merge naar de productiebranch van Backend of Auth door Render Auto-Deploy direct een release kan starten.
3. Monitor na een goedgekeurde Homeapp- of publieke-frontendpromotie de resulterende Vercel-productiedeployment en herhaal de login/owner- en publieke browser-smokes.
4. Maak vóór de Auth-release een verse versleutelde databaseback-up, herstel die naar staging en test daar exact de PR-head plus alle drie migratieparen. Kies en test bovendien precies **één** migrator en coördineer Auto-Deploy.
5. Rol Auth pas daarna uit en verifieer health, OIDC, JWKS, contactformulier, CORS, e-mailworker en migratieversie.
6. Rol de tenant-keywijziging afzonderlijk uit. Vervang `TENANT_SECRET_KEY` niet in-place.
7. Draai de wizard opnieuw, controleer de gedeployde SHA's apart en accepteer geen resterende `FAIL`, `UNKNOWN` of andere blockerende niet-`PASS` uitkomst zonder een gedocumenteerde beslissing.

## Providerrotaties en exacte mapping

Voor iedere rij geldt: een credential die ooit in broncode, een migratie, een dump, console-output of Git-geschiedenis stond, moet als gecompromitteerd worden behandeld. Alleen verwijderen uit Git maakt de oude credential niet veilig.

| Provider | Nieuwe credential | Bijwerken in | Afronden met |
| --- | --- | --- | --- |
| Todoist | API-token | Backend live Render-service: `TODOIST_API_TOKEN`; hetzelfde token in het actieve Google Apps Script via `setTodoistToken()` | Backend/Todoist-smoke en Apps Script-run; daarna oude token intrekken |
| bunq | API-key | Backend live Render-service: `BUNQ_API_KEY` | bunq-diagnostiek/transactie-read smoke; daarna oude key intrekken |
| Google OAuth | OAuth client secret en refresh token | Backend live Render-service: `GOOGLE_CLIENT_SECRET` en `GOOGLE_REFRESH_TOKEN`; genereer het refresh token met `scripts/gen-gmail-token.mjs` | Gmail- én Calendar-sync testen; daarna oude secret/token intrekken |
| Microsoft Entra | Client secret | Backend live Render-service: `MICROSOFT_CLIENT_SECRET`; als dezelfde Entra-app voor Auth IMAP OAuth wordt gebruikt, ook die tenant-IMAP-config opnieuw opslaan zodat `imap_accounts.oauth_client_secret_encrypted` wordt vernieuwd | Graph-mailbox en iedere betrokken IMAP-account testen; daarna oude secret verwijderen |
| Telegram | Bot tokens | Backend Render: `TELEGRAM_BOT_TOKEN`; Auth-worker Render: `OBSERVATORY_BOT_TOKEN`; tenantbots via Auth admin-config in `tenants.telegram_config` | Per bot een bericht/test uitvoeren; daarna oude BotFather-token intrekken |
| Clerk | Backend secret | Vercel `jeffries-homeapp`: `CLERK_SECRET_KEY` voor Preview én Production | Nieuwe Preview en Production redeployen en login/owner-gate testen; daarna oude secret intrekken |
| xAI / Groq | API-keys | Backend live Render-service: `GROK_API_KEY` en `GROQ_API_KEY` | Primaire AI-call en fallback/voice-smoke; daarna oude keys intrekken |
| Auth PostgreSQL | Databasecredential/connection string | Bestaande Auth API, Auth worker en de ene gekozen migrator: `DATABASE_URL` | Migratieversie, API-health en worker controleren; daarna oude DB-credential intrekken |

Belangrijk bij Google: de Google-variabelen in Homeapp zijn runtime-ongebruikt. Voeg de nieuwe Google-secret of refresh token daarom niet aan Vercel toe. Verwijder eventuele oude Vercel-varianten pas nadat de nieuwe Homeapp-build nogmaals groen is.

Belangrijk bij Telegram: `TELEGRAM_BOT_TOKEN`, `OBSERVATORY_BOT_TOKEN` en tenant-bottokens zijn verschillende trustdomeinen. Gebruik niet één token voor meerdere rollen.

### Interne trust-koppelingen

Deze waarden zijn geen providercredentials, maar moeten bij een rotatie atomair aan beide kanten worden bijgewerkt:

| Producent/consumer | Vereiste relatie |
| --- | --- |
| Backend `APP_SECRET_KEY` ↔ Vercel Homeapp `BACKEND_API_KEY` | Exact gelijk; eerst beide instellen, dan Homeapp en Backend gecontroleerd redeployen |
| Backend `LAVENTECARE_INTAKE_SECRET` ↔ Auth `HOMEAPP_LAVENTECARE_INTAKE_SECRET` | Exact gelijk; intake-smoke uitvoeren voordat de oude waarde vervalt |
| Backend `BRIDGE_API_KEY` ↔ lokale bridge | Exact gelijk; bridge apart van web- en intakeverkeer houden |

`APP_SECRET_KEY`, `BRIDGE_API_KEY`, `LAVENTECARE_SECRET_KEY` en `LAVENTECARE_INTAKE_SECRET` moeten onderling uniek zijn. Gebruik deze waarden ook niet als tenant-key, JWT-key, providerkey of databasewachtwoord.

## Secret-artifacts en Git-geschiedenis: juiste volgorde

De Auth-current-tree sanitization is afgerond in draft-PR #2: de getrackte dump, echte hashes/ciphertexts/verifier en bekende plaintext credentials zijn uit de huidige tree verwijderd. Current-tree en staged secret-scans, `git diff --check`, de lokale Go `1.25.12` containerchecks en de volledige GitHub-CI zijn groen. De resterende **releaseblokkades** zijn externe credentialrotatie, een verse productieback-up met bewezen staging-restore en het kiezen van één migrator. Daarbij geldt dat:

- de getrackte databaseback-up `backup_before_email_security_20260202_183058.sql` — alleen indien operationeel nodig — eerst als versleutelde, toegangsbeperkte back-up is veiliggesteld, daarna niet meer in de huidige Git-tree staat en back-up/dump-patronen worden genegeerd;
- seed- en correctiemigraties geen echte SMTP-ciphertext, productie-verifiers of herbruikbare vooringevulde wachtwoordhashes bevatten;
- vervangende fixtures alleen deterministische, niet-productiewaarden gebruiken en een verse database nog steeds correct migreert;
- current-tree én staged scans slagen en `git diff --check` schoon is.

De historische inventaris blijft daarna apart relevant:

- Auth-geschiedenis bevatte onder meer een `.env` met JWT-signing- en tenant-keymateriaal, device-secrets, productie-PostgreSQL-credentials en versleutelde SMTP-configuratie. Omdat ook keymateriaal is blootgesteld, moet die ciphertext als potentieel ontsleutelbaar worden behandeld.
- Backend-geschiedenis bevatte een echt Todoist-token in oudere Apps Script/auditbestanden.
- De verwijderde Homeapp-publishable Clerk-key is op zichzelf openbaar clientmateriaal, geen Clerk backend-secret.

Hanteer deze volgorde:

1. **Sanitize de huidige trees vóór nieuwe commits of releases.** Voeg geen nieuwe secret toe en leg per gevonden credential de live consumenten vast.
2. **Roteer/revoke eerst extern en gecoördineerd.** Wacht hiermee niet op een history rewrite. Werk per provider alle consumenten bij, deploy, smoke-test en trek dan de oude credential in. Roteer de Auth-databasecredential pas nadat API, worker en migrator gereedstaan.
3. **Behandel Auth-cryptografische sleutels apart.** Roteer de JWT-signingkey met geteste JWKS/key-overlap of accepteer expliciet dat sessies ongeldig worden. Voer tenant-keyrotatie uitsluitend uit met de versieerbare rollout hieronder; nooit als directe in-place vervanging.
4. **Verifieer na iedere rotatie** dat de oude credential echt geweigerd wordt en dat logs/current trees/scans geen nieuwe waarde bevatten.
5. **Herschrijf Git-geschiedenis pas als afzonderlijke, expliciet goedgekeurde onderhoudsactie.** Dit is nog niet uitgevoerd. Freeze pushes, inventariseer branches/tags/PR-refs, maak een herstelplan, herschrijf gericht, force-push gecoördineerd en laat alle medewerkers opnieuw clonen. History rewrite vervangt nooit credentialrotatie.

## Auth-migraties: veilige productievolgorde

De volgende migratieparen zijn getrackt in Auth draft-PR #2 en zijn op PR-head `e5c65cb` door GitHub CI succesvol toegepast op een verse PostgreSQL 16-database:

1. `20260717000001_public_contact_idempotency`
2. `20260717000002_laventecare_com_origins`
3. `20260717000003_email_logs_sent_at`

Voer ze niet los of rechtstreeks vanuit de huidige werkboom op productie uit. Gebruik deze volgorde:

1. Review de drie `.up.sql`- en drie `.down.sql`-bestanden samen met de code die ervan afhankelijk is; houd ze als één release-eenheid.
2. Behoud de groene CI op exact de te releasen commit en voer `scripts/verify-client-compatibility.ps1` tegen staging uit voordat productie wordt geraakt.
3. Controleer op productie de huidige migratieversie en dat de database niet `dirty` is; voer nog geen `up` uit.
4. Maak direct voor de rollout een versleutelde Render-snapshot/export en controleer dat de restore-instructie en retentie bekend zijn.
5. Test een restore/clone op staging en voer daar de migraties in bovenstaande volgorde uit. Test minimaal registratie/login, public contact (inclusief dubbele request), `.nl`/`.com` CORS, e-mailverzending en workerstart.
6. Pauzeer of coördineer Auto-Deploy zodat API en worker niet tegelijk als migrator starten.
7. Laat in productie precies **één** pre-deploy/job/migrator de migrations uitvoeren. Rol pas na succes API en worker uit.
8. Verifieer `/health`, OIDC discovery, JWKS, contactformulier, CORS en workerlogs. Bewaar de back-up tot na de observatieperiode.

Gebruik geen automatische `down`-migratie als noodknop. Bij dataverliesrisico: applicatierollback plus restore/gerichte forward-fix op basis van de verse back-up.

## Render: belangrijke blueprintwaarschuwing

De live Render-servicenamen en inrichting wijken af van de lokale `render.yaml`, en de bestaande productie-services zijn niet als gekoppelde Blueprint uit dit bestand aangemaakt. Gebruik daarom **niet** “Apply Blueprint” of een nieuwe Blueprint om productie te “synchroniseren”; dat kan dubbele services/databases of verkeerde environment-koppelingen maken.

| Repository | Bestaande live resources | Namen in lokale `render.yaml` |
| --- | --- | --- |
| Backend | service `JeffriesBackend`; database `jeffries-db` | service `jeffriesbackend`; database `jeffries-db` |
| Auth | API `LaventeCareAuthSystems`; database `LaventeCareDB`; worker `dkl-imapworker` | API `laventecare-api`; database `laventecare-db`; worker `dkl-imapworker` |

Werk uitsluitend op de bestaande live service-ID's. Zowel Backend als Auth hebben op de bestaande services Auto-Deploy actief: een push/merge naar hun productiebranch kan dus meteen een productiedeploy starten.

Daarnaast beschrijft de Auth-runbook een Render Pre-Deploy-migratie, terwijl `render.yaml` geen `preDeployCommand` bevat en de Docker `ENTRYPOINT` zelf migrations start. Daardoor kunnen API en worker dezelfde database gelijktijdig proberen te migreren. Kies en test vóór de Auth-rollout één migratie-eigenaar; verwijder/omzeil daarna de concurrerende startmigratie. Pas `render.yaml` pas later aan als we de bestaande live resources bewust aan een Blueprint koppelen.

## Tenant-key: rollout zonder uitval

De versieerbare tenant-keycode is lokaal gerepareerd en getest, maar nog niet gedeployed. Roteer `TENANT_SECRET_KEY` daarom nu nog niet. De veilige aparte rollout is:

1. Commit/deploy eerst de versieerbare code terwijl alleen V1 (`TENANT_SECRET_KEY`) actief blijft. Verifieer API, worker, SMTP, IMAP, Telegram en X/social met de bestaande ciphertext.
2. Maak een verse databaseback-up en inventariseer alle versleutelde waarden in:
   - `tenants.mail_config` plus `mail_config_key_version`;
   - `tenants.telegram_config`;
   - `tenants.x_config`;
   - `imap_accounts.imap_pass_encrypted`;
   - `imap_accounts.oauth_client_secret_encrypted`.
3. Genereer V2 met `go run ./tools/generate_key/`. Zet `TENANT_SECRET_KEY_V2` op zowel de bestaande Auth API als worker; behoud V1 overal.
4. Nieuwe writes gebruiken daarna de hoogste actieve versie. Re-encrypt alle bestaande V1/legacywaarden gecontroleerd naar V2. Het bestaande `tools/encrypt_mail_config` dekt SMTP; de overige velden vereisen een gecontroleerde migratietool of opnieuw opslaan via hun admin-endpoint.
5. Test na re-encryptie SMTP-send, IMAP ingest, Telegram-test en X/social. Controleer dat geen `mail_config_key_version = 1` en geen legacy `enc:<base64>`-ciphertext meer resteert.
6. Verwijder V1 pas daarna uit API en worker. Houd de back-up en oude key offline beschikbaar volgens het incident-/retentiebeleid, niet in Git of losse notities.

Nooit de waarde van `TENANT_SECRET_KEY` vervangen terwijl V1-ciphertext nog bestaat: dat maakt opgeslagen SMTP-, IMAP-, Telegram- en social-credentials onleesbaar.

## Domeinstatus

- Werkende en huidige Homeapp-productie-URL: <https://jeffries-homeapp.vercel.app>.
- `jeffrieshomeapp.com` bestaat op de statusdatum niet in DNS (NXDOMAIN) en is niet als Vercel-domein gekoppeld.
- Zonder custom domain is geen actie nodig; gebruik de werkende `vercel.app`-URL.
- Voor een custom domain blijft handmatig nodig: domein registreren, in Vercel toevoegen, de door Vercel gegeven DNS-records bij de registrar plaatsen en daarna redirects, Clerk-origins, CSP/CORS, Telegram WebApp URL en TLS opnieuw verifiëren.

## Wat echt handmatig blijft

- Nieuwe credentials aanmaken en oude intrekken in Todoist, bunq, Google, Entra, BotFather/Telegram, Clerk, xAI, Groq en Render PostgreSQL. Dit vereist jouw ingelogde provideraccounts en soms MFA/consent.
- De vier draft-PR's reviewen/mergen of bewust naar productie promoveren. READY-previews en smoke-tests nemen die productie-keuze niet over.
- Een verse Auth-databaseback-up/snapshot maken en de restore op staging bevestigen.
- Op de bekende bestaande live Render-services Auto-Deploy rond de migratie coördineren en één migrator kiezen. De lokale blueprint mag dit niet automatisch overnemen.
- Na credentialrotatie eventueel een gecoördineerde Git-history rewrite goedkeuren; dit verandert gedeelde refs en vereist opnieuw clonen.
- Eventueel `jeffrieshomeapp.com` registreren en DNS-eigendom bevestigen; alleen nodig als je dat custom domain wilt.

De afgeronde voorbereiding en de nog open releaseblokkades staan hierboven afzonderlijk vermeld. Werk providerrotaties één voor één af en draai na iedere wijziging de relevante smoke-test plus de read-only wizard; beschouw een lokale groene check nooit als bewijs van een productiedeploy.
