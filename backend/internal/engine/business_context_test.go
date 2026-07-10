package engine

import (
	"testing"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/google/uuid"
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

func TestExplicitContactMentionRequiresUnambiguousAtSyntax(t *testing.T) {
	jan := model.Contact{ID: uuid.New(), DisplayName: "Jan"}
	janJansen := model.Contact{ID: uuid.New(), DisplayName: "Jan Jansen"}
	anne := model.Contact{ID: uuid.New(), DisplayName: "Anne van Dijk"}
	contacts := []model.Contact{jan, janJansen, anne}

	cases := []struct {
		name  string
		text  string
		want  uuid.UUID
		found bool
	}{
		{name: "bracket name", text: "Bespreek dit met @[Anne van Dijk].", want: anne.ID, found: true},
		{name: "longest exact name", text: "Bel @Jan Jansen morgen", want: janJansen.ID, found: true},
		{name: "single word", text: "Vraag het aan @Jan!", want: jan.ID, found: true},
		{name: "ordinary prose", text: "Jan Jansen moet dit zien", found: false},
		{name: "email is not mention", text: "Mail naar mail@Jan.nl", found: false},
		{name: "bracket after email local part", text: "Geen relatie: mail@[Anne van Dijk]", found: false},
		{name: "multiple people", text: "Vraag @Jan en @[Anne van Dijk]", found: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := explicitContactMention(tc.text, contacts)
			if ok != tc.found {
				t.Fatalf("found = %v, want %v (contact=%+v)", ok, tc.found, got)
			}
			if ok && got.ID != tc.want {
				t.Fatalf("contact = %s, want %s", got.ID, tc.want)
			}
		})
	}
}

func TestExplicitContactMentionRejectsDuplicateDisplayName(t *testing.T) {
	contacts := []model.Contact{
		{ID: uuid.New(), DisplayName: "Sam"},
		{ID: uuid.New(), DisplayName: "Sam"},
	}
	if got, ok := explicitContactMention("Vraag @Sam", contacts); ok {
		t.Fatalf("duplicate name should be ambiguous, got %+v", got)
	}
}
