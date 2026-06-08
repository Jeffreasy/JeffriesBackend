package engine

import (
	"testing"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

func TestScoreCompanyContextMatchRecognizesCompactNamesAndWebsite(t *testing.T) {
	website := "https://www.henkewonen.nl/"
	company := &model.LCCompany{
		Naam:    "Henke Wonen",
		Website: &website,
	}

	cases := []string{
		"Maak een pilotnotitie voor HenkeWonen.",
		"Plan overleg over henkewonen.nl accounts.",
		"Start testfase voor Henke Wonen.",
	}

	for _, input := range cases {
		text := normalizeLCContextText(input)
		score := scoreCompanyContextMatch(text, compactLCContextText(text), company)
		if score < 75 {
			t.Fatalf("expected %q to match company context, got score %d", input, score)
		}
	}
}

func TestScoreCompanyContextMatchIgnoresGenericAliases(t *testing.T) {
	company := &model.LCCompany{Naam: "Klant"}
	text := normalizeLCContextText("Maak een klantnotitie zonder concrete bedrijfsnaam.")

	if score := scoreCompanyContextMatch(text, compactLCContextText(text), company); score != 0 {
		t.Fatalf("expected generic company alias to be ignored, got score %d", score)
	}
}
