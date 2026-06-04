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
			Description: "Haalt de gecombineerde planning op: werkdiensten plus persoonlijke agenda-afspraken voor een dag of periode.",
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
					"symbol": {"type": "string", "description": "Optioneel UI-symbool."}
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
					"symbol": {"type": "string"}
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
			Description: "Vinkt een habit af of zet meetbare voortgang voor een datum. Naam mag gebruikt worden als ID ontbreekt.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"id": {"type": "string", "description": "Habit UUID."},
					"habitId": {"type": "string", "description": "Habit UUID alternatief."},
					"naam": {"type": "string", "description": "Habitnaam als ID onbekend is."},
					"datum": {"type": "string", "description": "YYYY-MM-DD, standaard vandaag."},
					"waarde": {"type": "number", "description": "Meetwaarde voor kwantitatieve habits."},
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
			Name:        "laventecareLeadsOpvragen",
			Description: "Haalt recente LaventeCare leads op.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"limit": {
						"type": "number",
						"description": "Aantal leads (max 30)."
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
			Description: "Haalt recente LaventeCare projecten op.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"limit": {
						"type": "number",
						"description": "Aantal projecten (max 30)."
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
			Description: "Haalt open LaventeCare actie-items op.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"limit": {
						"type": "number",
						"description": "Aantal acties (max 30)."
					}
				},
				"required": []
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
					"status": {"type": "string"},
					"fit_score": {"type": "number"},
					"pijnpunt": {"type": "string"},
					"prioriteit": {"type": "string"},
					"volgende_stap": {"type": "string"},
					"volgende_actie_datum": {"type": "string"}
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
					"linked_lead_id": {"type": "string"},
					"linked_project_id": {"type": "string"}
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

	// ── SMART HOME ─────────────────────────────────────────────────────
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "lampBedien",
			Description: "Bedient de smart home lampen.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"actie": {
						"type": "string",
						"description": "aan, uit, of een scene (bijv. ocean, romance)."
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
