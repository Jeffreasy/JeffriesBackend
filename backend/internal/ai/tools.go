package ai

import "encoding/json"

// AllTools is a slice of all available AI tools (read-only MVP phase)
var AllTools = []ToolDefinition{
	// ── EMAIL ────────────────────────────────────────────────────────
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "leesEmail",
			Description: "Leest de volledige inhoud van één specifieke e-mail.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"emailId": {
						"type": "string",
						"description": "Het interne ID van de email."
					}
				},
				"required": ["emailId"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "zoekEmails",
			Description: "Zoekt in e-mails op trefwoord of afzender.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"query": {
						"type": "string",
						"description": "Zoekterm of e-mailadres."
					},
					"limit": {
						"type": "number",
						"description": "Maximaal aantal resultaten (max 10)."
					}
				},
				"required": ["query"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "markeerGelezen",
			Description: "Markeert een e-mail als gelezen of ongelezen. Deze mutatie komt eerst in de bevestigingswachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"gmailId": {"type": "string", "description": "Gmail message ID."},
					"emailId": {"type": "string", "description": "Alias voor gmailId."},
					"read": {"type": "boolean", "description": "true = gelezen, false = ongelezen."}
				},
				"required": ["gmailId"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "markeerSter",
			Description: "Zet of verwijdert een ster op een e-mail. Deze mutatie komt eerst in de bevestigingswachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"gmailId": {"type": "string", "description": "Gmail message ID."},
					"starred": {"type": "boolean", "description": "true = ster plaatsen, false = ster verwijderen."}
				},
				"required": ["gmailId"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "verwijderEmail",
			Description: "Verplaatst een e-mail naar prullenbak en markeert hem lokaal verwijderd. Deze mutatie komt eerst in de bevestigingswachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"gmailId": {"type": "string", "description": "Gmail message ID."}
				},
				"required": ["gmailId"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "emailVersturen",
			Description: "Verstuurt een nieuwe e-mail via Gmail. Deze mutatie komt eerst in de bevestigingswachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"to": {"type": "string", "description": "Ontvanger."},
					"subject": {"type": "string", "description": "Onderwerp."},
					"body": {"type": "string", "description": "Plain text bericht."}
				},
				"required": ["to", "subject", "body"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "emailBeantwoorden",
			Description: "Beantwoordt een bestaande e-mail in dezelfde Gmail-thread. Deze mutatie komt eerst in de bevestigingswachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"gmailId": {"type": "string", "description": "Gmail message ID."},
					"body": {"type": "string", "description": "Plain text antwoord."}
				},
				"required": ["gmailId", "body"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "bulkMarkeerGelezen",
			Description: "Markeert maximaal 20 e-mails als gelezen of ongelezen. Deze mutatie komt eerst in de bevestigingswachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"gmailIds": {"type": "array", "items": {"type": "string"}},
					"read": {"type": "boolean"}
				},
				"required": ["gmailIds"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "bulkVerwijder",
			Description: "Verwijdert maximaal 20 e-mails. Deze mutatie komt eerst in de bevestigingswachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"gmailIds": {"type": "array", "items": {"type": "string"}}
				},
				"required": ["gmailIds"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "inboxOpruimen",
			Description: "Zoekt e-mails en ruimt maximaal 20 matches op door te markeren als gelezen of te verwijderen. Deze mutatie komt eerst in de bevestigingswachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"query": {"type": "string", "description": "Zoekterm."},
					"action": {"type": "string", "enum": ["mark_read", "delete"], "description": "Actie op matches."},
					"limit": {"type": "number", "description": "Maximaal aantal matches (max 20)."}
				},
				"required": ["query", "action"]
			}`),
		},
	},

	// ── SYSTEM & AUTOMATIONS ──────────────────────────────────────────
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "syncStatusOpvragen",
			Description: "Haalt de status op van rooster, persoonlijke agenda, Gmail sync en command queue.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "automationsOverzicht",
			Description: "Haalt actieve automations, laatste runs en command queue status op.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "contextBriefingOpvragen",
			Description: "Haalt een cross-domain live briefing op voor Telegram/Grok: planning, Gmail, notities, LaventeCare, syncstatus en aanbevolen acties.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"scope": {
						"type": "string",
						"enum": ["vandaag", "morgen", "week", "laventecare"],
						"description": "Periode/focus. Default vandaag."
					},
					"dagen": {
						"type": "number",
						"description": "Aantal dagen vooruit, max 14."
					},
					"limit": {
						"type": "number",
						"description": "Maximaal aantal items per blok, max 12."
					}
				},
				"required": []
			}`),
		},
	},

	// ── ROOSTER & FINANCE ──────────────────────────────────────────────
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "dienstenOpvragen",
			Description: "Haalt het dienstrooster op. Zonder datums gebruikt de backend automatisch de eerstvolgende diensten.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"startIso": {
						"type": "string",
						"description": "Start datum (YYYY-MM-DD)."
					},
					"eindIso": {
						"type": "string",
						"description": "Eind datum (YYYY-MM-DD)."
					}
				},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "contractAnalyseOpvragen",
			Description: "Haalt een slimme contractanalyse op (16 uur basis), berekent wekelijkse uren en het opgebouwde plus/min saldo.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "salarisOpvragen",
			Description: "Haalt een samenvatting of details van de loonstroken op. Jaar en periode zijn optioneel.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"jaar": {
						"type": "number",
						"description": "Het kalenderjaar (bijv. 2026)."
					},
					"periode": {
						"type": "number",
						"description": "Periode of maand (1-12)."
					}
				},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "saldoOpvragen",
			Description: "Haalt actuele totaalsaldo op plus een standaard maand-tot-nu snapshot voor finance status.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "transactiesZoeken",
			Description: "Zoekt in de financiële transacties. Zonder query geeft dit een beperkte recente selectie terug.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"query": {
						"type": "string",
						"description": "Omschrijving, tegenpartij of tegenrekening. Optioneel."
					},
					"limit": {
						"type": "number",
						"description": "Aantal resultaten (max 20)."
					}
				},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "uitgavenOverzicht",
			Description: "Geeft een uitgavenoverzicht met topcategorieën, merchants en kasstroom. Zonder jaar/maand gebruikt de backend de huidige maand tot vandaag.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"jaar": {
						"type": "string",
						"description": "Kalenderjaar, bijvoorbeeld 2026."
					},
					"maand": {
						"type": "string",
						"description": "Maand in YYYY-MM formaat, of maandnummer als jaar apart is ingevuld, bijvoorbeeld 2026-06 of 6."
					},
					"iban": {
						"type": "string",
						"description": "Optionele rekeningfilter."
					},
					"limit": {
						"type": "number",
						"description": "Aantal topregels (max 10)."
					}
				},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "maandVergelijken",
			Description: "Vergelijkt twee financiële maanden op inkomsten, uitgaven, netto stroom en transacties.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"maandA": {
						"type": "string",
						"description": "Eerste maand in YYYY-MM formaat."
					},
					"maandB": {
						"type": "string",
						"description": "Tweede maand in YYYY-MM formaat."
					}
				},
				"required": ["maandA", "maandB"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "vasteLastenAnalyse",
			Description: "Analyseert terugkerende uitgaven op basis van merchants die in meerdere maanden voorkomen.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"jaar": {
						"type": "string",
						"description": "Optioneel kalenderjaar, bijvoorbeeld 2026."
					},
					"limit": {
						"type": "number",
						"description": "Aantal terugkerende posten (max 15)."
					}
				},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "ongelabeldAnalyse",
			Description: "Vindt recente transacties zonder categorie en groepeert ze voor categorisatievoorstellen.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"limit": {
						"type": "number",
						"description": "Aantal transacties zonder categorie (max 30)."
					}
				},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "categorieWijzigen",
			Description: "Wijzigt de categorie van één financiële transactie. Deze mutatie komt eerst in de bevestigingswachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"transactionId": {"type": "string", "description": "UUID van de transactie."},
					"categorie": {"type": "string", "description": "Nieuwe categorie."}
				},
				"required": ["transactionId", "categorie"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "bulkCategoriseren",
			Description: "Wijzigt de categorie van meerdere financiële transacties. Deze mutatie komt eerst in de bevestigingswachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"transactionIds": {"type": "array", "items": {"type": "string"}, "description": "UUIDs van transacties, max 50."},
					"categorie": {"type": "string", "description": "Nieuwe categorie."}
				},
				"required": ["transactionIds", "categorie"]
			}`),
		},
	},

	// ── AGENDA ─────────────────────────────────────────────────────────
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "planningOpvragen",
			Description: "Haalt de gecombineerde planning op: werkdiensten, persoonlijke agenda-afspraken en open notities met een deadline (inclusief overdue) voor een dag of periode.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"startIso": {
						"type": "string",
						"description": "Start datum (YYYY-MM-DD). Laat leeg voor vandaag."
					},
					"eindIso": {
						"type": "string",
						"description": "Eind datum (YYYY-MM-DD). Laat leeg voor dezelfde dag als startIso."
					}
				},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "afsprakenOpvragen",
			Description: "Haalt Google Calendar afspraken op. Zonder datums gebruikt de backend automatisch de eerstvolgende afspraken.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"startIso": {
						"type": "string",
						"description": "Start datum (YYYY-MM-DD)."
					},
					"eindIso": {
						"type": "string",
						"description": "Eind datum (YYYY-MM-DD)."
					}
				},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "afspraakMaken",
			Description: "Maakt een persoonlijke agenda-afspraak. Deze mutatie komt eerst in de bevestigingswachtrij en daarna in de Google Calendar sync-wachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"titel": {"type": "string", "description": "Titel van de afspraak."},
					"startDatum": {"type": "string", "description": "Startdatum YYYY-MM-DD."},
					"startTijd": {"type": "string", "description": "Starttijd HH:MM, leeg bij hele dag."},
					"eindDatum": {"type": "string", "description": "Einddatum YYYY-MM-DD."},
					"eindTijd": {"type": "string", "description": "Eindtijd HH:MM, leeg bij hele dag."},
					"heledag": {"type": "boolean"},
					"locatie": {"type": "string"},
					"beschrijving": {"type": "string"},
					"symbol": {"type": "string", "description": "Optioneel UI-symbool."},
					"businessContextType": {"type": "string", "enum": ["laventecare", "laventecare_company", "laventecare_lead", "laventecare_workstream", "laventecare_project"], "description": "Optionele zakelijke context voor LaventeCare."},
					"businessContextId": {"type": "string", "description": "Optioneel klant-, lead-, opdracht- of project-id als de afspraak aan een specifiek LaventeCare object hangt."},
					"businessContextTitle": {"type": "string", "description": "Leesbare naam van de zakelijke context."}
				},
				"required": ["titel", "startDatum"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "afspraakBewerken",
			Description: "Bewerkt een persoonlijke agenda-afspraak. Deze mutatie komt eerst in de bevestigingswachtrij en daarna in de Google Calendar sync-wachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"eventId": {"type": "string", "description": "Event ID uit persoonlijke agenda."},
					"titel": {"type": "string"},
					"startDatum": {"type": "string"},
					"startTijd": {"type": "string"},
					"eindDatum": {"type": "string"},
					"eindTijd": {"type": "string"},
					"heledag": {"type": "boolean"},
					"locatie": {"type": "string"},
					"beschrijving": {"type": "string"},
					"symbol": {"type": "string"},
					"businessContextType": {"type": "string", "enum": ["laventecare", "laventecare_company", "laventecare_lead", "laventecare_workstream", "laventecare_project"]},
					"businessContextId": {"type": "string"},
					"businessContextTitle": {"type": "string"}
				},
				"required": ["eventId"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "afspraakVerwijderen",
			Description: "Verwijdert een persoonlijke agenda-afspraak. Deze mutatie komt eerst in de bevestigingswachtrij en daarna in de Google Calendar sync-wachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"eventId": {"type": "string", "description": "Event ID uit persoonlijke agenda."}
				},
				"required": ["eventId"]
			}`),
		},
	},

	// ── NOTITIES & HABITS ──────────────────────────────────────────────
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "notitiesZoeken",
			Description: "Zoekt in persoonlijke notities en de knowledge base.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"query": {
						"type": "string",
						"description": "De zoekterm."
					}
				},
				"required": ["query"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "notitiesOverzicht",
			Description: "Haalt een compact overzicht van recente actieve notities op.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"limit": {
						"type": "number",
						"description": "Aantal notities (max 20)."
					}
				},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "notitieAanmaken",
			Description: "Maakt een nieuwe notitie aan in het systeem op basis van wat de gebruiker vertelt.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"titel": {
						"type": "string",
						"description": "Een korte en bondige titel voor de notitie."
					},
					"inhoud": {
						"type": "string",
						"description": "De volledige inhoud van de notitie. Mag markdown bevatten."
					},
					"tags": {
						"type": "array",
						"items": {"type": "string"},
						"description": "Lijst van tags (zonder #), bijv. ['idee', 'werk', 'huis']."
					},
					"prioriteit": {
						"type": "string",
						"enum": ["laag", "normaal", "hoog"],
						"description": "Optionele prioriteit wanneer de gebruiker urgentie aangeeft."
					},
					"symbol": {
						"type": "string",
						"description": "Optioneel frontend-symbool, bijv. note, check, calendar, warning, work, finance, habit, shield, sparkles of light."
					},
					"deadline": {
						"type": "string",
						"description": "Optionele deadline als ISO datum/tijd of yyyy-mm-dd wanneer de gebruiker een datum noemt."
					},
					"triage_flag": {
						"type": "boolean",
						"description": "Zet op true wanneer de notitie vandaag aandacht nodig heeft, urgent is of een open actie bevat."
					},
					"businessContextType": {
						"type": "string",
						"enum": ["laventecare", "laventecare_company", "laventecare_lead", "laventecare_workstream", "laventecare_project"],
						"description": "Optionele zakelijke context; gebruik laventecare_company voor klantdossiers en laventecare_* wanneer de notitie over het bedrijf, een lead, opdracht of project gaat."
					},
					"businessContextId": {
						"type": "string",
						"description": "Optioneel klant-, lead-, opdracht- of project-id als bekend."
					},
					"businessContextTitle": {
						"type": "string",
						"description": "Leesbare naam van lead/project of 'LaventeCare'."
					}
				},
				"required": ["titel", "inhoud"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "notitiesVandaag",
			Description: "Haalt notities op die vandaag zijn aangemaakt of gewijzigd. Dit is een vandaag-filter, geen volledig actief notitieoverzicht.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "notitiePinnen",
			Description: "Zet een bestaande notitie vast of haalt de pin eraf. Gebruik een id uit notitiesOverzicht of Live Data.notes.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"id": {"type": "string", "description": "Notitie-id."},
					"pinned": {"type": "boolean", "description": "Optioneel: true om vast te zetten, false om los te maken. Zonder waarde wordt gewisseld."}
				},
				"required": ["id"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "notitieBewerken",
			Description: "Bewerkt een bestaande notitie. Deze mutatie loopt via bevestiging voordat hij wordt uitgevoerd.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"id": {"type": "string", "description": "Notitie-id."},
					"titel": {"type": "string"},
					"inhoud": {"type": "string", "description": "Nieuwe volledige inhoud wanneer de inhoud vervangen moet worden."},
					"tags": {"type": "array", "items": {"type": "string"}},
					"prioriteit": {"type": "string", "enum": ["laag", "normaal", "hoog"]},
					"symbol": {"type": "string"},
					"deadline": {"type": "string", "description": "ISO datum/tijd, yyyy-mm-dd, dd-mm-yyyy of leeg om te wissen."},
					"triage_flag": {"type": "boolean"},
					"is_completed": {"type": "boolean"},
					"businessContextType": {"type": "string", "enum": ["laventecare", "laventecare_company", "laventecare_lead", "laventecare_workstream", "laventecare_project"]},
					"businessContextId": {"type": "string"},
					"businessContextTitle": {"type": "string"}
				},
				"required": ["id"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "notitieArchiveren",
			Description: "Archiveert of herstelt een bestaande notitie. Deze mutatie loopt via bevestiging voordat hij wordt uitgevoerd.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"id": {"type": "string", "description": "Notitie-id."},
					"archived": {"type": "boolean", "description": "Optioneel: true archiveert, false zet terug."}
				},
				"required": ["id"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "bulkArchiveerNotities",
			Description: "Archiveert meerdere notities tegelijk. Deze mutatie loopt via bevestiging voordat hij wordt uitgevoerd.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"ids": {"type": "array", "items": {"type": "string"}, "description": "Maximaal 20 notitie-id's."}
				},
				"required": ["ids"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "habitAanmaken",
			Description: "Maakt een nieuwe habit met verstandige defaults. Deze mutatie wordt direct uitgevoerd.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"naam": {"type": "string", "description": "Naam van de habit."},
					"emoji": {"type": "string", "description": "Emoji voor de habit."},
					"type": {"type": "string", "description": "positief of negatief."},
					"beschrijving": {"type": "string"},
					"frequentie": {"type": "string", "description": "dagelijks, weekdagen, weekenddagen, aangepast, x_per_week of x_per_maand."},
					"aangepaste_dagen": {"type": "array", "items": {"type": "number"}, "description": "0=zondag t/m 6=zaterdag."},
					"doel_aantal": {"type": "number"},
					"rooster_filter": {"type": "string", "description": "alle, werkdagen, vrijeDagen, vroegeDienst of lateDienst."},
					"is_kwantitatief": {"type": "boolean"},
					"doel_waarde": {"type": "number"},
					"eenheid": {"type": "string", "description": "Bijv. min, ml, km, pg of x."},
					"doel_tijd": {"type": "string", "description": "HH:MM."},
					"moeilijkheid": {"type": "string", "description": "makkelijk, normaal of moeilijk."},
					"kleur": {"type": "string"}
				},
				"required": ["naam"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "habitVoltooien",
			Description: "Vinkt een habit af, zet meetbare voortgang, of maakt het afvinken ongedaan (heropenen) voor een datum. Zet voltooid=false om een eerder afgevinkte habit weer te openen. Naam mag gebruikt worden als ID ontbreekt.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"id": {"type": "string", "description": "Habit UUID."},
					"habitId": {"type": "string", "description": "Habit UUID alternatief."},
					"naam": {"type": "string", "description": "Habitnaam als ID onbekend is."},
					"datum": {"type": "string", "description": "YYYY-MM-DD, standaard vandaag."},
					"waarde": {"type": "number", "description": "Meetwaarde voor kwantitatieve habits."},
					"voltooid": {"type": "boolean", "description": "Standaard true (afvinken). Zet false om het afvinken ongedaan te maken (heropenen) — verwijdert de xp voor die dag."},
					"notitie": {"type": "string"}
				},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "habitIncident",
			Description: "Logt een incident of terugval bij een negatieve habit. Deze mutatie komt eerst in de bevestigingswachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"id": {"type": "string", "description": "Habit UUID."},
					"habitId": {"type": "string", "description": "Habit UUID alternatief."},
					"naam": {"type": "string", "description": "Habitnaam als ID onbekend is."},
					"trigger": {"type": "string"},
					"notitie": {"type": "string"}
				},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "habitNotitie",
			Description: "Voegt een notitie toe aan de habit-log van vandaag of een opgegeven datum.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"id": {"type": "string", "description": "Habit UUID."},
					"habitId": {"type": "string", "description": "Habit UUID alternatief."},
					"naam": {"type": "string", "description": "Habitnaam als ID onbekend is."},
					"datum": {"type": "string", "description": "YYYY-MM-DD, standaard vandaag."},
					"notitie": {"type": "string"}
				},
				"required": ["notitie"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "habitsOverzicht",
			Description: "Haalt een overzicht van actieve habits op.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "habitStreaks",
			Description: "Haalt habit streaks, XP en totale voltooiingen op.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "habitBadges",
			Description: "Haalt behaalde habit badges op.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "habitRapport",
			Description: "Haalt een compact habit rapport op met stats, actieve habits, badges en heatmap.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"dagen": {
						"type": "number",
						"description": "Aantal dagen heatmap, max 60."
					}
				},
				"required": []
			}`),
		},
	},

	// ── LAVENTECARE ────────────────────────────────────────────────────
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareCockpit",
			Description: "Haalt de LaventeCare status cockpit op (Leads, projecten, SLA's).",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareKennisZoeken",
			Description: "Zoekt in de LaventeCare documenten en protocollen.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"query": {
						"type": "string",
						"description": "Zoekterm voor documenten."
					}
				},
				"required": ["query"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareKennisAdviesOpvragen",
			Description: "Geeft read-only AI-advies welke LaventeCare kennisdocumenten/templates passen bij een klant, lead, opdracht, project of vrije context. Wijzigt niets.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"company_id": {"type": "string", "description": "Optionele klant/company UUID."},
					"lead_id": {"type": "string", "description": "Optionele lead UUID."},
					"project_id": {"type": "string", "description": "Optionele project UUID."},
					"workstream_id": {"type": "string", "description": "Optionele opdracht/workstream UUID."},
					"query": {"type": "string", "description": "Vrije context of zoekterm wanneer er geen UUID bekend is."},
					"limit": {"type": "number", "description": "Aantal aanbevelingen (max 20)."}
				},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareDossierCheckOpvragen",
			Description: "Controleert read-only de volledigheid van een LaventeCare klant-/lead-/opdracht-/projectdossier: aanwezige PDF's, ontbrekende bouwblokken, aanbevolen templates en vervolgstappen.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"company_id": {"type": "string", "description": "Optionele klant/company UUID."},
					"lead_id": {"type": "string", "description": "Optionele lead UUID."},
					"project_id": {"type": "string", "description": "Optionele project UUID."},
					"workstream_id": {"type": "string", "description": "Optionele opdracht/workstream UUID."},
					"query": {"type": "string", "description": "Vrije context of zoekterm wanneer er geen UUID bekend is."},
					"limit": {"type": "number", "description": "Aantal aanbevelingen (max 20)."}
				},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareKlantenOpvragen",
			Description: "Haalt LaventeCare klantdossiers op als centrale CRM-basis. Technisch zijn dit companies; zakelijk zijn het klanten, prospects, partners, leveranciers of interne/eigen projectcontexten.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"limit": {"type": "number", "description": "Aantal klanten (max 30)."},
					"query": {"type": "string", "description": "Optionele zoekterm op bedrijfsnaam of website."},
					"q": {"type": "string", "description": "Alias voor query."}
				},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareContactenOpvragen",
			Description: "Haalt LaventeCare contactpersonen op, optioneel gefilterd op company_id.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"limit": {"type": "number", "description": "Aantal contacten (max 30)."},
					"company_id": {"type": "string", "description": "Optionele klant/company UUID."}
				},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "contactenOpvragen",
			Description: "Haalt contacten/relaties op uit de globale Contacten-module (familie, vrienden, collega's, zakelijk). Optioneel filteren op relatie-type, vrije labels of zoekterm. Elk contact heeft relationship_types (basis) én labels (vrije, gekleurde tags zoals 'investeerder' of 'VIP'). Gebruik 'labels' om op die tags te filteren, bijv. 'welke investeerders ken ik'.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"limit": {"type": "number", "description": "Aantal contacten (max 50)."},
					"query": {"type": "string", "description": "Optionele zoekterm op naam, e-mail of notitie."},
					"type": {"type": "string", "description": "Optioneel relatie-type: family, friend, colleague, business."},
					"labels": {"type": "array", "items": {"type": "string"}, "description": "Optioneel: filter op één of meer labelnamen (bijv. ['investeerder'])."},
					"label_match": {"type": "string", "description": "Bij meerdere labels: 'any' (standaard, minstens één) of 'all' (alle labels)."}
				},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "belangrijkeDatumsOpvragen",
			Description: "Haalt aankomende belangrijke datums van contacten op (verjaardagen, jubilea) binnen een venster, gesorteerd op eerstvolgende. Gebruik dit voor vragen als 'wie is er binnenkort jarig?'.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"days": {"type": "number", "description": "Vooruitkijk-venster in dagen (standaard 30, max 365)."}
				},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "contactMaken",
			Description: "Maakt een nieuw contact/relatie aan in de Contacten-module. Vereist een naam; relationship_types zijn optioneel (family, friend, colleague, business). Labels zijn vrije tags die worden aangemaakt als ze nog niet bestaan.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"display_name": {"type": "string", "description": "Naam van de persoon."},
					"relationship_types": {"type": "array", "items": {"type": "string"}, "description": "Optioneel: family, friend, colleague, business."},
					"labels": {"type": "array", "items": {"type": "string"}, "description": "Optionele vrije labels/tags (bijv. ['investeerder','VIP'])."},
					"email": {"type": "string"},
					"phone": {"type": "string"},
					"notes": {"type": "string", "description": "Optionele vrije notitie."}
				},
				"required": ["display_name"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "contactBijwerken",
			Description: "Werkt een bestaand contact bij. Geef contact_id mee (uit contactenOpvragen) en alleen de velden die wijzigen.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"contact_id": {"type": "string", "description": "UUID van het contact."},
					"display_name": {"type": "string"},
					"relationship_types": {"type": "array", "items": {"type": "string"}},
					"email": {"type": "string"},
					"phone": {"type": "string"},
					"address": {"type": "string"},
					"notes": {"type": "string"}
				},
				"required": ["contact_id"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "contactFeitOnthouden",
			Description: "Onthoudt een los feit bij een contact (bijv. 'Piet is verhuisd naar Amsterdam'). Geef contact_id mee (uit contactenOpvragen).",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"contact_id": {"type": "string", "description": "UUID van het contact."},
					"fact": {"type": "string", "description": "Het feit om te onthouden."}
				},
				"required": ["contact_id", "fact"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "whatsappSamenvattingOpvragen",
			Description: "Haalt samenvattingen (metadata, GEEN letterlijke berichten) van geïmporteerde WhatsApp-gesprekken op, optioneel voor één contact via contact_id.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"contact_id": {"type": "string", "description": "Optioneel: UUID van het contact."},
					"limit": {"type": "number", "description": "Aantal samenvattingen (max 30)."}
				},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "labelsOpvragen",
			Description: "Haalt de labelvocabulaire van de gebruiker op: alle vrije labels/tags in de Contacten-module met hun kleur en hoeveel contacten ze hebben. Gebruik dit om te weten welke labels bestaan voordat je op labels filtert of een label toekent.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "contactsOmTeSpreken",
			Description: "Haalt contacten op die je al een tijd niet hebt gesproken (op basis van de laatst gelogde interactie), oudste eerst. Gebruik dit voor 'wie moet ik weer eens spreken?'. days_since is het aantal dagen sinds het laatste contact (null = nog nooit gelogd).",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"days": {"type": "number", "description": "Minimaal aantal dagen sinds het laatste contact (standaard 60)."},
					"limit": {"type": "number", "description": "Aantal contacten (max 100, standaard 25)."}
				},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "contactLabelToevoegen",
			Description: "Kent een vrij label/tag toe aan een bestaand contact (het label wordt aangemaakt als het nog niet bestaat). Gebruik dit voor 'markeer Jan als investeerder' of 'label Sophie als VIP'. Geef contact_id mee (uit contactenOpvragen).",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"contact_id": {"type": "string", "description": "UUID van het contact."},
					"label": {"type": "string", "description": "De labelnaam (bijv. 'investeerder')."},
					"color": {"type": "string", "description": "Optionele kleur: slate, amber, sky, emerald, rose, violet, orange, teal, blue, pink, lime, cyan, red, indigo, fuchsia."}
				},
				"required": ["contact_id", "label"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareLeadsOpvragen",
			Description: "Haalt recente LaventeCare leads op. Geef company_id mee als de vraag over een specifiek klantdossier gaat, anders krijg je leads van alle klanten door elkaar.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"limit": {
						"type": "number",
						"description": "Aantal leads (max 30)."
					},
					"company_id": {
						"type": "string",
						"description": "Optionele klant/company UUID om alleen leads van dit bedrijf op te halen."
					}
				},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareProjectenOpvragen",
			Description: "Haalt recente LaventeCare projecten op. Geef company_id mee als de vraag over een specifiek klantdossier gaat, anders krijg je projecten van alle klanten door elkaar.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"limit": {
						"type": "number",
						"description": "Aantal projecten (max 30)."
					},
					"company_id": {
						"type": "string",
						"description": "Optionele klant/company UUID om alleen projecten van dit bedrijf op te halen."
					}
				},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareOpdrachtenOpvragen",
			Description: "Haalt recente LaventeCare opdrachten/werkstreams op voor flexibele kleine en middelgrote klussen. Geef company_id mee als de vraag over een specifiek klantdossier gaat, anders krijg je opdrachten van alle klanten door elkaar.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"limit": {
						"type": "number",
						"description": "Aantal opdrachten (max 30)."
					},
					"include_closed": {
						"type": "boolean",
						"description": "Ook afgeronde/gearchiveerde opdrachten tonen."
					},
					"company_id": {
						"type": "string",
						"description": "Optionele klant/company UUID om alleen opdrachten van dit bedrijf op te halen."
					}
				},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareActiesOpvragen",
			Description: "Haalt open LaventeCare actie-items op. Geef company_id mee als de vraag over een specifiek klantdossier gaat, anders krijg je acties van alle klanten door elkaar.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"limit": {
						"type": "number",
						"description": "Aantal acties (max 30)."
					},
					"company_id": {
						"type": "string",
						"description": "Optionele klant/company UUID om alleen acties van dit bedrijf op te halen."
					}
				},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareDossierDocumentenOpvragen",
			Description: "Haalt recent vastgelegde LaventeCare PDF dossierdocumenten op, optioneel gefilterd op klant, lead, opdracht of project.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"limit": {
						"type": "number",
						"description": "Aantal documenten (max 30)."
					},
					"lead_id": {
						"type": "string",
						"description": "Optionele lead UUID om op te filteren."
					},
					"project_id": {
						"type": "string",
						"description": "Optionele project UUID om op te filteren."
					},
					"workstream_id": {
						"type": "string",
						"description": "Optionele opdracht/workstream UUID om op te filteren."
					},
					"company_id": {
						"type": "string",
						"description": "Optionele klant/company UUID om op te filteren."
					}
				},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareBillingOpvragen",
			Description: "Haalt LaventeCare offertes, urenregels, facturen, open bedragen en bunq-readiness op. Alleen lezen; geen facturen of betaalverzoeken maken.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"limit": {
						"type": "number",
						"description": "Aantal recente items (max 80)."
					},
					"company_id": {
						"type": "string",
						"description": "Optionele klant/company UUID om commercie op klantniveau te filteren."
					}
				},
				"required": []
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareBetaalverzoekMaken",
			Description: "Maakt voor een bestaande LaventeCare factuur een bunq betaalverzoek en markeert de factuur als verstuurd. Deze mutatie komt eerst in de bevestigingswachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"invoice_id": {
						"type": "string",
						"description": "UUID van de LaventeCare factuur waarvoor een bunq betaalverzoek moet worden gemaakt."
					}
				},
				"required": ["invoice_id"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareKlantMaken",
			Description: "Maakt een LaventeCare klantdossier. Technisch wordt dit als company opgeslagen; deze mutatie komt eerst in de bevestigingswachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"naam": {"type": "string"},
					"website": {"type": "string"},
					"sector": {"type": "string"},
					"status": {"type": "string", "description": "Bijv. actief, prospect, inactief."},
					"relatie_type": {"type": "string", "description": "Bijv. prospect, klant, partner, leverancier, intern of eigen_project."},
					"notities": {"type": "string"},
					"laatste_contact": {"type": "string", "description": "YYYY-MM-DD of RFC3339."},
					"volgende_actie": {"type": "string", "description": "YYYY-MM-DD."}
				},
				"required": ["naam"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareKlantBijwerken",
			Description: "Werkt een LaventeCare klantdossier bij. Technisch wordt dit als company opgeslagen; deze mutatie komt eerst in de bevestigingswachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"id": {"type": "string"},
					"naam": {"type": "string"},
					"website": {"type": "string"},
					"sector": {"type": "string"},
					"status": {"type": "string"},
					"relatie_type": {"type": "string"},
					"notities": {"type": "string"},
					"laatste_contact": {"type": "string"},
					"volgende_actie": {"type": "string"}
				},
				"required": ["id"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareContactMaken",
			Description: "Maakt een LaventeCare contactpersoon, optioneel gekoppeld aan een klantdossier/company_id. Deze mutatie komt eerst in de bevestigingswachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"company_id": {"type": "string"},
					"naam": {"type": "string"},
					"email": {"type": "string"},
					"telefoon": {"type": "string"},
					"rol": {"type": "string"},
					"is_primary": {"type": "boolean"},
					"notities": {"type": "string"}
				},
				"required": ["naam"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareOpdrachtMaken",
			Description: "Maakt een generieke LaventeCare opdracht/workstream. Gebruik dit voor kleine tussendoorprojecten, audits, integratiechecks, automatiseringen, support of advies. Deze mutatie komt eerst in de bevestigingswachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"titel": {"type": "string"},
					"type": {"type": "string", "description": "Bijv. website_platform, integratie, automatisering, ai_workflow, crm_sales, data_reporting, security_privacy, support_beheer, discovery_advies."},
					"status": {"type": "string", "description": "Default nieuw. Bijv. nieuw, intake, analyse, uitvoering, wacht_op_klant, afgerond."},
					"prioriteit": {"type": "string"},
					"company_id": {"type": "string", "description": "Bestaand klantdossier/company UUID. Gebruik dit boven klant_naam wanneer bekend."},
					"klant_naam": {"type": "string"},
					"bron": {"type": "string"},
					"source_id": {"type": "string"},
					"lead_id": {"type": "string"},
					"project_id": {"type": "string"},
					"doel": {"type": "string"},
					"scope": {"type": "string"},
					"deliverable": {"type": "string"},
					"bevindingen": {"type": "string"},
					"volgende_stap": {"type": "string"},
					"deadline": {"type": "string"},
					"geschatte_minuten": {"type": "number"},
					"waarde_indicatie": {"type": "number"},
					"stack_tags": {"type": "array", "items": {"type": "string"}, "description": "Vrije stack/systeem tags, niet hardcoded: bijv. cms, api, webhook, google-workspace, make, zapier, wordpress."},
					"tags": {"type": "array", "items": {"type": "string"}}
				},
				"required": ["titel"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareOpdrachtBijwerken",
			Description: "Werkt een LaventeCare opdracht/workstream bij. Deze mutatie komt eerst in de bevestigingswachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"id": {"type": "string"},
					"type": {"type": "string"},
					"status": {"type": "string"},
					"prioriteit": {"type": "string"},
					"company_id": {"type": "string"},
					"project_id": {"type": "string"},
					"klant_naam": {"type": "string"},
					"doel": {"type": "string"},
					"scope": {"type": "string"},
					"deliverable": {"type": "string"},
					"bevindingen": {"type": "string"},
					"volgende_stap": {"type": "string"},
					"deadline": {"type": "string"},
					"geschatte_minuten": {"type": "number"},
					"waarde_indicatie": {"type": "number"},
					"stack_tags": {"type": "array", "items": {"type": "string"}},
					"tags": {"type": "array", "items": {"type": "string"}}
				},
				"required": ["id"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareOpdrachtNaarProject",
			Description: "Promoveert een LaventeCare opdracht/workstream naar een volledig project. Deze mutatie komt eerst in de bevestigingswachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"workstream_id": {"type": "string"},
					"project_id": {"type": "string", "description": "Bestaand project UUID. Gebruik dit om de opdracht onder een bestaand project te hangen in plaats van een nieuw project te maken."},
					"naam": {"type": "string"},
					"fase": {"type": "string"},
					"status": {"type": "string"},
					"samenvatting": {"type": "string"}
				},
				"required": ["workstream_id"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareLeadMaken",
			Description: "Maakt een LaventeCare lead. Deze mutatie komt eerst in de bevestigingswachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"titel": {"type": "string"},
					"company_id": {"type": "string", "description": "Bestaande klant/company UUID."},
					"company_name": {"type": "string"},
					"website": {"type": "string"},
					"bron": {"type": "string"},
					"pijnpunt": {"type": "string"},
					"prioriteit": {"type": "string"},
					"fit_score": {"type": "number"},
					"volgende_stap": {"type": "string"},
					"volgende_actie_datum": {"type": "string"}
				},
				"required": ["titel"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareLeadBijwerken",
			Description: "Werkt een LaventeCare lead bij. Deze mutatie komt eerst in de bevestigingswachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"id": {"type": "string"},
					"company_id": {"type": "string"},
					"contact_id": {"type": "string"},
					"status": {"type": "string"},
					"fit_score": {"type": "number"},
					"pijnpunt": {"type": "string"},
					"prioriteit": {"type": "string"},
					"volgende_stap": {"type": "string"},
					"volgende_actie_datum": {"type": "string"},
					"bron": {"type": "string"}
				},
				"required": ["id"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareLeadNaarProject",
			Description: "Converteert een LaventeCare lead naar een project. Deze mutatie komt eerst in de bevestigingswachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"lead_id": {"type": "string"},
					"naam": {"type": "string"},
					"company_id": {"type": "string"},
					"company_name": {"type": "string"},
					"website": {"type": "string"},
					"fase": {"type": "string"},
					"status": {"type": "string"},
					"samenvatting": {"type": "string"}
				},
				"required": ["lead_id", "naam"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareProjectMaken",
			Description: "Maakt een LaventeCare project. Deze mutatie komt eerst in de bevestigingswachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"naam": {"type": "string"},
					"fase": {"type": "string"},
					"status": {"type": "string"},
					"waarde_indicatie": {"type": "number"},
					"start_datum": {"type": "string"},
					"deadline": {"type": "string"},
					"samenvatting": {"type": "string"}
				},
				"required": ["naam"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareProjectBijwerken",
			Description: "Werkt een LaventeCare project bij. Deze mutatie komt eerst in de bevestigingswachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"id": {"type": "string"},
					"company_id": {"type": "string"},
					"fase": {"type": "string"},
					"status": {"type": "string"},
					"waarde_indicatie": {"type": "number"},
					"start_datum": {"type": "string"},
					"deadline": {"type": "string"},
					"samenvatting": {"type": "string"}
				},
				"required": ["id"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareActieMaken",
			Description: "Maakt een LaventeCare actie-item. Deze mutatie komt eerst in de bevestigingswachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"source": {"type": "string"},
					"source_id": {"type": "string"},
					"title": {"type": "string"},
					"summary": {"type": "string"},
					"action_type": {"type": "string"},
					"priority": {"type": "string"},
					"due_date": {"type": "string"},
					"due_time": {"type": "string", "description": "Tijdstip HH:MM, optioneel naast due_date."},
					"linked_lead_id": {"type": "string"},
					"linked_project_id": {"type": "string"},
					"linked_workstream_id": {"type": "string"},
					"linked_company_id": {"type": "string"}
				},
				"required": ["title"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareActieAfronden",
			Description: "Rondt een LaventeCare actie af of wijzigt de status. Deze mutatie komt eerst in de bevestigingswachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"id": {"type": "string"},
					"status": {"type": "string", "description": "Bij leeg wordt done gebruikt."}
				},
				"required": ["id"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareBesluitMaken",
			Description: "Legt een LaventeCare besluit vast in de decision log. Deze mutatie komt eerst in de bevestigingswachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project_id": {"type": "string", "description": "Optionele project UUID."},
					"titel": {"type": "string"},
					"besluit": {"type": "string"},
					"reden": {"type": "string"},
					"impact": {"type": "string"},
					"status": {"type": "string", "description": "Default: genomen."},
					"datum": {"type": "string", "description": "YYYY-MM-DD, default vandaag."}
				},
				"required": ["titel", "besluit"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareChangeRequestMaken",
			Description: "Maakt een LaventeCare change request. Deze mutatie komt eerst in de bevestigingswachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project_id": {"type": "string", "description": "Optionele project UUID."},
					"titel": {"type": "string"},
					"impact": {"type": "string"},
					"planning_impact": {"type": "string"},
					"budget_impact": {"type": "string"},
					"status": {"type": "string", "description": "Default: nieuw."}
				},
				"required": ["titel", "impact"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "laventecareSlaIncidentMaken",
			Description: "Registreert een LaventeCare SLA-incident. Deze mutatie komt eerst in de bevestigingswachtrij.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project_id": {"type": "string", "description": "Optionele project UUID."},
					"titel": {"type": "string"},
					"prioriteit": {"type": "string", "description": "Bijv. P1, P2, P3 of P4. Default P3."},
					"status": {"type": "string", "description": "Default: open."},
					"kanaal": {"type": "string", "description": "Default: telegram."},
					"reactie_deadline": {"type": "string", "description": "Optioneel RFC3339, YYYY-MM-DD HH:MM of YYYY-MM-DD."},
					"samenvatting": {"type": "string"}
				},
				"required": ["titel"]
			}`),
		},
	},

	// ── SMART HOME ─────────────────────────────────────────────────────
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "lampBedien",
			Description: "Bedient alle smart home lampen tegelijk (aan/uit/scene). Voor losse kamers/lampen is er nog geen tool — meld dat expliciet als de gebruiker dat vraagt.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"actie": {
						"type": "string",
						"description": "aan, uit, of een scene: helder, avond, nacht, film, focus, ochtend."
					},
					"dimming": {
						"type": "number",
						"description": "Helderheid (10-100)."
					}
				},
				"required": ["actie"]
			}`),
		},
	},
}
