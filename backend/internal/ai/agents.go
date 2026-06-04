package ai

// Agent defines a specialist AI agent within the Jeffries HomeBot.
type Agent struct {
	ID           string   `json:"id"`
	Naam         string   `json:"naam"`
	Emoji        string   `json:"emoji"`
	Beschrijving string   `json:"beschrijving"`
	Capabilities []string `json:"capabilities"`
}

// ToolPolicy governs access and behavior per tool.
type ToolPolicy struct {
	Agents               []string
	Mutates              bool
	RequiresConfirmation bool
}

// Registry holds all available agents.
var Registry = []Agent{
	{ID: "brain", Naam: "Jeffries Brain", Emoji: "🧠", Beschrijving: "Centrale assistent — combineert alle domeinen: planning, welzijn, geld, email, notities, lampen, habits, LaventeCare en systeemstatus.", Capabilities: []string{"Cross-domain dagbriefing", "Planning en agenda", "Email management", "Finance inzicht", "Smart home bediening", "LaventeCare CRM", "Notities en habits"}},
	{ID: "dashboard", Naam: "Dashboard", Emoji: "📊", Beschrijving: "Compact dashboard snapshot met totalen uit alle domeinen.", Capabilities: []string{"Quick status overview"}},
	{ID: "lampen", Naam: "Lamp Control", Emoji: "💡", Beschrijving: "Smart home lamp bediening — scenes, helderheid, kleuren.", Capabilities: []string{"Lampen aan/uit", "Scenes activeren", "Helderheid/kleur instellen"}},
	{ID: "rooster", Naam: "Rooster", Emoji: "📅", Beschrijving: "Werkrooster, diensten en ORT berekeningen.", Capabilities: []string{"Diensten opvragen", "Salaris berekenen", "Rooster analyse"}},
	{ID: "agenda", Naam: "Agenda", Emoji: "🗓️", Beschrijving: "Google Calendar afspraken beheren.", Capabilities: []string{"Afspraken maken", "Afspraken bewerken", "Afspraken zoeken"}},
	{ID: "finance", Naam: "Finance", Emoji: "💰", Beschrijving: "Salaris, transacties, uitgaven en budget analyse.", Capabilities: []string{"Saldo opvragen", "Transacties zoeken", "Uitgaven overzicht", "Categoriseren"}},
	{ID: "automations", Naam: "Automations", Emoji: "⚙️", Beschrijving: "Systeem status en automations overzicht.", Capabilities: []string{"Sync health", "Automation status"}},
	{ID: "email", Naam: "Email", Emoji: "📧", Beschrijving: "Gmail inbox beheren — lezen, zoeken, verwijderen, versturen.", Capabilities: []string{"Email lezen", "Email zoeken", "Email versturen", "Inbox opruimen"}},
	{ID: "notes", Naam: "Notities", Emoji: "📝", Beschrijving: "Dagelijks journal en knowledge base — notities aanmaken, zoeken, pinnen en archiveren.", Capabilities: []string{"Dagnotities", "Notitie maken", "Notities zoeken", "Weekoverzicht", "Pinnen", "Archiveren"}},
	{ID: "habits", Naam: "Habits", Emoji: "🎯", Beschrijving: "Habits volgen, streaks, badges en rapportage.", Capabilities: []string{"Habit voltooien", "Streaks bekijken", "Badges", "Rapport"}},
	{ID: "laventecare", Naam: "LaventeCare", Emoji: "🏢", Beschrijving: "LaventeCare CRM — leads, projecten, acties, kennis en SLA.", Capabilities: []string{"Cockpit", "Kennis zoeken", "Leads beheren", "Projecten beheren", "Acties beheren"}},
}

// GetAgent returns the agent by ID or nil.
func GetAgent(id string) *Agent {
	for i := range Registry {
		if Registry[i].ID == id {
			return &Registry[i]
		}
	}
	return nil
}

// Policies governs which agent can use which tool and confirmation requirements.
var Policies = map[string]ToolPolicy{
	// Email reads
	"leesEmail":  {Agents: []string{"email", "dashboard", "brain"}, Mutates: false, RequiresConfirmation: false},
	"zoekEmails": {Agents: []string{"email", "dashboard", "brain"}, Mutates: false, RequiresConfirmation: false},
	// System reads
	"syncStatusOpvragen":   {Agents: []string{"automations", "dashboard", "brain"}, Mutates: false, RequiresConfirmation: false},
	"automationsOverzicht": {Agents: []string{"automations", "dashboard", "brain"}, Mutates: false, RequiresConfirmation: false},
	// Email writes
	"markeerGelezen":     {Agents: []string{"email", "brain"}, Mutates: true, RequiresConfirmation: true},
	"verwijderEmail":     {Agents: []string{"email", "brain"}, Mutates: true, RequiresConfirmation: true},
	"markeerSter":        {Agents: []string{"email", "brain"}, Mutates: true, RequiresConfirmation: true},
	"emailVersturen":     {Agents: []string{"email", "brain"}, Mutates: true, RequiresConfirmation: true},
	"emailBeantwoorden":  {Agents: []string{"email", "brain"}, Mutates: true, RequiresConfirmation: true},
	"bulkMarkeerGelezen": {Agents: []string{"email", "brain"}, Mutates: true, RequiresConfirmation: true},
	"bulkVerwijder":      {Agents: []string{"email", "brain"}, Mutates: true, RequiresConfirmation: true},
	"inboxOpruimen":      {Agents: []string{"email", "brain"}, Mutates: true, RequiresConfirmation: true},
	// Smart home
	"lampBedien": {Agents: []string{"lampen", "brain"}, Mutates: true, RequiresConfirmation: false},
	// Schedule reads
	"planningOpvragen":        {Agents: []string{"agenda", "rooster", "dashboard", "brain"}, Mutates: false, RequiresConfirmation: false},
	"dienstenOpvragen":        {Agents: []string{"agenda", "rooster", "finance", "dashboard", "brain"}, Mutates: false, RequiresConfirmation: false},
	"contractAnalyseOpvragen": {Agents: []string{"rooster", "dashboard", "brain"}, Mutates: false, RequiresConfirmation: false},
	"salarisOpvragen":         {Agents: []string{"finance", "dashboard", "brain"}, Mutates: false, RequiresConfirmation: false},
	// Finance reads
	"saldoOpvragen":      {Agents: []string{"finance", "dashboard", "brain"}, Mutates: false, RequiresConfirmation: false},
	"transactiesZoeken":  {Agents: []string{"finance", "dashboard", "brain"}, Mutates: false, RequiresConfirmation: false},
	"uitgavenOverzicht":  {Agents: []string{"finance", "dashboard", "brain"}, Mutates: false, RequiresConfirmation: false},
	"maandVergelijken":   {Agents: []string{"finance", "dashboard", "brain"}, Mutates: false, RequiresConfirmation: false},
	"vasteLastenAnalyse": {Agents: []string{"finance", "dashboard", "brain"}, Mutates: false, RequiresConfirmation: false},
	"ongelabeldAnalyse":  {Agents: []string{"finance", "dashboard", "brain"}, Mutates: false, RequiresConfirmation: false},
	// Finance writes
	"categorieWijzigen": {Agents: []string{"finance", "brain"}, Mutates: true, RequiresConfirmation: true},
	"bulkCategoriseren": {Agents: []string{"finance", "brain"}, Mutates: true, RequiresConfirmation: true},
	// Calendar
	"afspraakMaken":       {Agents: []string{"agenda", "rooster", "brain"}, Mutates: true, RequiresConfirmation: true},
	"afspraakBewerken":    {Agents: []string{"agenda", "rooster", "brain"}, Mutates: true, RequiresConfirmation: true},
	"afspraakVerwijderen": {Agents: []string{"agenda", "rooster", "brain"}, Mutates: true, RequiresConfirmation: true},
	"afsprakenOpvragen":   {Agents: []string{"agenda", "rooster", "dashboard", "brain"}, Mutates: false, RequiresConfirmation: false},
	// Notes
	"notitieAanmaken":       {Agents: []string{"notes", "brain"}, Mutates: true, RequiresConfirmation: false},
	"notitiesZoeken":        {Agents: []string{"notes", "dashboard", "brain"}, Mutates: false, RequiresConfirmation: false},
	"notitiesOverzicht":     {Agents: []string{"notes", "dashboard", "brain"}, Mutates: false, RequiresConfirmation: false},
	"notitiePinnen":         {Agents: []string{"notes", "brain"}, Mutates: true, RequiresConfirmation: false},
	"notitieBewerken":       {Agents: []string{"notes", "brain"}, Mutates: true, RequiresConfirmation: true},
	"notitieArchiveren":     {Agents: []string{"notes", "brain"}, Mutates: true, RequiresConfirmation: true},
	"notitiesVandaag":       {Agents: []string{"notes", "dashboard", "brain"}, Mutates: false, RequiresConfirmation: false},
	"bulkArchiveerNotities": {Agents: []string{"notes", "brain"}, Mutates: true, RequiresConfirmation: true},
	// Habits
	"habitAanmaken":   {Agents: []string{"habits", "brain"}, Mutates: true, RequiresConfirmation: false},
	"habitVoltooien":  {Agents: []string{"habits", "brain"}, Mutates: true, RequiresConfirmation: false},
	"habitIncident":   {Agents: []string{"habits", "brain"}, Mutates: true, RequiresConfirmation: true},
	"habitsOverzicht": {Agents: []string{"habits", "dashboard", "brain"}, Mutates: false, RequiresConfirmation: false},
	"habitStreaks":    {Agents: []string{"habits", "dashboard", "brain"}, Mutates: false, RequiresConfirmation: false},
	"habitBadges":     {Agents: []string{"habits", "dashboard", "brain"}, Mutates: false, RequiresConfirmation: false},
	"habitRapport":    {Agents: []string{"habits", "dashboard", "brain"}, Mutates: false, RequiresConfirmation: false},
	"habitNotitie":    {Agents: []string{"habits", "brain"}, Mutates: true, RequiresConfirmation: false},
	// LaventeCare reads
	"laventecareCockpit":           {Agents: []string{"laventecare", "dashboard", "brain"}, Mutates: false, RequiresConfirmation: false},
	"laventecareKennisZoeken":      {Agents: []string{"laventecare", "dashboard", "brain"}, Mutates: false, RequiresConfirmation: false},
	"laventecareLeadsOpvragen":     {Agents: []string{"laventecare", "dashboard", "brain"}, Mutates: false, RequiresConfirmation: false},
	"laventecareProjectenOpvragen": {Agents: []string{"laventecare", "dashboard", "brain"}, Mutates: false, RequiresConfirmation: false},
	"laventecareActiesOpvragen":    {Agents: []string{"laventecare", "dashboard", "brain"}, Mutates: false, RequiresConfirmation: false},
	// LaventeCare writes
	"laventecareLeadMaken":          {Agents: []string{"laventecare", "brain"}, Mutates: true, RequiresConfirmation: true},
	"laventecareLeadBijwerken":      {Agents: []string{"laventecare", "brain"}, Mutates: true, RequiresConfirmation: true},
	"laventecareLeadNaarProject":    {Agents: []string{"laventecare", "brain"}, Mutates: true, RequiresConfirmation: true},
	"laventecareProjectMaken":       {Agents: []string{"laventecare", "brain"}, Mutates: true, RequiresConfirmation: true},
	"laventecareProjectBijwerken":   {Agents: []string{"laventecare", "brain"}, Mutates: true, RequiresConfirmation: true},
	"laventecareActieMaken":         {Agents: []string{"laventecare", "brain"}, Mutates: true, RequiresConfirmation: true},
	"laventecareActieAfronden":      {Agents: []string{"laventecare", "brain"}, Mutates: true, RequiresConfirmation: true},
	"laventecareBesluitMaken":       {Agents: []string{"laventecare", "brain"}, Mutates: true, RequiresConfirmation: true},
	"laventecareChangeRequestMaken": {Agents: []string{"laventecare", "brain"}, Mutates: true, RequiresConfirmation: true},
	"laventecareSlaIncidentMaken":   {Agents: []string{"laventecare", "brain"}, Mutates: true, RequiresConfirmation: true},
}

// IsToolAllowed checks if the given agent may use the tool.
func IsToolAllowed(agentID, toolName string) bool {
	p, ok := Policies[toolName]
	if !ok {
		return false
	}
	for _, a := range p.Agents {
		if a == agentID {
			return true
		}
	}
	return false
}

// IsMutatingTool returns true for tools that change state.
func IsMutatingTool(toolName string) bool {
	p, ok := Policies[toolName]
	return ok && p.Mutates
}

// RequiresConfirmation returns true for tools that need user approval.
func RequiresConfirmation(toolName string) bool {
	p, ok := Policies[toolName]
	if !ok {
		return true // default safe
	}
	return p.RequiresConfirmation
}

// GetToolsForAgent returns tool definitions filtered by agent access.
func GetToolsForAgent(agentID string, allTools []ToolDefinition) []ToolDefinition {
	var result []ToolDefinition
	for _, t := range allTools {
		if IsToolAllowed(agentID, t.Function.Name) {
			result = append(result, t)
		}
	}
	return result
}
