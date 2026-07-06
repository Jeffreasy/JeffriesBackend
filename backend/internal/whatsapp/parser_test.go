package whatsapp

import "testing"

func TestParse_iOSAndroidMixedFormats(t *testing.T) {
	export := "‎[06-07-2026, 14:32:15] Jeffrey: Hoi mam!\n" +
		"Tweede regel van hetzelfde bericht\n" +
		"[06-07-2026, 14:33:01] Mama: Hallo lieverd\n" +
		"[06-07-2026, 14:34:00] Mama: ‎<Media weggelaten>\n"

	res := Parse(export)

	if len(res.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(res.Messages))
	}
	if res.Messages[0].Sender != "Jeffrey" || res.Messages[0].Body != "Hoi mam!\nTweede regel van hetzelfde bericht" {
		t.Errorf("message 0 wrong: %+v", res.Messages[0])
	}
	if res.Messages[0].SentAt == nil || res.Messages[0].SentAt.Year() != 2026 || res.Messages[0].SentAt.Hour() != 14 {
		t.Errorf("message 0 timestamp not parsed: %+v", res.Messages[0].SentAt)
	}
	if res.Messages[2].Kind != "media" {
		t.Errorf("expected media kind, got %q", res.Messages[2].Kind)
	}
	if len(res.Participants) != 2 || res.Participants[0] != "Jeffrey" || res.Participants[1] != "Mama" {
		t.Errorf("participants wrong: %v", res.Participants)
	}
}

func TestParse_AndroidDashAndSystemLine(t *testing.T) {
	export := "06-07-2026 14:30 - Berichten en oproepen zijn end-to-end versleuteld.\n" +
		"06-07-2026 14:31 - Piet: Yo\n"

	res := Parse(export)
	if len(res.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(res.Messages))
	}
	if res.Messages[0].Kind != "system" || res.Messages[0].Sender != "" {
		t.Errorf("expected system line, got %+v", res.Messages[0])
	}
	if res.Messages[1].Sender != "Piet" || res.Messages[1].Body != "Yo" {
		t.Errorf("expected Piet message, got %+v", res.Messages[1])
	}
	if len(res.Participants) != 1 || res.Participants[0] != "Piet" {
		t.Errorf("participants wrong: %v", res.Participants)
	}
}
