package server

import (
	"github.com/go-chi/chi/v5"
	httpSwagger "github.com/swaggo/http-swagger/v2"

	_ "github.com/Jeffreasy/JeffriesBackend/docs"
	"github.com/Jeffreasy/JeffriesBackend/internal/config"
	"github.com/Jeffreasy/JeffriesBackend/internal/handler"
)

// registerRoutes mounts all API routes onto the chi router.
func registerRoutes(
	r *chi.Mux,
	cfg *config.Config,
	health *handler.HealthHandler,
	rooms *handler.RoomHandler,
	devices *handler.DeviceHandler,
	bridgeH *handler.BridgeHandler,
	scenes *handler.SceneHandler,
	automations *handler.AutomationHandler,
	scheduleH *handler.ScheduleHandler,
	transactionH *handler.TransactionHandler,
	salaryH *handler.SalaryHandler,
	loonstrookH *handler.LoonstrookHandler,
	personalEventH *handler.PersonalEventHandler,
	emailH *handler.EmailHandler,
	privacyH *handler.PrivacyHandler,
	noteH *handler.NoteHandler,
	habitH *handler.HabitHandler,
	lcH *handler.LaventeCareHandler,
	settingsH *handler.SettingsHandler,
	syncH *handler.SyncHandler,
	pendingH *handler.PendingActionHandler,
	focusH *handler.FocusHandler,
) {
	authMw := apiKeyMiddleware(cfg.AppSecretKey)
	// The local LAN bridge authenticates with its OWN key, which falls back to
	// APP_SECRET_KEY when unset (config). Validating BridgeAPIKey here is what makes
	// setting a distinct BRIDGE_API_KEY correct instead of a 403 storm.
	bridgeMw := apiKeyMiddleware(cfg.BridgeAPIKey)

	r.Get("/", health.Check)
	r.Head("/", health.Check)

	r.Route("/api/v1", func(r chi.Router) {
		// Swagger Docs
		r.Get("/swagger/*", httpSwagger.Handler(
			httpSwagger.URL("/api/v1/swagger/doc.json"),
		))

		// Health
		r.Get("/health", health.Check)

		// Local LAN bridge (Render queue -> local WiZ UDP). Authenticated with the
		// bridge's own key (bridgeMw), NOT the app secret, so the trust-boundary
		// separation the docs promise actually exists.
		r.Group(func(r chi.Router) {
			r.Use(bridgeMw)
			r.Route("/bridge", func(r chi.Router) {
				r.Post("/commands/claim", bridgeH.ClaimCommands)
				r.Post("/commands/{commandID}/complete", bridgeH.CompleteCommand)
				r.Post("/devices/{deviceID}/status", bridgeH.UpdateDeviceStatus)
			})
		})

		r.Group(func(r chi.Router) {
			r.Use(authMw)

			// Rooms (PostgreSQL)
			r.Route("/rooms", func(r chi.Router) {
				r.Get("/", rooms.List)
				r.Get("/{roomID}", rooms.Get)
				r.With(authMw).Post("/", rooms.Create)
				r.With(authMw).Patch("/{roomID}", rooms.Update)
				r.With(authMw).Delete("/{roomID}", rooms.Delete)
			})

			// Devices (PostgreSQL + WiZ UDP)
			r.Route("/devices", func(r chi.Router) {
				r.Get("/", devices.List)
				r.Get("/{deviceID}", devices.Get)
				r.With(authMw).Post("/register", devices.Register)
				r.With(authMw).Post("/{deviceID}/command", devices.Command)
				r.With(authMw).Patch("/{deviceID}", devices.Update)
				r.With(authMw).Delete("/{deviceID}", devices.Delete)
			})

			// Scenes (PostgreSQL + WiZ UDP)
			r.Route("/scenes", func(r chi.Router) {
				r.Get("/", scenes.List)
				r.Get("/{sceneID}", scenes.Get)
				r.With(authMw).Post("/", scenes.Create)
				r.With(authMw).Delete("/{sceneID}", scenes.Delete)
				r.With(authMw).Post("/{sceneID}/activate", scenes.Activate)
			})

			// Automations (PostgreSQL — migrated from Convex)
			r.Route("/automations", func(r chi.Router) {
				r.Get("/", automations.List)
				r.With(authMw).Post("/", automations.Create)
				r.With(authMw).Put("/{id}", automations.Update)
				r.With(authMw).Post("/{id}/toggle", automations.Toggle)
				r.With(authMw).Delete("/{id}", automations.Delete)
				r.With(authMw).Delete("/group", automations.DeleteByGroup)
			})

			// Schedule (PostgreSQL — migrated from Convex)
			r.Route("/schedule", func(r chi.Router) {
				r.Get("/", scheduleH.List)
				r.Get("/meta", scheduleH.GetMeta)
				r.Get("/date/{date}", scheduleH.ListByDate)
				r.With(authMw).Post("/import", scheduleH.Import)
			})

			// Transactions (PostgreSQL — migrated from Convex)
			r.Route("/transactions", func(r chi.Router) {
				r.Get("/", transactionH.List)
				r.Get("/stats", transactionH.Stats)
				r.With(authMw).Post("/import", transactionH.Import)
				r.With(authMw).Patch("/{txID}", transactionH.UpdateCategorie)
			})

			// Salary (PostgreSQL — migrated from Convex)
			r.Route("/salary", func(r chi.Router) {
				r.Get("/", salaryH.List)
				r.Get("/periode", salaryH.GetByPeriode)
				r.With(authMw).Post("/", salaryH.Upsert)
			})

			// Loonstroken (PostgreSQL — migrated from Convex)
			r.Route("/loonstroken", func(r chi.Router) {
				r.Get("/", loonstrookH.List)
				r.With(authMw).Post("/import", loonstrookH.Import)
			})

			// Personal Events (PostgreSQL — migrated from Convex)
			r.Route("/personal-events", func(r chi.Router) {
				r.Get("/", personalEventH.List)
				r.Get("/upcoming", personalEventH.ListUpcoming)
				r.Get("/date/{date}", personalEventH.ListByDate)
				r.With(authMw).Post("/", personalEventH.Upsert)
				r.With(authMw).Patch("/{eventID}/status", personalEventH.UpdateStatus)
			})

			// Emails (PostgreSQL — migrated from Convex)
			r.Route("/emails", func(r chi.Router) {
				r.Get("/", emailH.List)
				r.Get("/search", emailH.Search)
				r.Get("/stats", emailH.Stats)
				r.With(authMw).Patch("/read", emailH.MarkRead)
				r.With(authMw).Patch("/delete", emailH.Delete)
			})

			// Privacy Settings (PostgreSQL — migrated from Convex)
			r.Route("/privacy", func(r chi.Router) {
				r.Get("/", privacyH.Get)
				r.With(authMw).Put("/", privacyH.Update)
			})

			// Notes (PostgreSQL — migrated from Convex)
			r.Route("/notes", func(r chi.Router) {
				r.Get("/", noteH.List)
				r.Get("/search", noteH.Search)
				r.Get("/tags", noteH.Tags)
				r.Get("/{id}", noteH.Get)
				r.Get("/{id}/backlinks", noteH.Backlinks)
				r.Get("/{id}/revisions", noteH.Revisions)
				r.With(authMw).Post("/", noteH.Create)
				r.With(authMw).Patch("/{id}", noteH.Update)
				r.With(authMw).Post("/{id}/revisions/{revisionID}/restore", noteH.RestoreRevision)
				r.With(authMw).Delete("/{id}", noteH.Delete)
			})

			// Habits (PostgreSQL — migrated from Convex)
			r.Route("/habits", func(r chi.Router) {
				r.Get("/", habitH.List)
				r.Get("/for-date", habitH.ForDate)
				r.Get("/stats", habitH.Stats)
				r.Get("/heatmap", habitH.Heatmap)
				r.Get("/badges", habitH.Badges)
				r.Get("/{id}", habitH.Get)
				r.With(authMw).Post("/", habitH.Create)
				r.With(authMw).Patch("/{id}", habitH.Update)
				r.With(authMw).Post("/{id}/toggle", habitH.Toggle)
				r.With(authMw).Post("/{id}/incident", habitH.Incident)
				r.With(authMw).Post("/{id}/pause", habitH.TogglePause)
				r.With(authMw).Post("/{id}/archive", habitH.Archive)
				r.With(authMw).Post("/reorder", habitH.Reorder)
				r.With(authMw).Delete("/{id}", habitH.Delete)
			})

			// LaventeCare CRM (PostgreSQL — migrated from Convex)
			r.Route("/laventecare", func(r chi.Router) {
				r.Get("/cockpit", lcH.Cockpit)
				r.Get("/billing", lcH.Billing)
				r.Get("/mailbox", lcH.Mailbox)
				r.With(authMw).Post("/mailbox/templates", lcH.CreateMailTemplate)
				r.With(authMw).Patch("/mailbox/templates/{id}", lcH.UpdateMailTemplate)
				r.With(authMw).Post("/mailbox/ai-suggest", lcH.SuggestMailContent)
				r.With(authMw).Post("/mailbox/send-template", lcH.SendTemplatedMail)
				r.Get("/companies", lcH.ListCompanies)
				r.With(authMw).Post("/companies", lcH.CreateCompany)
				r.With(authMw).Patch("/companies/{id}", lcH.UpdateCompany)
				r.With(authMw).Delete("/companies/{id}", lcH.DeleteCompany)
				r.Get("/contacts", lcH.ListContacts)
				r.With(authMw).Post("/contacts", lcH.CreateContact)
				r.With(authMw).Patch("/contacts/{id}", lcH.UpdateContact)
				r.Get("/access-credentials", lcH.ListAccessCredentials)
				r.With(authMw).Post("/access-credentials", lcH.CreateAccessCredential)
				r.With(authMw).Patch("/access-credentials/{id}", lcH.UpdateAccessCredential)
				r.With(authMw).Post("/quotes", lcH.CreateQuote)
				r.With(authMw).Patch("/quotes/{id}/status", lcH.UpdateQuoteStatus)
				r.With(authMw).Post("/quotes/{id}/invoice", lcH.CreateInvoiceFromQuote)
				r.With(authMw).Post("/time-entries", lcH.CreateTimeEntry)
				r.With(authMw).Post("/invoices", lcH.CreateInvoice)
				r.Get("/invoices/{id}/document", lcH.GetInvoiceDocument)
				r.With(authMw).Patch("/invoices/{id}/status", lcH.UpdateInvoiceStatus)
				r.With(authMw).Post("/invoices/{id}/payment-request", lcH.CreateInvoicePaymentRequestAction)
				r.With(authMw).Post("/invoices/{id}/payment-refresh", lcH.RefreshInvoicePaymentStatus)
				r.Get("/documents", lcH.ListDocuments)
				r.Get("/dossier-documents", lcH.ListDossierDocuments)
				r.Get("/ai/dossier-advice", lcH.DossierAdvice)
				r.With(authMw).Post("/dossier-documents", lcH.CreateDossierDocument)
				r.Get("/activity", lcH.ListActivityEvents)
				r.With(authMw).Post("/activity", lcH.CreateActivityEvent)
				r.Get("/decisions", lcH.ListDecisions)
				r.With(authMw).Post("/decisions", lcH.CreateDecision)
				r.With(authMw).Patch("/decisions/{id}/status", lcH.UpdateDecisionStatus)
				r.Get("/changes", lcH.ListChangeRequests)
				r.With(authMw).Post("/changes", lcH.CreateChangeRequest)
				r.With(authMw).Patch("/changes/{id}/status", lcH.UpdateChangeRequestStatus)
				r.Get("/sla-incidents", lcH.ListSlaIncidents)
				r.With(authMw).Post("/sla-incidents", lcH.CreateSlaIncident)
				r.With(authMw).Patch("/sla-incidents/{id}/status", lcH.UpdateSlaIncidentStatus)
				r.Get("/leads", lcH.ListLeads)
				r.With(authMw).Post("/leads", lcH.CreateLead)
				r.With(authMw).Patch("/leads/{id}", lcH.UpdateLead)
				r.With(authMw).Post("/leads/{id}/convert", lcH.ConvertLeadToProject)
				r.Get("/projects", lcH.ListProjects)
				r.With(authMw).Post("/projects", lcH.CreateProject)
				r.With(authMw).Patch("/projects/{id}", lcH.UpdateProject)
				r.Get("/workstreams", lcH.ListWorkstreams)
				r.With(authMw).Post("/workstreams", lcH.CreateWorkstream)
				r.With(authMw).Patch("/workstreams/{id}", lcH.UpdateWorkstream)
				r.With(authMw).Post("/workstreams/{id}/convert-project", lcH.ConvertWorkstreamToProject)
				r.Get("/actions", lcH.ListActions)
				r.With(authMw).Post("/actions", lcH.CreateAction)
				r.With(authMw).Patch("/actions/{id}/status", lcH.UpdateActionStatus)
				r.With(authMw).Post("/signals/convert-lead", lcH.ConvertSignalToLead)
				r.With(authMw).Post("/documents/seed", lcH.SeedDocuments)
			})

			// Settings
			r.Route("/settings", func(r chi.Router) {
				r.Get("/overview", settingsH.Overview)
				r.Get("/backup", settingsH.Backup)
				r.Get("/telegram/status", settingsH.TelegramStatus)
				r.Get("/ai/diagnostics", settingsH.AIDiagnostics)
				r.Get("/bunq/introspect", settingsH.BunqIntrospect)
			})

			// AI confirmation queue
			r.Route("/ai/pending", func(r chi.Router) {
				r.Get("/", pendingH.List)
				r.With(authMw).Post("/{id}/confirm", pendingH.Confirm)
				r.With(authMw).Post("/{id}/cancel", pendingH.Cancel)
			})

			// Focus cockpit
			r.Route("/focus", func(r chi.Router) {
				r.Get("/summary", focusH.Summary)
			})

			// Sync
			r.Route("/sync", func(r chi.Router) {
				r.Get("/status", syncH.GetSyncStatus)
				r.With(authMw).Post("/calendar", syncH.SyncCalendar)
				r.With(authMw).Post("/gmail", syncH.SyncGmail)
			})
		})
	})
}
