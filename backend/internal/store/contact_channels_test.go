package store

import "testing"

func TestNormalizeChannelKind(t *testing.T) {
	cases := map[string]string{
		"email":    "email",
		"E-Mail":   "email",
		"telefoon": "phone",
		"Mobiel":   "phone",
		"fax":      "other",
		"":         "other",
	}
	for in, want := range cases {
		if got := normalizeChannelKind(in); got != want {
			t.Errorf("normalizeChannelKind(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeInteractionKind(t *testing.T) {
	cases := map[string]string{
		"gebeld":   "call",
		"afspraak": "meeting",
		"whatsapp": "message",
		"mail":     "email",
		"":         "note",
		"borrel":   "other",
	}
	for in, want := range cases {
		if got := normalizeInteractionKind(in); got != want {
			t.Errorf("normalizeInteractionKind(%q) = %q, want %q", in, got, want)
		}
	}
}
