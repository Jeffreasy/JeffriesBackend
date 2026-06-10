package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

func TestResolveDossierAdviceTargetTreatsLaventeCareQueryAsGlobal(t *testing.T) {
	store := &LaventeCareStore{}

	target, _, _, err := store.resolveDossierAdviceTarget(context.Background(), "user-test", model.LCDossierAdviceRequest{
		Query: "laventecare",
	})
	if err != nil {
		t.Fatalf("resolveDossierAdviceTarget returned error: %v", err)
	}
	if target.Kind != "laventecare" {
		t.Fatalf("expected laventecare target for generic query, got %q", target.Kind)
	}
	if target.Title != "LaventeCare" {
		t.Fatalf("expected global title, got %q", target.Title)
	}
}

func TestResolveDossierAdviceTargetRejectsMultipleContexts(t *testing.T) {
	store := &LaventeCareStore{}
	companyID := uuid.New()
	projectID := uuid.New()

	_, _, _, err := store.resolveDossierAdviceTarget(context.Background(), "user-test", model.LCDossierAdviceRequest{
		CompanyID: &companyID,
		ProjectID: &projectID,
	})
	if !errors.Is(err, ErrInvalidDossierAdviceTarget) {
		t.Fatalf("expected ErrInvalidDossierAdviceTarget, got %v", err)
	}
}

func TestBuildDossierRequirementsGlobalContextIsConsistent(t *testing.T) {
	docs := []model.LCDocument{
		testLCDocument("analyse", "Analyse", "commercieel"),
		testLCDocument("pilot", "Pilot", "proces"),
		testLCDocument("sla", "SLA", "governance"),
	}
	presentByKey := map[string]model.LCDossierDocument{
		"analyse": {ID: uuid.New(), DocumentKey: "analyse"},
	}

	requirements := buildDossierRequirements(docs, presentByKey, model.LCDossierAdviceTarget{
		Kind:     "laventecare",
		Title:    "LaventeCare",
		Subtitle: "algemene bedrijfscontext",
	}, lcDossierAdviceRelations{})

	context := findDossierRequirement(t, requirements, "business_context")
	if context.Status != "ok" {
		t.Fatalf("expected global business context ok, got %q", context.Status)
	}
	if strings.Contains(strings.ToLower(context.Reason), "company_id") || strings.Contains(strings.ToLower(context.Reason), "koppel dit advies") {
		t.Fatalf("global business context should not show target warning, got %q", context.Reason)
	}
}

func TestBuildDossierRequirementsTargetWithoutCompanyNeedsAttention(t *testing.T) {
	requirements := buildDossierRequirements([]model.LCDocument{
		testLCDocument("analyse", "Analyse", "commercieel"),
	}, map[string]model.LCDossierDocument{}, model.LCDossierAdviceTarget{
		Kind:  "project",
		Title: "Los project",
	}, lcDossierAdviceRelations{})

	context := findDossierRequirement(t, requirements, "customer_context")
	if context.Status != "attention" {
		t.Fatalf("expected missing company context to need attention, got %q", context.Status)
	}
	if !strings.Contains(strings.ToLower(context.Reason), "klantdossier") {
		t.Fatalf("expected klantdossier guidance, got %q", context.Reason)
	}
}

func TestDossierRequirementIssueCounts(t *testing.T) {
	missing, attention := dossierRequirementIssueCounts([]model.LCDossierRequirement{
		{Status: "ok"},
		{Status: "attention"},
		{Status: "missing"},
		{Status: "attention"},
	})
	if missing != 1 || attention != 2 {
		t.Fatalf("issue counts = missing %d attention %d, want 1/2", missing, attention)
	}
}

func testLCDocument(key, title, category string) model.LCDocument {
	return model.LCDocument{
		ID:          uuid.New(),
		DocumentKey: key,
		Titel:       title,
		Categorie:   category,
		Versie:      "1.0",
	}
}

func findDossierRequirement(t *testing.T, requirements []model.LCDossierRequirement, key string) model.LCDossierRequirement {
	t.Helper()
	for _, requirement := range requirements {
		if requirement.Key == key {
			return requirement
		}
	}
	t.Fatalf("requirement %q not found in %#v", key, requirements)
	return model.LCDossierRequirement{}
}
