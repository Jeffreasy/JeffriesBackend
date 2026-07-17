package store

import (
	"strings"
	"testing"

	"github.com/Jeffreasy/JeffriesBackend/internal/whatsapp"
)

func TestBuildWhatsAppSummaryMarksGroupContext(t *testing.T) {
	messages := []whatsapp.Message{
		{Sender: "Jeffrey", Body: "Hoi", Kind: "text"},
		{Sender: "Mama", Body: "Hoi", Kind: "text"},
		{Sender: "Piet", Body: "Hoi", Kind: "text"},
	}
	group := buildWhatsAppSummary("Familie", true, messages)
	if !strings.Contains(group, "WhatsApp-groepsgesprek") {
		t.Fatalf("group context missing from summary: %s", group)
	}
	direct := buildWhatsAppSummary("Mama", false, messages[:2])
	if !strings.Contains(direct, "WhatsApp-1-op-1-gesprek") {
		t.Fatalf("direct context missing from summary: %s", direct)
	}
}
