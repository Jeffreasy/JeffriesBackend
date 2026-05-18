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
			Name:        "habitsOverzicht",
			Description: "Haalt een overzicht van actieve habits op.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {},
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
