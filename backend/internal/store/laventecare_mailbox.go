package store

import (
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

// GetMailbox returns the LaventeCare outbound mailbox workspace.
func (s *LaventeCareStore) GetMailbox(ctx context.Context, userID string, limit int, configured bool, senderEmail string) (*model.LCMailbox, error) {
	if err := s.SeedDefaultMailTemplates(ctx, userID); err != nil {
		return nil, err
	}
	templates, err := s.ListMailTemplates(ctx, userID, 80)
	if err != nil {
		return nil, err
	}
	outbox, err := s.ListMailOutbox(ctx, userID, limit)
	if err != nil {
		return nil, err
	}
	summary, err := s.GetMailboxSummary(ctx, userID, templates, configured, senderEmail)
	if err != nil {
		return nil, err
	}
	// Serialize empty collections as [] instead of null — the frontend .map's
	// over these directly (M3).
	if templates == nil {
		templates = []model.LCMailTemplate{}
	}
	if outbox == nil {
		outbox = []model.LCMailOutboxItem{}
	}
	return &model.LCMailbox{
		Summary:   summary,
		Templates: templates,
		Outbox:    outbox,
		Inbox:     []model.LCMailInboxItem{},
	}, nil
}

func (s *LaventeCareStore) BuildMailAIContext(ctx context.Context, userID string, input model.LCMailAISuggestionRequest) (*model.LCMailAIContext, error) {
	template, err := s.GetMailTemplate(ctx, userID, input.TemplateID)
	if err != nil {
		return nil, err
	}

	var company *model.LCCompany
	var contact *model.LCContact
	companyID := input.CompanyID
	contactID := input.ContactID

	if contactID != nil {
		contact, err = s.GetContact(ctx, userID, *contactID)
		if err != nil {
			return nil, err
		}
		if companyID == nil && contact.CompanyID != nil {
			companyID = contact.CompanyID
		}
	}
	if companyID != nil {
		company, err = s.GetCompany(ctx, userID, *companyID)
		if err != nil {
			return nil, err
		}
	}

	project, projectCompanyID, err := s.mailAIProject(ctx, userID, input.ProjectID)
	if err != nil {
		return nil, err
	}
	if companyID == nil && projectCompanyID != nil {
		companyID = projectCompanyID
		company, _ = s.GetCompany(ctx, userID, *companyID)
	}

	workstream, workstreamCompanyID, workstreamProjectID, err := s.mailAIWorkstream(ctx, userID, input.WorkstreamID)
	if err != nil {
		return nil, err
	}
	if companyID == nil && workstreamCompanyID != nil {
		companyID = workstreamCompanyID
		company, _ = s.GetCompany(ctx, userID, *companyID)
	}
	if input.ProjectID == nil && workstreamProjectID != nil {
		input.ProjectID = workstreamProjectID
	}
	if project == nil && input.ProjectID != nil {
		project, projectCompanyID, err = s.mailAIProject(ctx, userID, input.ProjectID)
		if err != nil {
			return nil, err
		}
		if companyID == nil && projectCompanyID != nil {
			companyID = projectCompanyID
			company, _ = s.GetCompany(ctx, userID, *companyID)
		}
	}

	quote, quoteCompanyID, err := s.mailAIQuote(ctx, userID, input.QuoteID)
	if err != nil {
		return nil, err
	}
	if companyID == nil && quoteCompanyID != nil {
		companyID = quoteCompanyID
		company, _ = s.GetCompany(ctx, userID, *companyID)
	}

	invoice, invoiceCompanyID, err := s.mailAIInvoice(ctx, userID, input.InvoiceID)
	if err != nil {
		return nil, err
	}
	if companyID == nil && invoiceCompanyID != nil {
		companyID = invoiceCompanyID
		company, _ = s.GetCompany(ctx, userID, *companyID)
	}
	if invoice != nil {
		if input.ProjectID == nil {
			input.ProjectID = uuidPtrFromMapValue(invoice, "project_id")
		}
		if input.WorkstreamID == nil {
			input.WorkstreamID = uuidPtrFromMapValue(invoice, "workstream_id")
		}
	}
	if project == nil && input.ProjectID != nil {
		project, projectCompanyID, err = s.mailAIProject(ctx, userID, input.ProjectID)
		if err != nil {
			return nil, err
		}
		if companyID == nil && projectCompanyID != nil {
			companyID = projectCompanyID
			company, _ = s.GetCompany(ctx, userID, *companyID)
		}
	}
	if workstream == nil && input.WorkstreamID != nil {
		workstream, workstreamCompanyID, workstreamProjectID, err = s.mailAIWorkstream(ctx, userID, input.WorkstreamID)
		if err != nil {
			return nil, err
		}
		if companyID == nil && workstreamCompanyID != nil {
			companyID = workstreamCompanyID
			company, _ = s.GetCompany(ctx, userID, *companyID)
		}
		if input.ProjectID == nil && workstreamProjectID != nil {
			input.ProjectID = workstreamProjectID
		}
	}
	if project == nil && input.ProjectID != nil {
		project, projectCompanyID, err = s.mailAIProject(ctx, userID, input.ProjectID)
		if err != nil {
			return nil, err
		}
		if companyID == nil && projectCompanyID != nil {
			companyID = projectCompanyID
			company, _ = s.GetCompany(ctx, userID, *companyID)
		}
	}

	ids, keywords := mailAIContextKeys(company, contact, project, workstream)
	notes, err := s.mailAINotes(ctx, userID, ids, keywords)
	if err != nil {
		return nil, err
	}
	agenda, err := s.mailAIAgenda(ctx, userID, ids, keywords)
	if err != nil {
		return nil, err
	}
	schedule, err := s.mailAISchedule(ctx, userID)
	if err != nil {
		return nil, err
	}
	actions, err := s.mailAIActions(ctx, userID, companyID, input.ProjectID, input.WorkstreamID)
	if err != nil {
		return nil, err
	}
	activity, err := s.mailAIActivity(ctx, userID, companyID, input.ProjectID, input.WorkstreamID)
	if err != nil {
		return nil, err
	}
	billing, err := s.mailAIBilling(ctx, userID, companyID, input.ProjectID, input.WorkstreamID)
	if err != nil {
		return nil, err
	}
	dossier, err := s.mailAIDossier(ctx, userID, companyID, input.ProjectID, input.WorkstreamID)
	if err != nil {
		return nil, err
	}

	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		loc = time.UTC
	}
	return &model.LCMailAIContext{
		Template:     template,
		Company:      company,
		Contact:      contact,
		Project:      project,
		Workstream:   workstream,
		Quote:        quote,
		Invoice:      invoice,
		Notes:        notes,
		Agenda:       agenda,
		Schedule:     schedule,
		Actions:      actions,
		Activity:     activity,
		Billing:      billing,
		Dossier:      dossier,
		Attachments:  sanitizeMailAIAttachments(input.Attachments),
		ExistingVars: safeStringMap(input.Variables),
		Today:        time.Now().In(loc).Format("2006-01-02"),
	}, nil
}

func (s *LaventeCareStore) GetMailboxSummary(ctx context.Context, userID string, templates []model.LCMailTemplate, configured bool, senderEmail string) (model.LCMailboxSummary, error) {
	var activeTemplates int
	for _, template := range templates {
		if template.Status == "active" {
			activeTemplates++
		}
	}

	var totalOutbox, drafts, sent, failed int
	err := s.db.Pool.QueryRow(ctx,
		`SELECT
		    COUNT(*)::int,
		    COUNT(*) FILTER (WHERE status = 'concept')::int,
		    COUNT(*) FILTER (WHERE status = 'sent')::int,
		    COUNT(*) FILTER (WHERE status = 'failed')::int
		   FROM lc_mail_outbox
		  WHERE user_id = $1`,
		userID,
	).Scan(&totalOutbox, &drafts, &sent, &failed)
	if err != nil {
		return model.LCMailboxSummary{}, err
	}

	nextStep := "Maak een templateconcept of verstuur de eerste klantmail."
	if !configured {
		nextStep = "Vul Microsoft Graph env in en zet LAVENTECARE_MAIL_ENABLED=true."
	} else if sent > 0 {
		nextStep = "Mailbox is klaar. Gebruik templates per klant, offerte of factuur."
	} else if drafts > 0 {
		nextStep = "Controleer concepten en verstuur de eerste LaventeCare-mail."
	}

	return model.LCMailboxSummary{
		Templates:       len(templates),
		ActiveTemplates: activeTemplates,
		Outbox:          totalOutbox,
		Drafts:          drafts,
		Sent:            sent,
		Failed:          failed,
		Provider:        "microsoft_graph",
		SenderEmail:     senderEmail,
		Configured:      configured,
		NextStep:        nextStep,
	}, nil
}

func (s *LaventeCareStore) SeedDefaultMailTemplates(ctx context.Context, userID string) error {
	now := time.Now().UTC()
	defaults := []model.LCMailTemplateCreate{
		{
			TemplateKey:     "intake_followup",
			Name:            "Intake opvolging",
			Category:        "sales",
			Status:          "active",
			SubjectTemplate: "Vervolg intake - {{company.naam}}",
			BodyHTML: brandedMailHTML(mailTemplateContent{
				Preheader:   "Ik heb de intakepunten vastgelegd en stel een praktische vervolgstap voor.",
				Eyebrow:     "Sales intake",
				Title:       "Vervolg op onze intake",
				Greeting:    "Beste {{contact.naam}},",
				Intro:       "Dank voor je bericht en de context rond {{company.naam}}. Ik heb de belangrijkste punten vastgelegd en kijk graag met je mee naar de meest praktische vervolgstap.",
				Body:        "Mijn aanpak is om snel helder te maken waar de grootste winst zit, wat er belangrijk voor is en welke stap het meeste oplevert zonder het onnodig ingewikkeld te maken.",
				FocusTitle:  "Voorstel voor vervolg",
				FocusItems:  []string{"{{next_step}}", "Ik koppel dit aan de juiste klantcontext in LaventeCare.", "Na akkoord werk ik de eerste afspraak of planning concreet uit."},
				CTAURL:      "{{cta.url}}",
				CTALabel:    "{{cta.label}}",
				ClosingLine: "Als dit past, pak ik het gericht verder op.",
			}),
			BodyText: mailStrPtr("Beste {{contact.naam}},\n\nDank voor je bericht en de context rond {{company.naam}}. Ik heb de belangrijkste punten vastgelegd en kijk graag mee naar de meest praktische vervolgstap.\n\nVoorstel voor vervolg:\n- {{next_step}}\n- Ik koppel dit aan de juiste klantcontext in LaventeCare.\n- Na akkoord werk ik de eerste afspraak of planning concreet uit.\n\nAls dit past, pak ik het gericht verder op.\n\nMet vriendelijke groet,\nJeffrey Lavente\nLaventeCare"),
		},
		{
			TemplateKey:     "discovery_scope",
			Name:            "Discovery en scope",
			Category:        "sales",
			Status:          "active",
			SubjectTemplate: "Scope voorstel - {{company.naam}}",
			BodyHTML: brandedMailHTML(mailTemplateContent{
				Preheader:   "Een korte afspraak om richting, risico's en eerste oplevering scherp te krijgen.",
				Eyebrow:     "Discovery",
				Title:       "Scope en eerste werkpakket",
				Greeting:    "Beste {{contact.naam}},",
				Intro:       "Op basis van onze context stel ik voor om voor {{company.naam}} te starten met een korte discovery/scope. Dat is een eerste onderzoek waarin we het gewenste resultaat, wat er allemaal bij komt kijken en de prioriteiten helder maken voordat er gebouwd wordt.",
				Body:        "Zo voorkomen we losse keuzes op het moment zelf en hebben we een goed en betrouwbaar startpunt voor planning, budget en uitvoering.",
				FocusTitle:  "Wat ik oplever",
				FocusItems:  []string{"Probleem en doel scherp op papier.", "In beeld wat er technisch en in de praktijk bij komt kijken.", "Een concreet voorstel voor {{next_step}}."},
				CTAURL:      "{{cta.url}}",
				CTALabel:    "{{cta.label}}",
				ClosingLine: "Ik kan dit na akkoord direct voorbereiden.",
			}),
			BodyText: mailStrPtr("Beste {{contact.naam}},\n\nOp basis van onze context stel ik voor om voor {{company.naam}} te starten met een korte discovery/scope. Dat is een eerste onderzoek waarin we het gewenste resultaat, wat er allemaal bij komt kijken en de prioriteiten helder maken voordat er gebouwd wordt.\n\nWat ik oplever:\n- Probleem en doel scherp op papier.\n- In beeld wat er technisch en in de praktijk bij komt kijken.\n- Een concreet voorstel voor {{next_step}}.\n\nIk kan dit na akkoord direct voorbereiden.\n\nMet vriendelijke groet,\nJeffrey Lavente\nLaventeCare"),
		},
		{
			TemplateKey:     "proposal_qa",
			Name:            "Voorstel en Q&A",
			Category:        "sales",
			Status:          "active",
			SubjectTemplate: "Antwoorden en voorstel - {{company.naam}}",
			BodyHTML: brandedMailHTML(mailTemplateContent{
				Preheader:   "Kort antwoord op afspraken, werking, kosten, eigendom, AVG en de stappen.",
				Eyebrow:     "Voorstel",
				Title:       "Antwoorden en aanpak",
				Greeting:    "Beste {{contact.naam}},",
				Intro:       "Dank voor jullie vragen over {{company.naam}}. Hieronder zet ik kort op een rij wat we afspreken, wat er al staat en hoe ik dit stap voor stap zou aanpakken.",
				Body:        "{{proposal.current_state}}",
				FocusTitle:  "Kernantwoorden",
				FocusItems:  []string{"Afspraak: {{proposal.scope}}", "Waarde: {{proposal.value}}", "AI en matching: {{proposal.ai}}", "Veiligheid en eigendom: {{proposal.security}}", "Kosten en stappen: {{proposal.costs}}", "Volgende stap: {{proposal.next_step}}"},
				CTAURL:      "{{cta.url}}",
				CTALabel:    "{{cta.label}}",
				ClosingLine: "Ik kan dit in een demo laten zien en daarna de afspraak definitief maken, zodat jullie precies weten wat er in de eerste stap zit.",
			}),
			BodyText: mailStrPtr("Beste {{contact.naam}},\n\nDank voor jullie vragen over {{company.naam}}. Hieronder zet ik kort op een rij wat we afspreken, wat er al staat en hoe ik dit stap voor stap zou aanpakken.\n\n{{proposal.current_state}}\n\nKernantwoorden:\n- Afspraak: {{proposal.scope}}\n- Waarde: {{proposal.value}}\n- AI en matching: {{proposal.ai}}\n- Veiligheid en eigendom: {{proposal.security}}\n- Kosten en stappen: {{proposal.costs}}\n- Volgende stap: {{proposal.next_step}}\n\nIk kan dit in een demo laten zien en daarna de afspraak definitief maken, zodat jullie precies weten wat er in de eerste stap zit.\n\nMet vriendelijke groet,\nJeffrey Lavente\nLaventeCare"),
		},
		{
			TemplateKey:     "quote_send",
			Name:            "Offerte versturen",
			Category:        "commerce",
			Status:          "active",
			SubjectTemplate: "Offerte {{quote.number}} - {{company.naam}}",
			BodyHTML: brandedMailHTML(mailTemplateContent{
				Preheader:   "De offerte staat klaar met afspraken, planning en vervolgstap.",
				Eyebrow:     "Offerte",
				Title:       "Offerte staat klaar",
				Greeting:    "Beste {{contact.naam}},",
				Intro:       "De offerte voor {{company.naam}} staat klaar. De kern: {{quote.summary}}",
				Body:        "Ik heb de aanpak zo opgebouwd dat de afspraken, wie wat doet en het vervolg helder blijven. Zo kunnen we goed en zonder misverstanden van start.",
				FocusTitle:  "Samenvatting",
				FocusItems:  []string{"Offertenummer: {{quote.number}}", "Kern: {{quote.summary}}", "Volgende stap: {{next_step}}"},
				CTAURL:      "{{quote.url}}",
				CTALabel:    "Offerte bekijken",
				ClosingLine: "Laat me weten of je akkoord bent, dan plan ik de uitvoering in.",
			}),
			BodyText: mailStrPtr("Beste {{contact.naam}},\n\nDe offerte voor {{company.naam}} staat klaar. De kern: {{quote.summary}}\n\nSamenvatting:\n- Offertenummer: {{quote.number}}\n- Kern: {{quote.summary}}\n- Volgende stap: {{next_step}}\n\nLaat me weten of je akkoord bent, dan plan ik de uitvoering in.\n\nMet vriendelijke groet,\nJeffrey Lavente\nLaventeCare"),
		},
		{
			TemplateKey:     "invoice_send",
			Name:            "Factuur en betaalverzoek",
			Category:        "commerce",
			Status:          "active",
			SubjectTemplate: "Factuur {{invoice.number}} - {{company.naam}}",
			BodyHTML: brandedMailHTML(mailTemplateContent{
				Preheader:   "De factuur en betaalinformatie staan klaar.",
				Eyebrow:     "Facturatie",
				Title:       "Factuur staat klaar",
				Greeting:    "Beste {{contact.naam}},",
				Intro:       "De factuur voor de uitgevoerde werkzaamheden voor {{company.naam}} staat klaar.",
				Body:        "Je kunt betalen via het betaalverzoek hieronder. De betaling wordt gekoppeld aan het klantdossier zodat administratie en opvolging netjes bij elkaar blijven.",
				FocusTitle:  "Factuurinformatie",
				FocusItems:  []string{"Factuurnummer: {{invoice.number}}", "Bedrag: {{invoice.amount}}", "Betaaltermijn: {{invoice.due_date}}"},
				CTAURL:      "{{invoice.payment_url}}",
				CTALabel:    "Factuur betalen",
				ClosingLine: "Als er iets niet klopt, hoor ik het graag direct.",
			}),
			BodyText: mailStrPtr("Beste {{contact.naam}},\n\nDe factuur voor de uitgevoerde werkzaamheden voor {{company.naam}} staat klaar.\n\nFactuurinformatie:\n- Factuurnummer: {{invoice.number}}\n- Bedrag: {{invoice.amount}}\n- Betaaltermijn: {{invoice.due_date}}\n\nBetalen kan via: {{invoice.payment_url}}\n\nAls er iets niet klopt, hoor ik het graag direct.\n\nMet vriendelijke groet,\nJeffrey Lavente\nLaventeCare"),
		},
		{
			TemplateKey:     "payment_reminder",
			Name:            "Betalingsherinnering",
			Category:        "commerce",
			Status:          "active",
			SubjectTemplate: "Herinnering factuur {{invoice.number}} - {{company.naam}}",
			BodyHTML: brandedMailHTML(mailTemplateContent{
				Preheader:   "Vriendelijke herinnering voor een openstaande factuur.",
				Eyebrow:     "Betalingsherinnering",
				Title:       "Openstaande factuur",
				Greeting:    "Beste {{contact.naam}},",
				Intro:       "Ik zie dat factuur {{invoice.number}} voor {{company.naam}} nog openstaat. Mogelijk is deze er tussendoor geglipt.",
				Body:        "Hieronder staat de betaalinformatie nogmaals compact bij elkaar.",
				FocusTitle:  "Openstaand",
				FocusItems:  []string{"Factuurnummer: {{invoice.number}}", "Bedrag: {{invoice.amount}}", "Vervaldatum: {{invoice.due_date}}"},
				CTAURL:      "{{invoice.payment_url}}",
				CTALabel:    "Nu betalen",
				ClosingLine: "Als betaling inmiddels onderweg is, kun je deze mail als niet verzonden beschouwen.",
			}),
			BodyText: mailStrPtr("Beste {{contact.naam}},\n\nIk zie dat factuur {{invoice.number}} voor {{company.naam}} nog openstaat. Mogelijk is deze er tussendoor geglipt.\n\nOpenstaand:\n- Factuurnummer: {{invoice.number}}\n- Bedrag: {{invoice.amount}}\n- Vervaldatum: {{invoice.due_date}}\n\nBetalen kan via: {{invoice.payment_url}}\n\nAls betaling inmiddels onderweg is, kun je deze mail als niet verzonden beschouwen.\n\nMet vriendelijke groet,\nJeffrey Lavente\nLaventeCare"),
		},
		{
			TemplateKey:     "project_update",
			Name:            "Projectupdate",
			Category:        "delivery",
			Status:          "active",
			SubjectTemplate: "Projectupdate - {{project.naam}}",
			BodyHTML: brandedMailHTML(mailTemplateContent{
				Preheader:   "Korte update over voortgang, aandachtspunten en vervolgstap.",
				Eyebrow:     "Delivery update",
				Title:       "Projectupdate",
				Greeting:    "Beste {{contact.naam}},",
				Intro:       "Hierbij een korte update over {{project.naam}}.",
				Body:        "{{project.update}}",
				FocusTitle:  "Nu belangrijk",
				FocusItems:  []string{"Status: {{project.status}}", "Aandachtspunt: {{project.risk}}", "Volgende stap: {{next_step}}"},
				CTAURL:      "{{project.url}}",
				CTALabel:    "Project bekijken",
				ClosingLine: "Ik houd de lijn kort en praktisch, zodat we snel kunnen bijsturen waar nodig.",
			}),
			BodyText: mailStrPtr("Beste {{contact.naam}},\n\nHierbij een korte update over {{project.naam}}.\n\n{{project.update}}\n\nNu belangrijk:\n- Status: {{project.status}}\n- Aandachtspunt: {{project.risk}}\n- Volgende stap: {{next_step}}\n\nMet vriendelijke groet,\nJeffrey Lavente\nLaventeCare"),
		},
		{
			TemplateKey:     "pilot_start",
			Name:            "Pilot / testfase start",
			Category:        "delivery",
			Status:          "active",
			SubjectTemplate: "Start pilot - {{project.naam}}",
			BodyHTML: brandedMailHTML(mailTemplateContent{
				Preheader:   "De pilot/testfase start met heldere afspraken, testcriteria en feedbackmoment.",
				Eyebrow:     "Pilotfase",
				Title:       "Pilot en testfase starten",
				Greeting:    "Beste {{contact.naam}},",
				Intro:       "We kunnen de pilot/testfase voor {{project.naam}} starten. In deze fase toetsen we de belangrijkste onderdelen in de praktijk, zonder de volledige uitrol al definitief vast te zetten.",
				Body:        "De focus ligt op een korte, controleerbare test: wat werkt goed, waar zit frictie en welke keuzes moeten we maken voor de volgende stap.",
				FocusTitle:  "Pilotafspraken",
				FocusItems:  []string{"Afspraak: {{pilot.scope}}", "Testcriteria: {{pilot.criteria}}", "Feedbackmoment: {{pilot.feedback_moment}}", "Toegang: {{pilot.access_intro}}", "Vervolg: {{next_step}}"},
				ExtraHTML:   "{{pilot.access_block_html}}",
				CTAURL:      "{{pilot.login_url}}",
				CTALabel:    "Pilot openen",
				ClosingLine: "Na deze pilot weten we genoeg om een goede keuze te maken: bijsturen, afronden of breder uitrollen.",
			}),
			BodyText: mailStrPtr("Beste {{contact.naam}},\n\nWe kunnen de pilot/testfase voor {{project.naam}} starten. In deze fase toetsen we de belangrijkste onderdelen in de praktijk, zonder de volledige uitrol al definitief vast te zetten.\n\nPilotafspraken:\n- Afspraak: {{pilot.scope}}\n- Testcriteria: {{pilot.criteria}}\n- Feedbackmoment: {{pilot.feedback_moment}}\n- Pilotomgeving: {{pilot.login_url}}\n- Toegang: {{pilot.access_intro}}\n{{pilot.access_summary}}\n- Vervolg: {{next_step}}\n\nNa deze pilot weten we genoeg om een goede keuze te maken: bijsturen, afronden of breder uitrollen.\n\nMet vriendelijke groet,\nJeffrey Lavente\nLaventeCare"),
		},
		{
			TemplateKey:     "delivery_handover",
			Name:            "Oplevering en overdracht",
			Category:        "delivery",
			Status:          "active",
			SubjectTemplate: "Oplevering - {{project.naam}}",
			BodyHTML: brandedMailHTML(mailTemplateContent{
				Preheader:   "Oplevering, toegang en afspraken voor beheer staan klaar.",
				Eyebrow:     "Oplevering",
				Title:       "Oplevering en overdracht",
				Greeting:    "Beste {{contact.naam}},",
				Intro:       "{{project.naam}} is klaar voor oplevering. Hieronder staat compact wat is afgerond en wat de vervolgstap is.",
				Body:        "Ik heb de oplevering zo ingericht dat de belangrijkste onderdelen makkelijk terug te vinden blijven: afspraken, toegang, documentatie en eventuele beheerpunten.",
				FocusTitle:  "Overdracht",
				FocusItems:  []string{"Opgeleverd: {{delivery.done}}", "Nog te controleren: {{delivery.check}}", "Vervolg/beheer: {{next_step}}"},
				CTAURL:      "{{project.url}}",
				CTALabel:    "Oplevering bekijken",
				ClosingLine: "Na jouw akkoord zet ik de status definitief op opgeleverd.",
			}),
			BodyText: mailStrPtr("Beste {{contact.naam}},\n\n{{project.naam}} is klaar voor oplevering. Hieronder staat compact wat is afgerond en wat de vervolgstap is.\n\nOverdracht:\n- Opgeleverd: {{delivery.done}}\n- Nog te controleren: {{delivery.check}}\n- Vervolg/beheer: {{next_step}}\n\nNa jouw akkoord zet ik de status definitief op opgeleverd.\n\nMet vriendelijke groet,\nJeffrey Lavente\nLaventeCare"),
		},
		{
			TemplateKey:     "documentation_handover",
			Name:            "Klantdocumentatie versturen",
			Category:        "delivery",
			Status:          "active",
			SubjectTemplate: "Documentatie pilot - {{project.naam}}",
			BodyHTML: brandedMailHTML(mailTemplateContent{
				Preheader:   "De belangrijkste documentatie voor de pilot staat als bijlage klaar.",
				Eyebrow:     "Documentatie",
				Title:       "Klantdocumentatie voor de pilot",
				Greeting:    "Beste {{contact.naam}},",
				Intro:       "Hierbij stuur ik de documentatie voor {{project.naam}}.",
				Body:        "{{documentation.summary}}",
				FocusTitle:  "Bijgevoegd",
				FocusItems:  []string{"Documenten: {{documentation.attachments}}", "Gebruik: als praktische start- en naslagset voor de pilot.", "Vervolg: {{documentation.next_step}}"},
				CTAURL:      "{{project.url}}",
				CTALabel:    "Project bekijken",
				ClosingLine: "Zo houden we de start overzichtelijk en kunnen we gericht bijsturen waar nodig.",
			}),
			BodyText: mailStrPtr("Beste {{contact.naam}},\n\nHierbij stuur ik de documentatie voor {{project.naam}}.\n\n{{documentation.summary}}\n\nBijgevoegd:\n- Documenten: {{documentation.attachments}}\n- Gebruik: als praktische start- en naslagset voor de pilot.\n- Vervolg: {{documentation.next_step}}\n\nZo houden we de start overzichtelijk en kunnen we gericht bijsturen waar nodig.\n\nMet vriendelijke groet,\nJeffrey Lavente\nLaventeCare"),
		},
		{
			TemplateKey:     "meeting_recap",
			Name:            "Gespreksverslag",
			Category:        "dossier",
			Status:          "active",
			SubjectTemplate: "Samenvatting gesprek - {{company.naam}}",
			BodyHTML: brandedMailHTML(mailTemplateContent{
				Preheader:   "Korte samenvatting met besluiten, acties en vervolg.",
				Eyebrow:     "Klantdossier",
				Title:       "Samenvatting van ons gesprek",
				Greeting:    "Beste {{contact.naam}},",
				Intro:       "Hierbij de korte samenvatting van ons gesprek over {{company.naam}}.",
				Body:        "{{meeting.summary}}",
				FocusTitle:  "Acties en afspraken",
				FocusItems:  []string{"Besproken: {{meeting.topic}}", "Acties: {{meeting.actions}}", "Volgende stap: {{next_step}}"},
				CTAURL:      "{{meeting.url}}",
				CTALabel:    "Dossier bekijken",
				ClosingLine: "Als ik iets verkeerd heb geïnterpreteerd, hoor ik het graag. Dan corrigeer ik het dossier direct.",
			}),
			BodyText: mailStrPtr("Beste {{contact.naam}},\n\nHierbij de korte samenvatting van ons gesprek over {{company.naam}}.\n\n{{meeting.summary}}\n\nActies en afspraken:\n- Besproken: {{meeting.topic}}\n- Acties: {{meeting.actions}}\n- Volgende stap: {{next_step}}\n\nAls ik iets verkeerd heb geïnterpreteerd, hoor ik het graag. Dan corrigeer ik het dossier direct.\n\nMet vriendelijke groet,\nJeffrey Lavente\nLaventeCare"),
		},
		{
			TemplateKey:     "support_sla",
			Name:            "Support / SLA update",
			Category:        "support",
			Status:          "active",
			SubjectTemplate: "Support update - {{company.naam}}",
			BodyHTML: brandedMailHTML(mailTemplateContent{
				Preheader:   "Update over melding, status en vervolgstap.",
				Eyebrow:     "Support",
				Title:       "Support update",
				Greeting:    "Beste {{contact.naam}},",
				Intro:       "Hierbij een update over de melding voor {{company.naam}}.",
				Body:        "{{support.summary}}",
				FocusTitle:  "Status",
				FocusItems:  []string{"Prioriteit: {{support.priority}}", "Status: {{support.status}}", "Volgende stap: {{next_step}}"},
				CTAURL:      "{{support.url}}",
				CTALabel:    "Melding bekijken",
				ClosingLine: "Ik houd dit bij tot de melding volledig is afgerond.",
			}),
			BodyText: mailStrPtr("Beste {{contact.naam}},\n\nHierbij een update over de melding voor {{company.naam}}.\n\n{{support.summary}}\n\nStatus:\n- Prioriteit: {{support.priority}}\n- Status: {{support.status}}\n- Volgende stap: {{next_step}}\n\nIk houd dit bij tot de melding volledig is afgerond.\n\nMet vriendelijke groet,\nJeffrey Lavente\nLaventeCare"),
		},
		{
			TemplateKey:     "change_request",
			Name:            "Wijzigingsverzoek",
			Category:        "operations",
			Status:          "active",
			SubjectTemplate: "Wijzigingsverzoek - {{company.naam}}",
			BodyHTML: brandedMailHTML(mailTemplateContent{
				Preheader:   "Impact, planning en akkoord voor een wijziging.",
				Eyebrow:     "Change request",
				Title:       "Wijziging ter bevestiging",
				Greeting:    "Beste {{contact.naam}},",
				Intro:       "Voor {{company.naam}} staat er een wijziging klaar ter bevestiging.",
				Body:        "{{change.summary}}",
				FocusTitle:  "Impact",
				FocusItems:  []string{"Wijziging: {{change.title}}", "Planning impact: {{change.planning_impact}}", "Budget impact: {{change.budget_impact}}"},
				CTAURL:      "{{change.url}}",
				CTALabel:    "Wijziging bekijken",
				ClosingLine: "Na akkoord verwerk ik dit in planning en klantdossier.",
			}),
			BodyText: mailStrPtr("Beste {{contact.naam}},\n\nVoor {{company.naam}} staat er een wijziging klaar ter bevestiging.\n\n{{change.summary}}\n\nImpact:\n- Wijziging: {{change.title}}\n- Planning impact: {{change.planning_impact}}\n- Budget impact: {{change.budget_impact}}\n\nNa akkoord verwerk ik dit in planning en klantdossier.\n\nMet vriendelijke groet,\nJeffrey Lavente\nLaventeCare"),
		},
	}

	for _, template := range defaults {
		_, err := s.db.Pool.Exec(ctx,
			`INSERT INTO lc_mail_templates (id, user_id, template_key, name, category, status,
			        subject_template, body_html, body_text, default_cc, default_bcc, created_at, updated_at)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12)
			 ON CONFLICT (user_id, template_key) DO UPDATE SET
			        name = EXCLUDED.name,
			        category = EXCLUDED.category,
			        status = EXCLUDED.status,
			        subject_template = EXCLUDED.subject_template,
			        body_html = EXCLUDED.body_html,
			        body_text = EXCLUDED.body_text,
			        default_cc = EXCLUDED.default_cc,
			        default_bcc = EXCLUDED.default_bcc,
			        updated_at = EXCLUDED.updated_at
			  WHERE lc_mail_templates.body_html NOT LIKE '%laventecare-mail-shell:v2%'`,
			uuid.New(), userID, template.TemplateKey, template.Name, template.Category, template.Status,
			template.SubjectTemplate, template.BodyHTML, template.BodyText, cleanEmails(template.DefaultCC),
			cleanEmails(template.DefaultBCC), now)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *LaventeCareStore) ListMailTemplates(ctx context.Context, userID string, limit int) ([]model.LCMailTemplate, error) {
	if limit <= 0 || limit > 100 {
		limit = 40
	}
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, template_key, name, category, status, subject_template,
		        body_html, body_text, default_cc, default_bcc, created_at, updated_at
		   FROM lc_mail_templates
		  WHERE user_id = $1
		  ORDER BY category ASC, name ASC
		  LIMIT $2`,
		userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanMailTemplate)
}

func (s *LaventeCareStore) GetMailTemplate(ctx context.Context, userID string, id uuid.UUID) (*model.LCMailTemplate, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, template_key, name, category, status, subject_template,
		        body_html, body_text, default_cc, default_bcc, created_at, updated_at
		   FROM lc_mail_templates
		  WHERE user_id = $1 AND id = $2
		  LIMIT 1`,
		userID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	templates, err := pgx.CollectRows(rows, scanMailTemplate)
	if err != nil {
		return nil, err
	}
	if len(templates) == 0 {
		return nil, pgx.ErrNoRows
	}
	return &templates[0], nil
}

func (s *LaventeCareStore) CreateMailTemplate(ctx context.Context, userID string, input model.LCMailTemplateCreate) (*model.LCMailTemplate, error) {
	now := time.Now().UTC()
	id := uuid.New()
	key := strings.ToLower(strings.TrimSpace(input.TemplateKey))
	if key == "" {
		key = "template_" + id.String()[:8]
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "Nieuwe template"
	}
	category := cleanStatus(input.Category, "general")
	status := cleanStatus(input.Status, "active")
	subject := strings.TrimSpace(input.SubjectTemplate)
	bodyHTML := strings.TrimSpace(input.BodyHTML)
	if subject == "" || bodyHTML == "" {
		return nil, pgx.ErrNoRows
	}

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO lc_mail_templates (id, user_id, template_key, name, category, status,
		        subject_template, body_html, body_text, default_cc, default_bcc, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12)`,
		id, userID, key, name, category, status, subject, bodyHTML, cleanStringPtr(input.BodyText),
		cleanEmails(input.DefaultCC), cleanEmails(input.DefaultBCC), now)
	if err != nil {
		return nil, err
	}
	return s.GetMailTemplate(ctx, userID, id)
}

func (s *LaventeCareStore) UpdateMailTemplate(ctx context.Context, userID string, id uuid.UUID, input model.LCMailTemplateUpdate) error {
	tag, err := s.db.Pool.Exec(ctx,
		`UPDATE lc_mail_templates SET
			name = COALESCE($3, name),
			category = COALESCE($4, category),
			status = COALESCE($5, status),
			subject_template = COALESCE($6, subject_template),
			body_html = COALESCE($7, body_html),
			body_text = COALESCE($8, body_text),
			default_cc = CASE WHEN $9::text[] IS NULL THEN default_cc ELSE $9 END,
			default_bcc = CASE WHEN $10::text[] IS NULL THEN default_bcc ELSE $10 END,
			updated_at = $11
		 WHERE user_id = $1 AND id = $2`,
		userID, id, cleanStringPtr(input.Name), cleanStringPtr(input.Category), cleanStringPtr(input.Status),
		cleanStringPtr(input.SubjectTemplate), cleanStringPtr(input.BodyHTML), cleanStringPtr(input.BodyText),
		nullableEmails(input.DefaultCC), nullableEmails(input.DefaultBCC), time.Now().UTC())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *LaventeCareStore) ListMailOutbox(ctx context.Context, userID string, limit int) ([]model.LCMailOutboxItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 40
	}
	rows, err := s.db.Pool.Query(ctx, mailOutboxSelectSQL()+` WHERE m.user_id = $1 ORDER BY m.created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanMailOutboxItem)
}

func (s *LaventeCareStore) CreateMailFromTemplate(ctx context.Context, userID string, input model.LCMailSendRequest) (*model.LCMailOutboxItem, error) {
	template, err := s.GetMailTemplate(ctx, userID, input.TemplateID)
	if err != nil {
		return nil, err
	}
	if template.Status != "active" {
		return nil, errors.New("Mailtemplate is niet actief.")
	}

	contextValues, companyID, contactID, toEmail, toName, err := s.buildMailRenderContext(ctx, userID, input, template.TemplateKey)
	if err != nil {
		return nil, err
	}
	subject := cleanupRenderedMailSubject(renderTemplate(template.SubjectTemplate, contextValues))
	// Optional explicit subject override (reply flow sends "Re: <origineel>").
	if override := cleanStringPtr(input.Subject); override != nil {
		subject = cleanupRenderedMailSubject(*override)
	}
	bodyHTML := cleanupRenderedMailHTML(renderMailHTML(template.BodyHTML, contextValues))
	bodyText := cleanStringPtr(template.BodyText)
	if bodyText != nil {
		rendered := renderTemplate(*bodyText, contextValues)
		rendered = cleanupRenderedMailText(rendered)
		bodyText = &rendered
	}

	id := uuid.New()
	now := time.Now().UTC()
	// conversation_id is stored for UI threading/grouping with the inbound
	// message being replied to; sending still creates a fresh Graph message
	// (a true Graph reply is not plumbed through). On actual send, Graph's own
	// conversation id replaces it via MarkMailOutboxSent's COALESCE.
	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO lc_mail_outbox (id, user_id, template_id, company_id, contact_id,
		        project_id, workstream_id, quote_id, invoice_id, to_email, to_name, cc, bcc,
		        subject, body_html, body_text, conversation_id, status, provider, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,'concept','microsoft_graph',$18,$18)`,
		id, userID, template.ID, companyID, contactID, input.ProjectID, input.WorkstreamID,
		input.QuoteID, input.InvoiceID, toEmail, toName, mergeEmails(template.DefaultCC, input.CC),
		mergeEmails(template.DefaultBCC, input.BCC), subject, bodyHTML, bodyText,
		cleanStringPtr(input.ConversationID), now)
	if err != nil {
		return nil, err
	}
	return s.GetMailOutboxItem(ctx, userID, id)
}

func (s *LaventeCareStore) GetMailOutboxItem(ctx context.Context, userID string, id uuid.UUID) (*model.LCMailOutboxItem, error) {
	rows, err := s.db.Pool.Query(ctx, mailOutboxSelectSQL()+` WHERE m.user_id = $1 AND m.id = $2 LIMIT 1`, userID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items, err := pgx.CollectRows(rows, scanMailOutboxItem)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, pgx.ErrNoRows
	}
	return &items[0], nil
}

func (s *LaventeCareStore) MarkMailOutboxSending(ctx context.Context, userID string, id uuid.UUID) error {
	now := time.Now().UTC()
	tag, err := s.db.Pool.Exec(ctx,
		`UPDATE lc_mail_outbox
		    SET status = 'sending', error_message = NULL, updated_at = $3
		  WHERE user_id = $1 AND id = $2`,
		userID, id, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *LaventeCareStore) MarkMailOutboxSent(ctx context.Context, userID string, id uuid.UUID, providerMessageID, conversationID string) error {
	now := time.Now().UTC()
	msgID := cleanStringPtr(&providerMessageID)
	convID := cleanStringPtr(&conversationID)
	// Once delivered, redact any embedded pilot password from the stored bodies so
	// plaintext secrets don't linger in lc_mail_outbox indefinitely. The recipient
	// already received the real password; this only scrubs the at-rest copy. The
	// text pattern is per-line (n flag); the HTML pattern is anchored on the exact
	// monospace "secret" span style so it can't touch other content.
	// COALESCE keeps an already-stored conversation_id (the reply flow pins the
	// ORIGINAL conversation so the thread groups with the inbound message) and
	// only falls back to Graph's own conversation id for normal sends.
	tag, err := s.db.Pool.Exec(ctx,
		`UPDATE lc_mail_outbox
		    SET status = 'sent', provider_message_id = $3, conversation_id = COALESCE(conversation_id, $5),
		        error_message = NULL, sent_at = $4, updated_at = $4,
		        body_text = regexp_replace(COALESCE(body_text, ''),
		                    'Wachtwoord:.*', 'Wachtwoord: ••• (verwijderd na verzending)', 'gn'),
		        body_html = regexp_replace(COALESCE(body_html, ''),
		                    '(background:#e2e8f0;border:1px solid #cbd5e1;border-radius:8px;padding:7px 9px;font-family:ui-monospace[^>]*>)[^<]*(</span>)',
		                    '\1••• (verwijderd na verzending)\2', 'g')
		  WHERE user_id = $1 AND id = $2`,
		userID, id, msgID, now, convID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *LaventeCareStore) MarkMailOutboxFailed(ctx context.Context, userID string, id uuid.UUID, message string) error {
	now := time.Now().UTC()
	tag, err := s.db.Pool.Exec(ctx,
		`UPDATE lc_mail_outbox
		    SET status = 'failed', error_message = $3, updated_at = $4
		  WHERE user_id = $1 AND id = $2`,
		userID, id, strings.TrimSpace(message), now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// ─── Inbound mail ────────────────────────────────────────────────────────────

// UpsertInboxMessages idempotently stores received mail (keyed on the Graph
// message id) and links each message to a company/contact by matching the sender
// address against lc_contacts.email. Returns how many rows were inserted/updated.
func (s *LaventeCareStore) UpsertInboxMessages(ctx context.Context, userID string, msgs []model.LCMailInboxItem) (int, error) {
	if len(msgs) == 0 {
		return 0, nil
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	count := 0
	for _, m := range msgs {
		if strings.TrimSpace(m.MessageID) == "" || strings.TrimSpace(m.FromEmail) == "" {
			continue
		}
		received := m.ReceivedAt
		if received.IsZero() {
			received = time.Now().UTC()
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO lc_mail_inbox (user_id, message_id, conversation_id, from_email, from_name,
				subject, body_preview, web_link, has_attachments, is_read, received_at,
				company_id, contact_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,
				(SELECT company_id FROM lc_contacts WHERE user_id = $1 AND lower(email) = lower($4) LIMIT 1),
				(SELECT id FROM lc_contacts WHERE user_id = $1 AND lower(email) = lower($4) LIMIT 1))
			ON CONFLICT (user_id, message_id) DO UPDATE SET
				is_read         = EXCLUDED.is_read,
				conversation_id = COALESCE(EXCLUDED.conversation_id, lc_mail_inbox.conversation_id),
				updated_at      = now()
		`, userID, m.MessageID, m.ConversationID, strings.ToLower(strings.TrimSpace(m.FromEmail)), m.FromName,
			m.Subject, m.BodyPreview, m.WebLink, m.HasAttachments, m.IsRead, received)
		if err != nil {
			return count, err
		}
		count += int(tag.RowsAffected())
	}
	if err := tx.Commit(ctx); err != nil {
		return count, err
	}
	return count, nil
}

// ListInbox returns recent received mail, newest first, with the linked company name.
func (s *LaventeCareStore) ListInbox(ctx context.Context, userID string, limit int) ([]model.LCMailInboxItem, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Pool.Query(ctx, `
		SELECT i.id, i.user_id, i.message_id, i.conversation_id, i.company_id, i.contact_id,
			i.from_email, i.from_name, i.subject, i.body_preview, i.web_link,
			i.has_attachments, i.is_read, i.received_at, i.created_at, i.updated_at, c.naam
		FROM lc_mail_inbox i
		LEFT JOIN lc_companies c ON c.id = i.company_id
		WHERE i.user_id = $1
		ORDER BY i.received_at DESC
		LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.LCMailInboxItem{}
	for rows.Next() {
		var m model.LCMailInboxItem
		if err := rows.Scan(&m.ID, &m.UserID, &m.MessageID, &m.ConversationID, &m.CompanyID, &m.ContactID,
			&m.FromEmail, &m.FromName, &m.Subject, &m.BodyPreview, &m.WebLink,
			&m.HasAttachments, &m.IsRead, &m.ReceivedAt, &m.CreatedAt, &m.UpdatedAt, &m.CompanyName); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// LatestInboxReceivedAt returns the most recent received_at (zero when empty),
// used to fetch only newer messages on the next sync.
func (s *LaventeCareStore) LatestInboxReceivedAt(ctx context.Context, userID string) (time.Time, error) {
	var t *time.Time
	if err := s.db.Pool.QueryRow(ctx, `SELECT max(received_at) FROM lc_mail_inbox WHERE user_id = $1`, userID).Scan(&t); err != nil {
		return time.Time{}, err
	}
	if t == nil {
		return time.Time{}, nil
	}
	return *t, nil
}

// MarkInboxRead flips a received message to read in-app (the Graph sync also reconciles
// is_read, but this lets the owner clear it without leaving the app).
func (s *LaventeCareStore) MarkInboxRead(ctx context.Context, userID string, id uuid.UUID) error {
	tag, err := s.db.Pool.Exec(ctx,
		`UPDATE lc_mail_inbox SET is_read = TRUE, updated_at = now() WHERE user_id = $1 AND id = $2`,
		userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *LaventeCareStore) buildMailRenderContext(ctx context.Context, userID string, input model.LCMailSendRequest, templateKey string) (map[string]string, *uuid.UUID, *uuid.UUID, string, *string, error) {
	inputVars := safeStringMap(input.Variables)
	values := map[string]string{
		"laventecare.name":          "LaventeCare",
		"laventecare.owner":         "Jeffrey Lavente",
		"laventecare.email":         valueOr(strings.TrimSpace(os.Getenv("MICROSOFT_SENDER_EMAIL")), "jeffrey@laventecare.nl"),
		"laventecare.phone":         "+31 6 39 03 40 85",
		"laventecare.website":       "https://www.laventecare.nl",
		"laventecare.logo_url":      "https://ik.imagekit.io/a0oim4e3e/tr:f-png,w-112/LaventeCare/logo.svg?updatedAt=1779275051433",
		"laventecare.tagline":       "Van idee tot werkend systeem",
		"cta.label":                 "Afstemmen",
		"cta.url":                   "",
		"quote.number":              "concept",
		"quote.summary":             "de afgesproken scope",
		"quote.url":                 "",
		"invoice.number":            "concept",
		"invoice.amount":            "zie factuur",
		"invoice.due_date":          "14 dagen",
		"invoice.payment_url":       "",
		"project.naam":              "het project",
		"project.status":            "in uitvoering",
		"project.update":            "De voortgang loopt volgens afspraak.",
		"project.risk":              "geen bijzonderheden",
		"project.url":               "",
		"proposal.scope":            "de afgesproken scope",
		"proposal.current_state":    "Er is een werkende basis, maar productieafspraken, beveiliging, AVG, onderhoud en fasering moeten nog expliciet worden vastgelegd.",
		"proposal.value":            "minder handmatig werk, sneller kandidaten verwerken, beter overzicht en beter onderbouwde matches",
		"proposal.ai":               "de AI-score is adviserend, uitlegbaar en blijft onder menselijke controle",
		"proposal.security":         "broncode, export, eigendom en AVG-afspraken worden expliciet vastgelegd voordat productie live gaat",
		"proposal.costs":            "projectkosten en maandelijkse kosten worden apart gemaakt, inclusief hosting, AI, opslag en onderhoud",
		"proposal.next_step":        "demo tonen, vragen afstemmen en daarna scope/offerte definitief maken",
		"pilot.scope":               "de afgesproken testscope",
		"pilot.criteria":            "kernfunctionaliteit, gebruiksgemak en betrouwbaarheid",
		"pilot.feedback_moment":     "na de eerste testperiode",
		"pilot.login_url":           "",
		"pilot.access_intro":        "pilottoegang stemmen we voor de start af via het afgesproken kanaal",
		"pilot.access_summary":      "pilottoegang stemmen we voor de start af via het afgesproken kanaal",
		"pilot.access_block_html":   "",
		"meeting.topic":             "afstemming",
		"meeting.summary":           "De besproken punten zijn vastgelegd in het klantdossier.",
		"meeting.actions":           "de vervolgstap wordt opgepakt",
		"meeting.url":               "",
		"delivery.done":             "de afgesproken werkzaamheden",
		"delivery.check":            "laatste controle door klant",
		"documentation.summary":     "de klantdocumentatie staat klaar voor gebruik tijdens de pilot.",
		"documentation.attachments": "quickstart, workflowhandleiding, pilotafspraken en vrijgave/datakwaliteit",
		"documentation.next_step":   "loop de documenten rustig door en geef aan welke punten we bij de start samen willen aanscherpen",
		"support.priority":          "normaal",
		"support.status":            "in behandeling",
		"support.summary":           "De melding is geregistreerd en wordt opgevolgd.",
		"support.url":               "",
		"change.title":              "wijziging",
		"change.summary":            "De wijziging is vastgelegd ter bevestiging.",
		"change.planning_impact":    "nog te bepalen",
		"change.budget_impact":      "nog te bepalen",
		"change.url":                "",
		"next_step":                 "Ik hoor graag wat voor jou het beste moment is om dit op te pakken.",
	}

	var company *model.LCCompany
	var contact *model.LCContact
	var mailProject map[string]any
	var mailWorkstream map[string]any
	companyID := input.CompanyID
	contactID := input.ContactID

	if contactID != nil {
		c, err := s.GetContact(ctx, userID, *contactID)
		if err != nil {
			return nil, nil, nil, "", nil, err
		}
		contact = c
		if companyID == nil && c.CompanyID != nil {
			companyID = c.CompanyID
		}
		values["contact.naam"] = c.Naam
		values["contact.email"] = deref(c.Email)
		values["contact.rol"] = deref(c.Rol)
	}
	if companyID != nil {
		c, err := s.GetCompany(ctx, userID, *companyID)
		if err != nil {
			return nil, nil, nil, "", nil, err
		}
		company = c
		values["company.naam"] = c.Naam
		values["company.website"] = deref(c.Website)
		values["company.sector"] = deref(c.Sector)
		values["company.volgende_actie"] = deref(c.VolgendeActie)
	}
	if input.ProjectID != nil {
		project, _, err := s.mailAIProject(ctx, userID, input.ProjectID)
		if err != nil {
			return nil, nil, nil, "", nil, err
		}
		mailProject = project
		setMailValue(values, "project.naam", stringMapValue(project, "naam"))
		setMailValue(values, "project.status", stringMapValue(project, "status"))
		setMailValue(values, "project.update", stringMapValue(project, "samenvatting"))
		setMailValue(values, "pilot.scope", stringMapValue(project, "samenvatting"))
		setMailValue(values, "proposal.scope", stringMapValue(project, "samenvatting"))
	}
	if input.WorkstreamID != nil {
		workstream, _, projectID, err := s.mailAIWorkstream(ctx, userID, input.WorkstreamID)
		if err != nil {
			return nil, nil, nil, "", nil, err
		}
		mailWorkstream = workstream
		if input.ProjectID == nil && projectID != nil {
			input.ProjectID = projectID
		}
		setMailValue(values, "meeting.topic", stringMapValue(workstream, "titel"))
		setMailValue(values, "quote.summary", joinMailParts([]string{
			stringMapValue(workstream, "doel"),
			stringMapValue(workstream, "scope"),
			stringMapValue(workstream, "deliverable"),
		}, " "))
		proposalScope := joinMailParts([]string{
			stringMapValue(workstream, "doel"),
			stringMapValue(workstream, "scope"),
			stringMapValue(workstream, "deliverable"),
		}, " ")
		setMailValue(values, "proposal.scope", proposalScope)
		setMailValue(values, "proposal.current_state", stringMapValue(workstream, "bevindingen"))
		setMailValue(values, "proposal.next_step", stringMapValue(workstream, "volgende_stap"))
		setMailValue(values, "project.update", joinMailParts([]string{
			stringMapValue(workstream, "bevindingen"),
			stringMapValue(workstream, "volgende_stap"),
		}, " "))
		setMailValue(values, "pilot.scope", joinMailParts([]string{
			stringMapValue(workstream, "doel"),
			stringMapValue(workstream, "scope"),
			stringMapValue(workstream, "deliverable"),
		}, " "))
		setMailValue(values, "pilot.criteria", joinMailParts([]string{
			stringMapValue(workstream, "deliverable"),
			"functionele controle en feedback op de afgesproken scope",
		}, " - "))
		setMailValue(values, "pilot.feedback_moment", stringMapValue(workstream, "deadline"))
		setMailValue(values, "next_step", stringMapValue(workstream, "volgende_stap"))
	}
	if input.QuoteID != nil {
		quote, err := s.GetQuote(ctx, userID, *input.QuoteID)
		if err != nil {
			return nil, nil, nil, "", nil, err
		}
		setMailValue(values, "quote.number", quote.QuoteNumber)
		setMailValue(values, "quote.summary", joinMailParts([]string{quote.Titel, centsDisplay(quote.Currency, quote.TotalCents), deref(quote.Notes)}, " - "))
		if companyID == nil && quote.CompanyID != nil {
			companyID = quote.CompanyID
		}
	}
	if input.InvoiceID != nil {
		invoice, err := s.GetInvoice(ctx, userID, *input.InvoiceID)
		if err != nil {
			return nil, nil, nil, "", nil, err
		}
		setMailValue(values, "invoice.number", invoice.InvoiceNumber)
		setMailValue(values, "invoice.amount", centsDisplay(invoice.Currency, invoice.TotalCents))
		setMailValue(values, "invoice.due_date", deref(invoice.DueDate))
		setMailValue(values, "invoice.payment_url", deref(invoice.PaymentURL))
		if companyID == nil && invoice.CompanyID != nil {
			companyID = invoice.CompanyID
		}
	}
	if company == nil && companyID != nil {
		c, err := s.GetCompany(ctx, userID, *companyID)
		if err != nil {
			return nil, nil, nil, "", nil, err
		}
		company = c
		values["company.naam"] = c.Naam
		values["company.website"] = deref(c.Website)
		values["company.sector"] = deref(c.Sector)
		values["company.volgende_actie"] = deref(c.VolgendeActie)
	}

	toEmail := strings.TrimSpace(deref(input.ToEmail))
	if toEmail == "" && contact != nil {
		toEmail = strings.TrimSpace(deref(contact.Email))
	}
	if toEmail == "" {
		return nil, nil, nil, "", nil, errors.New("Ontvanger-e-mailadres is verplicht.")
	}
	if emailContact, err := s.mailContactByEmail(ctx, userID, toEmail, companyID); err != nil {
		return nil, nil, nil, "", nil, err
	} else if emailContact != nil {
		contact = emailContact
		contactID = &emailContact.ID
		if companyID == nil && emailContact.CompanyID != nil {
			companyID = emailContact.CompanyID
		}
		if company == nil && companyID != nil {
			c, err := s.GetCompany(ctx, userID, *companyID)
			if err != nil {
				return nil, nil, nil, "", nil, err
			}
			company = c
		}
	}
	toName := cleanStringPtr(input.ToName)
	if toName == nil && contact != nil {
		toName = &contact.Naam
	}
	for key, value := range inputVars {
		values[key] = value
	}
	// The HTML access block is generated server-side from parsed credential fields.
	values["pilot.access_block_html"] = ""
	applyResolvedMailIdentity(values, company, contact, toName)
	if strings.TrimSpace(values["pilot.login_url"]) == "" {
		setMailValue(values, "pilot.login_url", inferMailPilotLoginURL(values, company, mailProject, mailWorkstream))
	}
	accessIDs, accessKeywords := mailAIContextKeys(company, contact, mailProject, mailWorkstream)
	hasAccessNote, err := s.mailAIAccessNoteExists(ctx, userID, accessIDs, accessKeywords)
	if err != nil {
		return nil, nil, nil, "", nil, err
	}
	if hasAccessNote && templateKey == "pilot_start" {
		details, err := s.mailAIPilotAccessDetails(ctx, userID, accessIDs, accessKeywords, values["pilot.login_url"])
		if err != nil {
			return nil, nil, nil, "", nil, err
		}
		if details.Summary != "" {
			applyMailAccessDetails(values, details)
		} else if isDefaultPilotAccessSummary(values["pilot.access_summary"]) {
			setMailValue(values, "pilot.access_intro", "toegangsgegevens staan in het klantdossier")
			setMailValue(values, "pilot.access_summary", "toegangsgegevens zijn vastgelegd in het klantdossier; ik deel gevoelige gegevens alleen via het afgesproken veilige kanaal")
		}
	} else if hasAccessNote && isDefaultPilotAccessSummary(values["pilot.access_summary"]) {
		setMailValue(values, "pilot.access_intro", "toegangsgegevens staan in het klantdossier")
		setMailValue(values, "pilot.access_summary", "toegangsgegevens zijn vastgelegd in het klantdossier; ik deel gevoelige gegevens alleen via het afgesproken veilige kanaal")
	}
	normalizeMailAccessVariables(values)

	return values, companyID, contactID, toEmail, toName, nil
}

func (s *LaventeCareStore) mailContactByEmail(ctx context.Context, userID, email string, companyID *uuid.UUID) (*model.LCContact, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil, nil
	}
	var rows pgx.Rows
	var err error
	if companyID != nil {
		rows, err = s.db.Pool.Query(ctx,
			`SELECT id, user_id, company_id, naam, email, telefoon, rol, is_primary,
			        notities, preferred_channel, decision_role, created_at, updated_at
			   FROM lc_contacts
			  WHERE user_id = $1
			    AND lower(COALESCE(email, '')) = $2
			  ORDER BY CASE WHEN company_id = $3 THEN 0 ELSE 1 END, is_primary DESC, updated_at DESC
			  LIMIT 1`,
			userID, email, *companyID)
	} else {
		rows, err = s.db.Pool.Query(ctx,
			`SELECT id, user_id, company_id, naam, email, telefoon, rol, is_primary,
			        notities, preferred_channel, decision_role, created_at, updated_at
			   FROM lc_contacts
			  WHERE user_id = $1
			    AND lower(COALESCE(email, '')) = $2
			  ORDER BY is_primary DESC, updated_at DESC
			  LIMIT 1`,
			userID, email)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()
	contacts, err := pgx.CollectRows(rows, scanContact)
	if err != nil {
		return nil, err
	}
	if len(contacts) == 0 {
		return nil, nil
	}
	return &contacts[0], nil
}

func applyResolvedMailIdentity(values map[string]string, company *model.LCCompany, contact *model.LCContact, toName *string) {
	if contact != nil {
		values["contact.naam"] = contact.Naam
		values["contact.email"] = deref(contact.Email)
		values["contact.rol"] = deref(contact.Rol)
	} else if toName != nil && strings.TrimSpace(*toName) != "" {
		values["contact.naam"] = strings.TrimSpace(*toName)
	} else if strings.TrimSpace(values["contact.naam"]) == "" {
		values["contact.naam"] = "relatie"
	}
	if company != nil {
		values["company.naam"] = company.Naam
		values["company.website"] = deref(company.Website)
		values["company.sector"] = deref(company.Sector)
		values["company.volgende_actie"] = deref(company.VolgendeActie)
	} else if strings.TrimSpace(values["company.naam"]) == "" {
		values["company.naam"] = "je organisatie"
	}
}

func inferMailPilotLoginURL(values map[string]string, company *model.LCCompany, project map[string]any, workstream map[string]any) string {
	for _, value := range []string{
		values["pilot.login_url"],
		values["project.url"],
		stringMapValue(project, "url"),
		stringMapValue(project, "login_url"),
		stringMapValue(workstream, "url"),
		stringMapValue(workstream, "login_url"),
	} {
		if url := cleanMailURL(value); url != "" {
			return url
		}
	}
	parts := []string{
		values["company.naam"],
		values["company.website"],
		values["project.naam"],
		values["pilot.scope"],
		stringMapValue(project, "naam"),
		stringMapValue(project, "samenvatting"),
		stringMapValue(workstream, "titel"),
		stringMapValue(workstream, "klant_naam"),
		stringMapValue(workstream, "doel"),
		stringMapValue(workstream, "scope"),
		stringMapValue(workstream, "deliverable"),
	}
	if company != nil {
		parts = append(parts, company.Naam, deref(company.Website), deref(company.Notities))
	}
	haystack := strings.ToLower(strings.Join(parts, " "))
	if strings.Contains(haystack, "henke wonen") || strings.Contains(haystack, "henkewonen") || strings.Contains(haystack, "henke-wonen") {
		return "https://henke-wonen.vercel.app/login"
	}
	return ""
}

func cleanMailURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !isSafeMailCTAURL(value) {
		return ""
	}
	return value
}

func (s *LaventeCareStore) mailAIProject(ctx context.Context, userID string, id *uuid.UUID) (map[string]any, *uuid.UUID, error) {
	if id == nil {
		return nil, nil, nil
	}
	var companyID, leadID *uuid.UUID
	var naam, fase, status string
	var waardeIndicatie *int
	var startDatum, deadline, samenvatting *string
	var createdAt, updatedAt time.Time
	err := s.db.Pool.QueryRow(ctx,
		`SELECT company_id, lead_id, naam, fase, status, waarde_indicatie,
		        start_datum, deadline, samenvatting, created_at, updated_at
		   FROM lc_projects
		  WHERE user_id = $1 AND id = $2
		  LIMIT 1`,
		userID, *id,
	).Scan(&companyID, &leadID, &naam, &fase, &status, &waardeIndicatie,
		&startDatum, &deadline, &samenvatting, &createdAt, &updatedAt)
	if err != nil {
		return nil, nil, err
	}
	return map[string]any{
		"id":                id.String(),
		"company_id":        uuidPtrString(companyID),
		"lead_id":           uuidPtrString(leadID),
		"naam":              naam,
		"fase":              fase,
		"status":            status,
		"waarde_indicatie":  waardeIndicatie,
		"start_datum":       startDatum,
		"deadline":          deadline,
		"samenvatting":      samenvatting,
		"aangemaakt":        createdAt.Format(time.RFC3339),
		"laatst_bijgewerkt": updatedAt.Format(time.RFC3339),
	}, companyID, nil
}

func (s *LaventeCareStore) mailAIWorkstream(ctx context.Context, userID string, id *uuid.UUID) (map[string]any, *uuid.UUID, *uuid.UUID, error) {
	if id == nil {
		return nil, nil, nil, nil
	}
	var companyID, leadID, projectID *uuid.UUID
	var titel, typ, status, prioriteit, bron string
	var klantNaam, sourceID, doel, scope, deliverable, bevindingen, volgendeStap, deadline *string
	var geschatteMinuten, waardeIndicatie *int
	var stackTags, tags []string
	var completedAt *time.Time
	var createdAt, updatedAt time.Time
	err := s.db.Pool.QueryRow(ctx,
		`SELECT company_id, lead_id, project_id, titel, type, status, prioriteit,
		        klant_naam, bron, source_id, doel, scope, deliverable, bevindingen,
		        volgende_stap, deadline, geschatte_minuten, waarde_indicatie,
		        stack_tags, tags, completed_at, created_at, updated_at
		   FROM lc_workstreams
		  WHERE user_id = $1 AND id = $2
		  LIMIT 1`,
		userID, *id,
	).Scan(&companyID, &leadID, &projectID, &titel, &typ, &status, &prioriteit,
		&klantNaam, &bron, &sourceID, &doel, &scope, &deliverable, &bevindingen,
		&volgendeStap, &deadline, &geschatteMinuten, &waardeIndicatie,
		&stackTags, &tags, &completedAt, &createdAt, &updatedAt)
	if err != nil {
		return nil, nil, nil, err
	}
	return map[string]any{
		"id":                id.String(),
		"company_id":        uuidPtrString(companyID),
		"lead_id":           uuidPtrString(leadID),
		"project_id":        uuidPtrString(projectID),
		"titel":             titel,
		"type":              typ,
		"status":            status,
		"prioriteit":        prioriteit,
		"klant_naam":        klantNaam,
		"bron":              bron,
		"source_id":         sourceID,
		"doel":              doel,
		"scope":             scope,
		"deliverable":       deliverable,
		"bevindingen":       bevindingen,
		"volgende_stap":     volgendeStap,
		"deadline":          deadline,
		"geschatte_minuten": geschatteMinuten,
		"waarde_indicatie":  waardeIndicatie,
		"stack_tags":        stackTags,
		"tags":              tags,
		"completed_at":      completedAt,
		"aangemaakt":        createdAt.Format(time.RFC3339),
		"laatst_bijgewerkt": updatedAt.Format(time.RFC3339),
	}, companyID, projectID, nil
}

func (s *LaventeCareStore) mailAIQuote(ctx context.Context, userID string, id *uuid.UUID) (map[string]any, *uuid.UUID, error) {
	if id == nil {
		return nil, nil, nil
	}
	quote, err := s.GetQuote(ctx, userID, *id)
	if err != nil {
		return nil, nil, err
	}
	return map[string]any{
		"id":                quote.ID.String(),
		"company_id":        uuidPtrString(quote.CompanyID),
		"project_id":        uuidPtrString(quote.ProjectID),
		"workstream_id":     uuidPtrString(quote.WorkstreamID),
		"quote_number":      quote.QuoteNumber,
		"titel":             quote.Titel,
		"status":            quote.Status,
		"issue_date":        quote.IssueDate,
		"valid_until":       quote.ValidUntil,
		"currency":          quote.Currency,
		"total":             centsDisplay(quote.Currency, quote.TotalCents),
		"notes":             quote.Notes,
		"company_name":      quote.CompanyName,
		"project_name":      quote.ProjectName,
		"workstream_title":  quote.WorkstreamTitle,
		"laatst_bijgewerkt": quote.UpdatedAt.Format(time.RFC3339),
	}, quote.CompanyID, nil
}

func (s *LaventeCareStore) mailAIInvoice(ctx context.Context, userID string, id *uuid.UUID) (map[string]any, *uuid.UUID, error) {
	if id == nil {
		return nil, nil, nil
	}
	invoice, err := s.GetInvoice(ctx, userID, *id)
	if err != nil {
		return nil, nil, err
	}
	return map[string]any{
		"id":                 invoice.ID.String(),
		"company_id":         uuidPtrString(invoice.CompanyID),
		"project_id":         uuidPtrString(invoice.ProjectID),
		"workstream_id":      uuidPtrString(invoice.WorkstreamID),
		"quote_id":           uuidPtrString(invoice.QuoteID),
		"invoice_number":     invoice.InvoiceNumber,
		"status":             invoice.Status,
		"issue_date":         invoice.IssueDate,
		"due_date":           invoice.DueDate,
		"currency":           invoice.Currency,
		"total":              centsDisplay(invoice.Currency, invoice.TotalCents),
		"paid":               centsDisplay(invoice.Currency, invoice.PaidCents),
		"payment_provider":   invoice.PaymentProvider,
		"merchant_reference": invoice.MerchantReference,
		"payment_url":        invoice.PaymentURL,
		"notes":              invoice.Notes,
		"company_name":       invoice.CompanyName,
		"project_name":       invoice.ProjectName,
		"workstream_title":   invoice.WorkstreamTitle,
		"laatst_bijgewerkt":  invoice.UpdatedAt.Format(time.RFC3339),
	}, invoice.CompanyID, nil
}

func (s *LaventeCareStore) mailAINotes(ctx context.Context, userID string, ids, keywords []string) ([]model.LCMailAIContextItem, error) {
	if len(ids) == 0 && len(keywords) == 0 {
		return []model.LCMailAIContextItem{}, nil
	}
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id::text,
		        COALESCE(NULLIF(TRIM(titel), ''), 'Notitie'),
		        COALESCE(deadline::date::text, gewijzigd::date::text),
		        COALESCE(prioriteit, ''),
		        LEFT(TRIM(COALESCE(inhoud, '')), 520)
		   FROM notes
		  WHERE user_id = $1
		    AND is_archived = false
		    AND (
		      business_context_id = ANY($2::text[])
		      OR EXISTS (
		        SELECT 1
		          FROM unnest($3::text[]) q
		         WHERE LOWER(COALESCE(business_context_title, '') || ' ' || COALESCE(titel, '') || ' ' ||
		                     COALESCE(inhoud, '') || ' ' || COALESCE(symbol, '') || ' ' || COALESCE(array_to_string(tags, ' '), ''))
		               LIKE '%' || q || '%'
		      )
		    )
		  ORDER BY is_pinned DESC, COALESCE(triage_flag, false) DESC, gewijzigd DESC
		  LIMIT 12`,
		userID, ids, keywords)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []model.LCMailAIContextItem{}
	for rows.Next() {
		var item model.LCMailAIContextItem
		item.Type = "note"
		if err := rows.Scan(&item.ID, &item.Title, &item.Date, &item.Priority, &item.Summary); err != nil {
			return nil, err
		}
		if isMailAccessContextText(strings.Join([]string{item.Title, item.Summary}, " ")) {
			item.Summary = "Toegangsgegevens/accountcontext aanwezig in gekoppelde notitie. Gevoelige waarden zijn afgeschermd en mogen alleen via het afgesproken veilige kanaal worden gedeeld."
		} else {
			item.Summary = redactMailSensitiveText(item.Summary)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *LaventeCareStore) mailAIAccessNoteExists(ctx context.Context, userID string, ids, keywords []string) (bool, error) {
	keywords = mailAIUsefulKeywords(keywords)
	if len(ids) == 0 && len(keywords) == 0 {
		return false, nil
	}
	var exists bool
	err := s.db.Pool.QueryRow(ctx,
		`SELECT EXISTS (
		   SELECT 1
		     FROM notes
		    WHERE user_id = $1
		      AND is_archived = false
		      AND lower(COALESCE(business_context_title, '') || ' ' || COALESCE(titel, '') || ' ' ||
		                COALESCE(inhoud, '') || ' ' || COALESCE(symbol, '') || ' ' ||
		                COALESCE(array_to_string(tags, ' '), ''))
		          ~ '(account|accounts|login|inlog|toegang|wachtwoord|password|gebruikersnaam|username|portal)'
		      AND (
		        business_context_id = ANY($2::text[])
		        OR EXISTS (
		          SELECT 1
		            FROM unnest($3::text[]) q
		           WHERE lower(COALESCE(business_context_title, '') || ' ' || COALESCE(titel, '') || ' ' ||
		                       COALESCE(inhoud, '') || ' ' || COALESCE(symbol, '') || ' ' ||
		                       COALESCE(array_to_string(tags, ' '), ''))
		                 LIKE '%' || q || '%'
		        )
		      )
		    LIMIT 1
		 )`,
		userID, ids, keywords,
	).Scan(&exists)
	return exists, err
}

func (s *LaventeCareStore) mailAIPilotAccessDetails(ctx context.Context, userID string, ids, keywords []string, loginURL string) (mailAccessDetails, error) {
	keywords = mailAIUsefulKeywords(keywords)
	if len(ids) == 0 && len(keywords) == 0 {
		return mailAccessDetails{}, nil
	}
	rows, err := s.db.Pool.Query(ctx,
		`SELECT COALESCE(titel, ''), COALESCE(inhoud, '')
		   FROM notes
		  WHERE user_id = $1
		    AND is_archived = false
		    AND lower(COALESCE(business_context_title, '') || ' ' || COALESCE(titel, '') || ' ' ||
		              COALESCE(inhoud, '') || ' ' || COALESCE(symbol, '') || ' ' ||
		              COALESCE(array_to_string(tags, ' '), ''))
		        ~ '(account|accounts|login|inlog|toegang|wachtwoord|password|gebruikersnaam|username|portal)'
		    AND (
		      business_context_id = ANY($2::text[])
		      OR EXISTS (
		        SELECT 1
		          FROM unnest($3::text[]) q
		         WHERE lower(COALESCE(business_context_title, '') || ' ' || COALESCE(titel, '') || ' ' ||
		                     COALESCE(inhoud, '') || ' ' || COALESCE(symbol, '') || ' ' ||
		                     COALESCE(array_to_string(tags, ' '), ''))
		               LIKE '%' || q || '%'
		      )
		    )
		  ORDER BY is_pinned DESC, COALESCE(triage_flag, false) DESC, gewijzigd DESC
		  LIMIT 4`,
		userID, ids, keywords,
	)
	if err != nil {
		return mailAccessDetails{}, err
	}
	defer rows.Close()

	credentials := []mailAccessCredential{}
	for rows.Next() {
		var title, content string
		if err := rows.Scan(&title, &content); err != nil {
			return mailAccessDetails{}, err
		}
		credentials = append(credentials, parseMailAccessCredentials(title+"\n"+content)...)
		if len(credentials) >= 8 {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return mailAccessDetails{}, err
	}
	return formatMailAccessDetailsWithLoginURL(credentials, loginURL), nil
}

func (s *LaventeCareStore) mailAIAgenda(ctx context.Context, userID string, ids, keywords []string) ([]model.LCMailAIContextItem, error) {
	if len(ids) == 0 && len(keywords) == 0 {
		return []model.LCMailAIContextItem{}, nil
	}
	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		loc = time.UTC
	}
	today := time.Now().In(loc)
	from := today.AddDate(0, 0, -21).Format("2006-01-02")
	until := today.AddDate(0, 0, 45).Format("2006-01-02")
	rows, err := s.db.Pool.Query(ctx,
		`SELECT event_id,
		        titel,
		        start_datum::text || CASE WHEN COALESCE(start_tijd, '') = '' THEN '' ELSE ' ' || start_tijd END,
		        status,
		        LEFT(TRIM(COALESCE(beschrijving, locatie, '')), 420)
		   FROM personal_events
		  WHERE user_id = $1
		    AND start_datum >= $2::date
		    AND start_datum <= $3::date
		    AND status NOT IN ('VERWIJDERD', 'PendingDelete')
		    AND (
		      business_context_id = ANY($4::text[])
		      OR EXISTS (
		        SELECT 1
		          FROM unnest($5::text[]) q
		         WHERE LOWER(COALESCE(business_context_title, '') || ' ' || titel || ' ' ||
		                     COALESCE(beschrijving, '') || ' ' || COALESCE(locatie, '') || ' ' || COALESCE(symbol, ''))
		               LIKE '%' || q || '%'
		      )
		    )
		  ORDER BY start_datum DESC, COALESCE(start_tijd, '00:00') DESC
		  LIMIT 12`,
		userID, from, until, ids, keywords)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []model.LCMailAIContextItem{}
	for rows.Next() {
		var item model.LCMailAIContextItem
		item.Type = "agenda"
		if err := rows.Scan(&item.ID, &item.Title, &item.Date, &item.Status, &item.Summary); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *LaventeCareStore) mailAISchedule(ctx context.Context, userID string) ([]model.LCMailAIContextItem, error) {
	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		loc = time.UTC
	}
	rows, err := s.db.Pool.Query(ctx,
		`SELECT event_id,
		        titel,
		        start_datum::text || CASE WHEN COALESCE(start_tijd, '') = '' THEN '' ELSE ' ' || start_tijd END,
		        status,
		        LEFT(TRIM(COALESCE(shift_type, '') || ' ' || COALESCE(locatie, '') || ' ' || COALESCE(werktijd, '')), 360)
		   FROM schedule
		  WHERE user_id = $1
		    AND start_datum >= $2::date
		  ORDER BY start_datum, start_tijd
		  LIMIT 8`,
		userID, time.Now().In(loc).Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []model.LCMailAIContextItem{}
	for rows.Next() {
		var item model.LCMailAIContextItem
		item.Type = "schedule"
		if err := rows.Scan(&item.ID, &item.Title, &item.Date, &item.Status, &item.Summary); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *LaventeCareStore) mailAIActions(ctx context.Context, userID string, companyID, projectID, workstreamID *uuid.UUID) ([]model.LCMailAIContextItem, error) {
	if companyID == nil && projectID == nil && workstreamID == nil {
		return []model.LCMailAIContextItem{}, nil
	}
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id::text, title, COALESCE(due_date, updated_at::date::text),
		        status, priority, LEFT(TRIM(COALESCE(summary, '')), 420)
		   FROM lc_action_items
		  WHERE user_id = $1
		    AND status NOT IN ('afgerond', 'done', 'gesloten', 'gearchiveerd')
		    AND (
		      ($2::uuid IS NOT NULL AND linked_company_id = $2)
		      OR ($3::uuid IS NOT NULL AND linked_project_id = $3)
		      OR ($4::uuid IS NOT NULL AND linked_workstream_id = $4)
		    )
		  ORDER BY CASE priority WHEN 'hoog' THEN 1 WHEN 'normaal' THEN 2 ELSE 3 END, updated_at DESC
		  LIMIT 12`,
		userID, companyID, projectID, workstreamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []model.LCMailAIContextItem{}
	for rows.Next() {
		var item model.LCMailAIContextItem
		item.Type = "action"
		if err := rows.Scan(&item.ID, &item.Title, &item.Date, &item.Status, &item.Priority, &item.Summary); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *LaventeCareStore) mailAIActivity(ctx context.Context, userID string, companyID, projectID, workstreamID *uuid.UUID) ([]model.LCMailAIContextItem, error) {
	if companyID == nil && projectID == nil && workstreamID == nil {
		return []model.LCMailAIContextItem{}, nil
	}
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id::text, title, occurred_at::date::text, event_type,
		        LEFT(TRIM(COALESCE(body, '')), 460)
		   FROM lc_activity_events
		  WHERE user_id = $1
		    AND (
		      ($2::uuid IS NOT NULL AND company_id = $2)
		      OR ($3::uuid IS NOT NULL AND project_id = $3)
		      OR ($4::uuid IS NOT NULL AND workstream_id = $4)
		    )
		  ORDER BY occurred_at DESC
		  LIMIT 12`,
		userID, companyID, projectID, workstreamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []model.LCMailAIContextItem{}
	for rows.Next() {
		var item model.LCMailAIContextItem
		item.Type = "activity"
		if err := rows.Scan(&item.ID, &item.Title, &item.Date, &item.Status, &item.Summary); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *LaventeCareStore) mailAIBilling(ctx context.Context, userID string, companyID, projectID, workstreamID *uuid.UUID) ([]model.LCMailAIContextItem, error) {
	if companyID == nil && projectID == nil && workstreamID == nil {
		return []model.LCMailAIContextItem{}, nil
	}
	rows, err := s.db.Pool.Query(ctx,
		`SELECT type, id, title, date, status, summary
		   FROM (
		     SELECT 'quote' AS type,
		            id::text AS id,
		            quote_number || ' - ' || titel AS title,
		            issue_date::text AS date,
		            status,
		            CONCAT(currency, ' ', ROUND(total_cents::numeric / 100, 2), ' ', COALESCE(notes, '')) AS summary,
		            updated_at
		       FROM lc_quotes
		      WHERE user_id = $1
		        AND (($2::uuid IS NOT NULL AND company_id = $2)
		          OR ($3::uuid IS NOT NULL AND project_id = $3)
		          OR ($4::uuid IS NOT NULL AND workstream_id = $4))
		     UNION ALL
		     SELECT 'invoice' AS type,
		            id::text AS id,
		            invoice_number AS title,
		            COALESCE(due_date, issue_date)::text AS date,
		            status,
		            CONCAT(currency, ' ', ROUND(total_cents::numeric / 100, 2), ' betaald ', ROUND(paid_cents::numeric / 100, 2), ' ', COALESCE(notes, '')) AS summary,
		            updated_at
		       FROM lc_invoices
		      WHERE user_id = $1
		        AND (($2::uuid IS NOT NULL AND company_id = $2)
		          OR ($3::uuid IS NOT NULL AND project_id = $3)
		          OR ($4::uuid IS NOT NULL AND workstream_id = $4))
		   ) billing
		  ORDER BY updated_at DESC
		  LIMIT 10`,
		userID, companyID, projectID, workstreamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []model.LCMailAIContextItem{}
	for rows.Next() {
		var item model.LCMailAIContextItem
		if err := rows.Scan(&item.Type, &item.ID, &item.Title, &item.Date, &item.Status, &item.Summary); err != nil {
			return nil, err
		}
		item.Type = "billing_" + item.Type
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *LaventeCareStore) mailAIDossier(ctx context.Context, userID string, companyID, projectID, workstreamID *uuid.UUID) ([]model.LCMailAIContextItem, error) {
	if companyID == nil && projectID == nil && workstreamID == nil {
		return []model.LCMailAIContextItem{}, nil
	}
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id::text, titel, generated_at::date::text, context_type,
		        LEFT(TRIM(COALESCE(notes, context_title, template_label, '')), 380)
		   FROM lc_dossier_documents
		  WHERE user_id = $1
		    AND (
		      ($2::uuid IS NOT NULL AND company_id = $2)
		      OR ($3::uuid IS NOT NULL AND project_id = $3)
		      OR ($4::uuid IS NOT NULL AND workstream_id = $4)
		    )
		  ORDER BY generated_at DESC
		  LIMIT 8`,
		userID, companyID, projectID, workstreamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []model.LCMailAIContextItem{}
	for rows.Next() {
		var item model.LCMailAIContextItem
		item.Type = "dossier"
		if err := rows.Scan(&item.ID, &item.Title, &item.Date, &item.Status, &item.Summary); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func mailAIContextKeys(company *model.LCCompany, contact *model.LCContact, project map[string]any, workstream map[string]any) ([]string, []string) {
	ids := []string{}
	keywords := []string{"laventecare"}
	if company != nil {
		ids = append(ids, company.ID.String())
		keywords = append(keywords, company.Naam, deref(company.Website), deref(company.Sector))
	}
	if contact != nil {
		ids = append(ids, contact.ID.String())
		keywords = append(keywords, contact.Naam, deref(contact.Email), deref(contact.Rol))
	}
	if project != nil {
		ids = append(ids, stringMapValue(project, "id"))
		keywords = append(keywords, stringMapValue(project, "naam"), stringMapValue(project, "fase"), stringMapValue(project, "status"))
	}
	if workstream != nil {
		ids = append(ids, stringMapValue(workstream, "id"))
		keywords = append(keywords, stringMapValue(workstream, "titel"), stringMapValue(workstream, "klant_naam"),
			stringMapValue(workstream, "type"), stringMapValue(workstream, "status"), stringMapValue(workstream, "doel"),
			stringMapValue(workstream, "scope"), stringMapValue(workstream, "deliverable"))
		for _, key := range []string{"stack_tags", "tags"} {
			if values, ok := workstream[key].([]string); ok {
				keywords = append(keywords, values...)
			}
		}
	}
	return dedupeNonEmpty(ids), dedupeLowerKeywords(keywords)
}

func mailAIUsefulKeywords(values []string) []string {
	result := []string{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || value == "laventecare" {
			continue
		}
		result = append(result, value)
	}
	return dedupeLowerKeywords(result)
}

func sanitizeMailAIAttachments(items []model.LCMailAIContextAttachment) []model.LCMailAIContextAttachment {
	if len(items) == 0 {
		return []model.LCMailAIContextAttachment{}
	}
	const maxAttachments = 8
	const maxTextChars = 9000
	const maxSummaryChars = 700
	if len(items) > maxAttachments {
		items = items[:maxAttachments]
	}
	out := make([]model.LCMailAIContextAttachment, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(item.ExtractionStatus))
		if status != "ok" && status != "partial" && status != "failed" {
			status = "failed"
		}
		out = append(out, model.LCMailAIContextAttachment{
			Name:             name,
			ContentType:      strings.TrimSpace(item.ContentType),
			Size:             item.Size,
			Pages:            item.Pages,
			ExtractedText:    truncateMailAIText(item.ExtractedText, maxTextChars),
			Summary:          truncateMailAIText(item.Summary, maxSummaryChars),
			ExtractionStatus: status,
		})
	}
	return out
}

func truncateMailAIText(value string, max int) string {
	clean := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if max <= 0 || len(clean) <= max {
		return clean
	}
	return strings.TrimSpace(clean[:max-1]) + "…"
}

func safeStringMap(values map[string]string) map[string]string {
	result := map[string]string{}
	for key, value := range values {
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			result[key] = value
		}
	}
	return result
}

func uuidPtrString(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	value := id.String()
	return &value
}

func uuidPtrFromMapValue(values map[string]any, key string) *uuid.UUID {
	value := stringMapValue(values, key)
	if value == "" {
		return nil
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return nil
	}
	return &id
}

func stringMapValue(values map[string]any, key string) string {
	raw, ok := values[key]
	if !ok || raw == nil {
		return ""
	}
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case *string:
		return deref(value)
	case []string:
		return strings.Join(value, " ")
	case *uuid.UUID:
		if value == nil {
			return ""
		}
		return value.String()
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func dedupeNonEmpty(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func dedupeLowerKeywords(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if len(value) < 3 || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func redactMailSensitiveText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(wachtwoord|password|pass|ww|token|api[-_ ]?key|client[-_ ]?secret|secret|refresh[-_ ]?token|bearer|pincode|pin)\b\s*[:=]\s*[^\s,;]+`),
		regexp.MustCompile(`(?i)\b(authorization)\b\s*[:=]\s*[^\s,;]+`),
	}
	for _, pattern := range patterns {
		value = pattern.ReplaceAllString(value, "$1: [afgeschermd]")
	}
	return value
}

type mailAccessCredential struct {
	LoginURL string
	Email    string
	Username string
	Password string
	Role     string
}

type mailAccessDetails struct {
	Intro     string
	Summary   string
	BlockHTML string
}

var (
	mailAccessLabelPattern   = `account\s+email|e-?\s*mail|email|mail|login\s+url|portal\s+url|omgeving\s+url|gebruikersnaam|username|accountnaam|wachtwoord|password|pass|ww|rechten\s+rol|rechten|rol|role|portal|url`
	mailAccessLabelValueRe   = regexp.MustCompile(`(?i)(` + mailAccessLabelPattern + `)\s*[:=]`)
	mailAccessIndexedLabelRe = regexp.MustCompile(`(?i)(^|\s)(\d+)\.\s+(` + mailAccessLabelPattern + `)\s*[:=]`)
)

func parseMailAccessCredentials(value string) []mailAccessCredential {
	credentials := []mailAccessCredential{}
	current := mailAccessCredential{}
	flush := func() {
		if current.LoginURL == "" && current.Email == "" && current.Username == "" && current.Password == "" && current.Role == "" {
			return
		}
		credentials = append(credentials, current)
		current = mailAccessCredential{}
	}

	for _, rawLine := range strings.Split(value, "\n") {
		line := strings.TrimSpace(strings.TrimLeft(rawLine, "-* "))
		if line == "" {
			continue
		}
		for _, pair := range splitMailAccessLabelValues(line) {
			key, lineValue := pair[0], pair[1]
			if lineValue == "" {
				continue
			}
			switch key {
			case "email":
				if current.Email != "" || current.Username != "" || current.Password != "" || current.Role != "" {
					flush()
				}
				current.Email = lineValue
			case "username":
				if current.Username != "" && (current.Email != "" || current.Password != "" || current.Role != "") {
					flush()
				}
				current.Username = lineValue
			case "password":
				current.Password = lineValue
			case "role":
				current.Role = lineValue
			case "url":
				if current.LoginURL != "" && (current.Email != "" || current.Username != "" || current.Password != "" || current.Role != "") {
					flush()
				}
				current.LoginURL = lineValue
			}
		}
	}
	flush()
	if len(credentials) > 8 {
		return credentials[:8]
	}
	return credentials
}

func splitMailAccessLabelValues(line string) [][2]string {
	line = strings.TrimSpace(line)
	line = mailAccessIndexedLabelRe.ReplaceAllString(line, " $3:")
	matches := mailAccessLabelValueRe.FindAllStringSubmatchIndex(line, -1)
	if len(matches) == 0 {
		key, value, ok := splitMailAccessLabel(line)
		if !ok {
			return nil
		}
		return [][2]string{{key, value}}
	}
	pairs := make([][2]string, 0, len(matches))
	for index, match := range matches {
		if len(match) < 4 {
			continue
		}
		label := line[match[2]:match[3]]
		key, ok := canonicalMailAccessLabel(label)
		if !ok {
			continue
		}
		end := len(line)
		if index+1 < len(matches) {
			end = matches[index+1][0]
		}
		value := cleanMailAccessValue(line[match[1]:end])
		if value == "" {
			continue
		}
		pairs = append(pairs, [2]string{key, value})
	}
	return pairs
}

func cleanMailAccessValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimLeft(value, "-–—;|,. \t")
	value = strings.TrimRight(value, "-–—;|, \t")
	return strings.TrimSpace(value)
}

func splitMailAccessLabel(line string) (string, string, bool) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		parts = strings.SplitN(line, "=", 2)
	}
	if len(parts) != 2 {
		return "", "", false
	}
	key, ok := canonicalMailAccessLabel(parts[0])
	value := cleanMailAccessValue(parts[1])
	if !ok {
		return "", "", false
	}
	return key, value, true
}

func canonicalMailAccessLabel(label string) (string, bool) {
	key := strings.ToLower(strings.TrimSpace(label))
	key = strings.ReplaceAll(key, "-", "")
	key = strings.ReplaceAll(key, "_", "")
	key = strings.Join(strings.Fields(key), " ")
	switch key {
	case "e mail", "email", "mail", "account email":
		return "email", true
	case "gebruikersnaam", "username", "user", "login", "account", "accountnaam":
		return "username", true
	case "wachtwoord", "password", "pass", "ww":
		return "password", true
	case "rol", "role", "rechten", "rechten rol":
		return "role", true
	case "login url", "url", "portal", "portal url", "omgeving url":
		return "url", true
	default:
		return "", false
	}
}

func formatMailAccessCredentials(credentials []mailAccessCredential) string {
	return formatMailAccessCredentialsText(credentials)
}

func formatMailAccessDetailsWithLoginURL(credentials []mailAccessCredential, loginURL string) mailAccessDetails {

	credentials = withMailAccessLoginURL(credentials, loginURL)
	summary := formatMailAccessCredentialsText(credentials)
	if summary == "" {
		return mailAccessDetails{}
	}
	return mailAccessDetails{
		Intro:     "pilotaccounts staan klaar voor de testfase",
		Summary:   summary,
		BlockHTML: formatMailAccessCredentialsHTML(credentials),
	}
}

func withMailAccessLoginURL(credentials []mailAccessCredential, loginURL string) []mailAccessCredential {
	loginURL = strings.TrimSpace(loginURL)
	if loginURL == "" {
		return credentials
	}
	result := make([]mailAccessCredential, 0, len(credentials))
	for _, credential := range credentials {
		if strings.TrimSpace(credential.LoginURL) == "" {
			credential.LoginURL = loginURL
		}
		result = append(result, credential)
	}
	return result
}

func formatMailAccessCredentialsText(credentials []mailAccessCredential) string {
	if len(credentials) == 0 {
		return ""
	}
	lines := []string{"Pilotaccounts staan klaar voor de testfase."}
	for index, credential := range credentials {
		parts := []string{fmt.Sprintf("Account %d", index+1)}
		if credential.LoginURL != "" {
			parts = append(parts, "- Login URL: "+credential.LoginURL)
		}
		if credential.Email != "" {
			parts = append(parts, "- E-mail: "+credential.Email)
		}
		if credential.Username != "" && !strings.EqualFold(credential.Username, credential.Email) {
			parts = append(parts, "- Gebruikersnaam: "+credential.Username)
		}
		if credential.Password != "" {
			parts = append(parts, "- Wachtwoord: "+credential.Password)
		}
		if credential.Role != "" {
			parts = append(parts, "- Rol: "+credential.Role)
		}
		if len(parts) == 1 {
			continue
		}
		lines = append(lines, strings.Join(parts, "\n"))
	}
	if len(lines) == 1 {
		return ""
	}
	lines = append(lines, "Gebruik deze gegevens alleen voor de pilot/testfase; na afloop trekken we toegang in of zetten we deze om.")
	return strings.Join(lines, "\n")
}

func formatMailAccessCredentialsHTML(credentials []mailAccessCredential) string {
	if len(credentials) == 0 {
		return ""
	}
	var rows strings.Builder
	for index, credential := range credentials {
		titleParts := []string{fmt.Sprintf("Account %d", index+1)}
		if strings.TrimSpace(credential.Role) != "" {
			titleParts = append(titleParts, credential.Role)
		}
		fields := strings.Builder{}
		fields.WriteString(mailAccessFieldHTML("E-mail", credential.Email, "email"))
		if credential.Username != "" && !strings.EqualFold(credential.Username, credential.Email) {
			fields.WriteString(mailAccessFieldHTML("Gebruikersnaam", credential.Username, "text"))
		}
		fields.WriteString(mailAccessFieldHTML("Wachtwoord", credential.Password, "secret"))
		fields.WriteString(mailAccessFieldHTML("Rol", credential.Role, "text"))
		fields.WriteString(mailAccessFieldHTML("Login URL", credential.LoginURL, "url"))
		if fields.Len() == 0 {
			continue
		}
		rows.WriteString(fmt.Sprintf(
			`<tr><td style="padding:0 0 10px 0;">
  <table role="presentation" cellpadding="0" cellspacing="0" width="100%%" style="background:#ffffff;border:1px solid #dbeafe;border-radius:10px;">
    <tr><td style="padding:13px 14px 4px 14px;font-size:12px;line-height:1.45;font-weight:900;letter-spacing:.7px;text-transform:uppercase;color:#0369a1;">%s</td></tr>
    <tr><td style="padding:0 14px 12px 14px;">%s</td></tr>
  </table>
</td></tr>`,
			escapeMailText(strings.Join(titleParts, " · ")),
			fields.String(),
		))
	}
	if rows.Len() == 0 {
		return ""
	}
	return fmt.Sprintf(
		`<tr><td style="padding:0 28px 26px 28px;">
  <table role="presentation" cellpadding="0" cellspacing="0" width="100%%" style="background:#f8fafc;border:1px solid #cbd5e1;border-left:4px solid #0891b2;border-radius:12px;">
    <tr><td style="padding:16px 18px 6px 18px;">
      <div style="font-size:11px;line-height:1.4;font-weight:900;letter-spacing:1.2px;text-transform:uppercase;color:#0f766e;">Pilotaccounts</div>
      <div style="margin-top:5px;font-size:13px;line-height:1.55;color:#64748b;">Gebruik deze gegevens alleen voor de pilot/testfase.</div>
    </td></tr>
    <tr><td style="padding:4px 18px 4px 18px;"><table role="presentation" cellpadding="0" cellspacing="0" width="100%%">%s</table></td></tr>
    <tr><td style="padding:4px 18px 16px 18px;font-size:12px;line-height:1.55;color:#64748b;">Na afloop trekken we toegang in of zetten we deze om.</td></tr>
  </table>
</td></tr>`,
		rows.String(),
	)
}

func mailAccessFieldHTML(label, value, kind string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	display := escapeMailText(value)
	if kind == "email" && strings.Contains(value, "@") {
		display = fmt.Sprintf(`<a href="%s" style="color:#0369a1;font-weight:800;text-decoration:none;">%s</a>`, escapeMailAttr("mailto:"+value), escapeMailText(value))
	}
	if kind == "url" && strings.HasPrefix(strings.ToLower(value), "http") {
		display = fmt.Sprintf(`<a href="%s" style="color:#0369a1;font-weight:800;text-decoration:none;">%s</a>`, escapeMailAttr(value), escapeMailText(value))
	}
	valueStyle := "font-size:14px;line-height:1.55;color:#0f172a;word-break:break-word;"
	if kind == "secret" {
		display = fmt.Sprintf(`<span style="display:inline-block;margin-top:2px;background:#e2e8f0;border:1px solid #cbd5e1;border-radius:8px;padding:7px 9px;font-family:ui-monospace,SFMono-Regular,Consolas,'Liberation Mono',monospace;font-size:13px;line-height:1.35;color:#0f172a;word-break:break-all;">%s</span>`, escapeMailText(value))
	}
	return fmt.Sprintf(
		`<table role="presentation" cellpadding="0" cellspacing="0" width="100%%" style="border-top:1px solid #e2e8f0;">
  <tr>
    <td width="120" valign="top" style="padding:8px 10px 8px 0;font-size:12px;line-height:1.45;font-weight:800;color:#64748b;">%s</td>
    <td valign="top" style="padding:8px 0;%s">%s</td>
  </tr>
</table>`,
		escapeMailText(label),
		valueStyle,
		display,
	)
}

func applyMailAccessDetails(values map[string]string, details mailAccessDetails) {
	setMailValue(values, "pilot.access_intro", details.Intro)
	setMailValue(values, "pilot.access_summary", details.Summary)
	setMailValue(values, "pilot.access_block_html", details.BlockHTML)
}

func normalizeMailAccessVariables(values map[string]string) {
	if values == nil {
		return
	}
	summary := strings.TrimSpace(values["pilot.access_summary"])
	if summary == "" {
		return
	}
	credentials := parseMailAccessCredentials(summary)
	if len(credentials) == 0 {
		if strings.TrimSpace(values["pilot.access_intro"]) == "" {
			setMailValue(values, "pilot.access_intro", summary)
		}
		return
	}
	applyMailAccessDetails(values, formatMailAccessDetailsWithLoginURL(credentials, values["pilot.login_url"]))
}

func isMailAccessContextText(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false
	}
	accessWords := []string{
		"account",
		"accounts",
		"login",
		"inlog",
		"toegang",
		"wachtwoord",
		"password",
		"gebruikersnaam",
		"username",
		"portal",
	}
	for _, word := range accessWords {
		if strings.Contains(value, word) {
			return true
		}
	}
	return false
}

func centsDisplay(currency string, cents int) string {
	currency = valueOr(strings.TrimSpace(currency), "EUR")
	return fmt.Sprintf("%s %.2f", currency, float64(cents)/100)
}

func mailOutboxSelectSQL() string {
	return `SELECT m.id, m.user_id, m.template_id, m.company_id, m.contact_id, m.project_id,
		        m.workstream_id, m.quote_id, m.invoice_id, m.to_email, m.to_name, m.cc, m.bcc,
		        m.subject, m.body_html, m.body_text, m.status, m.provider, m.provider_message_id,
		        m.conversation_id, m.error_message, m.sent_at, m.created_at, m.updated_at,
		        t.name, c.naam, ct.naam
		   FROM lc_mail_outbox m
		   LEFT JOIN lc_mail_templates t ON t.id = m.template_id AND t.user_id = m.user_id
		   LEFT JOIN lc_companies c ON c.id = m.company_id AND c.user_id = m.user_id
		   LEFT JOIN lc_contacts ct ON ct.id = m.contact_id AND ct.user_id = m.user_id`
}

func scanMailTemplate(row pgx.CollectableRow) (model.LCMailTemplate, error) {
	var t model.LCMailTemplate
	err := row.Scan(&t.ID, &t.UserID, &t.TemplateKey, &t.Name, &t.Category,
		&t.Status, &t.SubjectTemplate, &t.BodyHTML, &t.BodyText, &t.DefaultCC,
		&t.DefaultBCC, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

func scanMailOutboxItem(row pgx.CollectableRow) (model.LCMailOutboxItem, error) {
	var m model.LCMailOutboxItem
	err := row.Scan(&m.ID, &m.UserID, &m.TemplateID, &m.CompanyID, &m.ContactID,
		&m.ProjectID, &m.WorkstreamID, &m.QuoteID, &m.InvoiceID, &m.ToEmail,
		&m.ToName, &m.CC, &m.BCC, &m.Subject, &m.BodyHTML, &m.BodyText,
		&m.Status, &m.Provider, &m.ProviderMessageID, &m.ConversationID, &m.ErrorMessage,
		&m.SentAt, &m.CreatedAt, &m.UpdatedAt, &m.TemplateName,
		&m.CompanyName, &m.ContactName)
	return m, err
}

type mailTemplateContent struct {
	Preheader   string
	Eyebrow     string
	Title       string
	Greeting    string
	Intro       string
	Body        string
	FocusTitle  string
	FocusItems  []string
	ExtraHTML   string
	CTAURL      string
	CTALabel    string
	ClosingLine string
}

func brandedMailHTML(content mailTemplateContent) string {
	const logoURL = "https://ik.imagekit.io/a0oim4e3e/tr:f-png,w-112/LaventeCare/logo.svg?updatedAt=1779275051433"
	focusRows := ""
	if content.FocusTitle != "" || len(content.FocusItems) > 0 {
		var items strings.Builder
		for _, item := range content.FocusItems {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			items.WriteString(fmt.Sprintf(
				`<tr><td style="padding:7px 0 7px 0;border-bottom:1px solid #e2e8f0;">
  <table role="presentation" cellpadding="0" cellspacing="0" width="100%%"><tr>
    <td width="18" valign="top" style="padding-top:3px;"><span style="display:block;width:7px;height:7px;border-radius:999px;background:#0891b2;"></span></td>
    <td style="font-size:14px;line-height:1.55;color:#334155;">%s</td>
  </tr></table>
</td></tr>`,
				escapeMailText(item),
			))
		}
		focusRows = fmt.Sprintf(
			`<tr><td style="padding:0 28px 26px 28px;">
  <table role="presentation" cellpadding="0" cellspacing="0" width="100%%" style="background:#f8fafc;border:1px solid #e2e8f0;border-left:4px solid #0891b2;border-radius:10px;">
    <tr><td style="padding:16px 18px 5px 18px;font-size:11px;line-height:1.4;font-weight:800;letter-spacing:1.2px;text-transform:uppercase;color:#0f766e;">%s</td></tr>
    <tr><td style="padding:0 18px 12px 18px;"><table role="presentation" cellpadding="0" cellspacing="0" width="100%%">%s</table></td></tr>
  </table>
</td></tr>`,
			escapeMailText(valueOr(content.FocusTitle, "Belangrijk")),
			items.String(),
		)
	}

	cta := ""
	if strings.TrimSpace(content.CTAURL) != "" && strings.TrimSpace(content.CTALabel) != "" {
		cta = fmt.Sprintf(
			`<tr><td align="center" style="padding:2px 28px 30px 28px;">
  <a href="%s" target="_blank" style="display:inline-block;background:#059669;border:1px solid #047857;border-radius:9px;color:#ffffff;font-size:14px;font-weight:800;line-height:1.1;padding:14px 22px;text-decoration:none;">%s</a>
</td></tr>`,
			escapeMailAttr(content.CTAURL),
			escapeMailText(content.CTALabel),
		)
	}

	return fmt.Sprintf(`<!doctype html>
<html lang="nl">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <meta http-equiv="x-ua-compatible" content="ie=edge">
  <title>%s</title>
</head>
<body style="margin:0;padding:0;background:#f1f5f9;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;color:#0f172a;">
  <!-- laventecare-mail-shell:v2 -->
  <div style="display:none;max-height:0;overflow:hidden;opacity:0;color:transparent;">%s</div>
  <table role="presentation" cellpadding="0" cellspacing="0" width="100%%" style="background:#f1f5f9;">
    <tr>
      <td align="center" style="padding:30px 14px;">
        <table role="presentation" cellpadding="0" cellspacing="0" width="100%%" style="max-width:620px;background:#ffffff;border-radius:16px;overflow:hidden;border:1px solid #e2e8f0;box-shadow:0 18px 45px rgba(15,23,42,.10);">
          <tr>
            <td style="background:#0a1628;padding:22px 28px;">
              <table role="presentation" cellpadding="0" cellspacing="0" width="100%%">
                <tr>
                  <td valign="middle" width="64" style="width:64px;padding-right:14px;">
                    <img src="%s" width="54" alt="LaventeCare" style="display:block;width:54px;max-width:54px;height:auto;border:0;outline:none;text-decoration:none;">
                  </td>
                  <td valign="middle">
                    <div style="font-size:21px;font-weight:900;letter-spacing:-.25px;color:#f8fafc;">Lavente<span style="color:#22d3ee;">Care</span></div>
                    <div style="margin-top:4px;font-size:11px;font-weight:800;letter-spacing:1.35px;text-transform:uppercase;color:#bae6fd;">{{laventecare.tagline}}</div>
                  </td>
                  <td align="right" valign="middle" style="font-size:11px;font-weight:800;letter-spacing:1.25px;text-transform:uppercase;color:#34d399;">AI · Automatisering · Websystemen</td>
                </tr>
              </table>
            </td>
          </tr>
          <tr>
            <td style="background:#0f1e35;padding:26px 28px 28px 28px;border-top:1px solid #1e3a52;">
              <div style="font-size:11px;font-weight:800;letter-spacing:1.5px;text-transform:uppercase;color:#22d3ee;">%s</div>
              <div style="margin-top:8px;font-size:28px;line-height:1.15;font-weight:900;letter-spacing:-.7px;color:#f0f9ff;">%s</div>
            </td>
          </tr>
          <tr>
            <td style="padding:28px 28px 10px 28px;">
              <p style="margin:0 0 16px 0;font-size:15px;line-height:1.65;color:#334155;">%s</p>
              <p style="margin:0 0 16px 0;font-size:15px;line-height:1.65;color:#334155;">%s</p>
              <p style="margin:0 0 8px 0;font-size:15px;line-height:1.65;color:#475569;">%s</p>
            </td>
          </tr>
          %s
          %s
          %s
          <tr>
            <td style="padding:0 28px 28px 28px;">
              <p style="margin:0 0 18px 0;font-size:15px;line-height:1.65;color:#334155;">%s</p>
              <p style="margin:0;font-size:15px;line-height:1.65;color:#334155;">Met vriendelijke groet,<br><strong style="color:#0f172a;">Jeffrey Lavente</strong><br><span style="color:#64748b;">LaventeCare</span></p>
            </td>
          </tr>
          <tr>
            <td style="background:#f8fafc;border-top:1px solid #e2e8f0;padding:20px 28px;">
              <p style="margin:0 0 6px 0;text-align:center;font-size:12px;line-height:1.55;color:#64748b;">
                <a href="mailto:{{laventecare.email}}" style="color:#0891b2;font-weight:800;text-decoration:none;">{{laventecare.email}}</a>
                <span style="color:#cbd5e1;"> · </span>
                <a href="https://www.laventecare.nl" style="color:#0891b2;font-weight:800;text-decoration:none;">laventecare.nl</a>
              </p>
              <p style="margin:0;text-align:center;font-size:11px;line-height:1.55;color:#94a3b8;">LaventeCare · KVK 88162710 · Dronten, Nederland</p>
            </td>
          </tr>
        </table>
      </td>
    </tr>
  </table>
</body>
</html>`,
		escapeMailText(valueOr(content.Title, "LaventeCare")),
		escapeMailText(valueOr(content.Preheader, "Bericht van LaventeCare")),
		escapeMailAttr(logoURL),
		escapeMailText(valueOr(content.Eyebrow, "LaventeCare")),
		escapeMailText(valueOr(content.Title, "Nieuw bericht")),
		escapeMailText(valueOr(content.Greeting, "Beste {{contact.naam}},")),
		escapeMailText(content.Intro),
		escapeMailText(content.Body),
		focusRows,
		content.ExtraHTML,
		cta,
		escapeMailText(content.ClosingLine),
	)
}

func escapeMailText(value string) string {
	return strings.ReplaceAll(html.EscapeString(value), "\n", "<br>")
}

func escapeMailAttr(value string) string {
	return html.EscapeString(strings.TrimSpace(value))
}

func renderTemplate(input string, values map[string]string) string {
	result := input
	for key, value := range values {
		result = strings.ReplaceAll(result, "{{"+key+"}}", value)
		result = strings.ReplaceAll(result, "{{ "+key+" }}", value)
	}
	return result
}

// rawMailHTMLKeys lists the substitution keys whose value is trusted, server-built
// HTML and must NOT be escaped when rendered into a mail body. Everything else —
// CRM fields, the free-text variable editor, AI/Grok drafts, PDF-extracted text —
// is treated as untrusted plain text and HTML-escaped on render.
var rawMailHTMLKeys = map[string]bool{
	// Built server-side from per-field-escaped credential values (mailAccessFieldHTML);
	// force-reset to "" before user/AI input is merged, so it can never carry injected markup.
	"pilot.access_block_html": true,
}

// renderMailHTML substitutes {{key}} tokens into an HTML mail body, HTML-escaping
// every value (so a company name like "Jansen & Zn", a "Scope <fase 2>" workstream,
// or an AI/PDF-injected "<img onerror=...>" can never corrupt the layout or inject
// markup into the email a real client receives). The only raw slot is the
// server-built access block (rawMailHTMLKeys). Use renderTemplate (no escaping) for
// the subject and plain-text body, which are not HTML.
func renderMailHTML(input string, values map[string]string) string {
	result := input
	for key, value := range values {
		rendered := value
		if !rawMailHTMLKeys[key] {
			rendered = escapeMailText(value)
		}
		result = strings.ReplaceAll(result, "{{"+key+"}}", rendered)
		result = strings.ReplaceAll(result, "{{ "+key+" }}", rendered)
	}
	return result
}

var (
	mailTemplateTokenRe = regexp.MustCompile(`\{\{\s*[a-zA-Z0-9_.-]+\s*\}\}`)
	mailCTAButtonRe     = regexp.MustCompile(`(?is)<tr>\s*<td\s+align="center"[^>]*>\s*<a\s+href="([^"]*)"[^>]*>.*?</a>\s*</td>\s*</tr>`)
)

func cleanupRenderedMailSubject(value string) string {
	value = mailTemplateTokenRe.ReplaceAllString(value, "")
	return strings.Join(strings.Fields(value), " ")
}

func cleanupRenderedMailHTML(value string) string {
	value = mailCTAButtonRe.ReplaceAllStringFunc(value, func(block string) string {
		match := mailCTAButtonRe.FindStringSubmatch(block)
		if len(match) < 2 || !isSafeMailCTAURL(match[1]) {
			return ""
		}
		return block
	})
	return mailTemplateTokenRe.ReplaceAllString(value, "")
}

func cleanupRenderedMailText(value string) string {
	lines := strings.Split(value, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if strings.Contains(trimmed, "{{") || strings.Contains(trimmed, "}}") {
			continue
		}
		if strings.HasPrefix(lower, "betalen kan via:") && !strings.Contains(lower, "http://") && !strings.Contains(lower, "https://") {
			continue
		}
		if strings.HasPrefix(lower, "- pilotomgeving:") && !strings.Contains(lower, "http://") && !strings.Contains(lower, "https://") {
			continue
		}
		cleaned = append(cleaned, line)
	}
	result := strings.TrimSpace(strings.Join(cleaned, "\n"))
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}
	return result
}

func isSafeMailCTAURL(value string) bool {
	value = strings.TrimSpace(html.UnescapeString(value))
	lower := strings.ToLower(value)
	if value == "" || strings.Contains(value, "{{") || strings.Contains(value, "}}") {
		return false
	}
	if strings.TrimRight(lower, "/") == "https://www.laventecare.nl/contact" || strings.TrimRight(lower, "/") == "http://www.laventecare.nl/contact" {
		return false
	}
	return strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://")
}

func setMailValue(values map[string]string, key, value string) {
	value = strings.TrimSpace(value)
	if value == "" || value == "<nil>" {
		return
	}
	values[key] = value
}

func isDefaultPilotAccessSummary(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return normalized == "" ||
		normalized == "via het afgesproken veilige kanaal" ||
		normalized == "toegang via het afgesproken veilige kanaal" ||
		normalized == "pilottoegang stemmen we voor de start af via het afgesproken kanaal" ||
		normalized == "pilottoegang stem ik voor de start af via het afgesproken kanaal" ||
		normalized == "pilotaccounts staan klaar; gevoelige inloggegevens deel ik via het afgesproken veilige kanaal"
}

func joinMailParts(values []string, separator string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || value == "<nil>" {
			continue
		}
		parts = append(parts, value)
	}
	return strings.Join(parts, separator)
}

func cleanEmails(values []string) []string {
	seen := map[string]bool{}
	emails := make([]string, 0, len(values))
	for _, value := range values {
		email := strings.ToLower(strings.TrimSpace(value))
		if email == "" || seen[email] {
			continue
		}
		seen[email] = true
		emails = append(emails, email)
	}
	return emails
}

func nullableEmails(values []string) any {
	if values == nil {
		return nil
	}
	return cleanEmails(values)
}

func mergeEmails(a, b []string) []string {
	return cleanEmails(append(append([]string{}, a...), b...))
}

func mailStrPtr(value string) *string {
	return &value
}

func valueOr(value, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func laventeCareMailConfiguredFromEnv() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("LAVENTECARE_MAIL_ENABLED")), "true") &&
		strings.TrimSpace(os.Getenv("MICROSOFT_TENANT_ID")) != "" &&
		strings.TrimSpace(os.Getenv("MICROSOFT_CLIENT_ID")) != "" &&
		strings.TrimSpace(os.Getenv("MICROSOFT_CLIENT_SECRET")) != "" &&
		strings.TrimSpace(os.Getenv("MICROSOFT_SENDER_EMAIL")) != ""
}
