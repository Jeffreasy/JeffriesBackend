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
			Description: "Haalt het dienstrooster op voor een specifieke periode.",
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
				"required": ["startIso", "eindIso"]
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
			Description: "Haalt een samenvatting of details van de loonstroken op.",
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
				"required": ["jaar"]
			}`),
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "saldoOpvragen",
			Description: "Haalt actuele totaalsaldo en rekeningbalansen op.",
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
			Description: "Zoekt in de financiële transacties.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"query": {
						"type": "string",
						"description": "Omschrijving of tegenrekening."
					},
					"limit": {
						"type": "number",
						"description": "Aantal resultaten (max 20)."
					}
				},
				"required": ["query"]
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
			Description: "Haalt Google Calendar afspraken op.",
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
				"required": ["startIso", "eindIso"]
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
			Description: "Haalt alle notities op die vandaag zijn aangemaakt of gewijzigd.",
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
