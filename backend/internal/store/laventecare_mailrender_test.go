package store

import (
	"strings"
	"testing"
)

func TestRenderMailHTML_EscapesUntrustedValues(t *testing.T) {
	tmpl := `<p>Beste {{contact.naam}},</p><p>{{company.naam}} — {{next_step}}</p>{{pilot.access_block_html}}`
	values := map[string]string{
		"contact.naam": `Jansen & Zn`,
		"company.naam": `<img src=x onerror=alert(1)>`,
		"next_step":    "Regel 1\nRegel 2",
		// trusted, server-built HTML — must survive verbatim
		"pilot.access_block_html": `<table><tr><td>account</td></tr></table>`,
	}

	got := renderMailHTML(tmpl, values)

	// Untrusted markup must be neutralised, not present as live tags.
	if strings.Contains(got, "<img") {
		t.Fatalf("injected <img> reached the rendered body: %q", got)
	}
	if !strings.Contains(got, "&lt;img src=x onerror=alert(1)&gt;") {
		t.Fatalf("company.naam not HTML-escaped: %q", got)
	}
	// Ampersand in a plain CRM field must be escaped so the client sees "Jansen & Zn".
	if !strings.Contains(got, "Jansen &amp; Zn") {
		t.Fatalf("contact.naam ampersand not escaped: %q", got)
	}
	// Newlines in a substituted value become <br> (matches escapeMailText for baked content).
	if !strings.Contains(got, "Regel 1<br>Regel 2") {
		t.Fatalf("newline not converted to <br>: %q", got)
	}
	// The one trusted raw-HTML slot must NOT be escaped.
	if !strings.Contains(got, `<table><tr><td>account</td></tr></table>`) {
		t.Fatalf("trusted access block was escaped: %q", got)
	}
}

func TestRenderMailHTML_PreservesCTAHref(t *testing.T) {
	tmpl := `<a href="{{cta.url}}">Betalen</a>`
	values := map[string]string{"cta.url": "https://pay.example.com/i?a=1&b=2"}

	got := renderMailHTML(tmpl, values)

	// The URL is attribute-escaped (& -> &amp;) — correct HTML for an href — and the
	// post-render CTA-safety check unescapes before validating, so the link survives.
	if !strings.Contains(got, `href="https://pay.example.com/i?a=1&amp;b=2"`) {
		t.Fatalf("CTA href not preserved/escaped correctly: %q", got)
	}
	if !isSafeMailCTAURL("https://pay.example.com/i?a=1&amp;b=2") {
		t.Fatalf("escaped CTA url should still pass the safety check")
	}
}

func TestRenderTemplate_PlainTextStaysRaw(t *testing.T) {
	// Subject and plain-text body are not HTML and must stay unescaped.
	got := renderTemplate("Offerte {{company.naam}}", map[string]string{"company.naam": "Jansen & Zn"})
	if got != "Offerte Jansen & Zn" {
		t.Fatalf("plain-text render should not escape: %q", got)
	}
}
