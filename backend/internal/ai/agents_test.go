package ai

import "testing"

func TestLaventeCareAgentCanReadAgendaAndNotesContext(t *testing.T) {
	for _, toolName := range []string{
		"planningOpvragen",
		"afsprakenOpvragen",
		"notitiesZoeken",
		"notitiesOverzicht",
		"notitiesVandaag",
		"laventecareDossierDocumentenOpvragen",
	} {
		if !IsToolAllowed("laventecare", toolName) {
			t.Fatalf("laventecare should be allowed to use %s", toolName)
		}
	}
}

func TestLaventeCareAgentDoesNotReceiveNoteMutations(t *testing.T) {
	for _, toolName := range []string{
		"notitieAanmaken",
		"notitieBewerken",
		"notitieArchiveren",
	} {
		if IsToolAllowed("laventecare", toolName) {
			t.Fatalf("laventecare should not be allowed to use %s", toolName)
		}
	}
}
