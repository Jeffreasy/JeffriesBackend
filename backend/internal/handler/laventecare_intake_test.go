package handler

import (
	"testing"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

func TestPublicIntakePayloadHashIgnoresSubmittedAt(t *testing.T) {
	base := model.LCPublicIntakeRequest{
		RequestID:   "req-12345678",
		Source:      "laventecare.nl",
		Name:        "Test Persoon",
		Email:       "test@example.com",
		Goal:        "Een betrouwbaar klantportaal",
		SubmittedAt: "2026-07-17T10:00:00Z",
	}
	first, err := publicIntakePayloadHash(base)
	if err != nil {
		t.Fatal(err)
	}

	retry := base
	retry.SubmittedAt = "2026-07-17T10:00:03Z"
	second, err := publicIntakePayloadHash(retry)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("transport timestamp changed semantic hash: %q != %q", first, second)
	}

	changed := base
	changed.Goal = "Een inhoudelijk ander project"
	third, err := publicIntakePayloadHash(changed)
	if err != nil {
		t.Fatal(err)
	}
	if first == third {
		t.Fatal("business-field change must produce a different idempotency hash")
	}
}
func TestNormalizePublicIntakeRejectsUnsafeURLsAndTimestamp(t *testing.T) {
	valid := func() model.LCPublicIntakeRequest {
		return model.LCPublicIntakeRequest{
			RequestID: "req-12345678",
			Name:      "Test Persoon",
			Email:     "test@example.com",
		}
	}
	tests := []struct {
		name   string
		mutate func(*model.LCPublicIntakeRequest)
	}{
		{"javascript website", func(in *model.LCPublicIntakeRequest) { in.Website = "javascript:alert(1)" }},
		{"relative page URL", func(in *model.LCPublicIntakeRequest) { in.PageURL = "/contact" }},
		{"origin with path", func(in *model.LCPublicIntakeRequest) { in.Origin = "https://example.com/contact" }},
		{"invalid submittedAt", func(in *model.LCPublicIntakeRequest) { in.SubmittedAt = "17-07-2026 10:00" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := valid()
			tc.mutate(&in)
			if err := normalizePublicIntake(&in); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestNormalizePublicIntakeAcceptsAbsoluteHTTPURLs(t *testing.T) {
	in := model.LCPublicIntakeRequest{
		RequestID:   "req-12345678",
		Name:        "Test Persoon",
		Email:       "test@example.com",
		Website:     "https://Example.COM/product",
		PageURL:     "http://example.com/contact?from=home",
		Origin:      "https://Example.COM:443",
		SubmittedAt: "2026-07-17T12:00:00+02:00",
	}
	if err := normalizePublicIntake(&in); err != nil {
		t.Fatalf("expected valid intake: %v", err)
	}
	if in.Origin != "https://example.com:443" {
		t.Fatalf("origin normalization = %q", in.Origin)
	}
	if in.SubmittedAt != "2026-07-17T10:00:00Z" {
		t.Fatalf("submittedAt normalization = %q", in.SubmittedAt)
	}
}
