package handler

import (
	"strings"
	"testing"
)

func TestValidDossierDocumentKey(t *testing.T) {
	if !validDossierDocumentKey("pilot.quickscan-v2") {
		t.Fatal("normal exact key rejected")
	}
	for _, value := range []string{"", "unsafe\nkey", strings.Repeat("x", 201)} {
		if validDossierDocumentKey(value) {
			t.Fatalf("invalid key accepted: %q", value)
		}
	}
}
