package telegram

import (
	"strings"
	"testing"
)

func TestSplitForTelegramShortTextUnchanged(t *testing.T) {
	chunks := splitForTelegram("korte tekst")
	if len(chunks) != 1 || chunks[0] != "korte tekst" {
		t.Fatalf("expected single unchanged chunk, got %v", chunks)
	}
}

func TestSplitForTelegramLongTextSplitsAtNewline(t *testing.T) {
	// Build a message well over telegramChunkByteTarget, with a newline
	// placed so the first break should land there rather than mid-word.
	para := strings.Repeat("a", telegramChunkByteTarget-10) + "\n" + strings.Repeat("b", telegramChunkByteTarget)
	chunks := splitForTelegram(para)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if escapedByteLen(c) > telegramChunkByteTarget {
			t.Fatalf("chunk %d exceeds target size: %d escaped bytes", i, escapedByteLen(c))
		}
	}
	if strings.Contains(chunks[0], "b") {
		t.Fatalf("expected first chunk to break at the newline before the b's, got tail: %q", chunks[0][len(chunks[0])-20:])
	}
}

// TestSplitForTelegramMultiByteRunesStayUnderByteBudget pins down the actual
// production bug: chunking by RUNE count let a chunk full of multi-byte
// runes (€=3 bytes each) balloon to ~3x telegramChunkByteTarget in bytes,
// which escapeAndCapForTelegram then silently truncated at send time —
// reintroducing the exact data loss this rework exists to prevent.
func TestSplitForTelegramMultiByteRunesStayUnderByteBudget(t *testing.T) {
	// No newlines at all: must still split into byte-safe chunks under the target.
	text := strings.Repeat("€", telegramChunkByteTarget) // 3 bytes/rune — old rune-based target would blow this 3x over budget
	chunks := splitForTelegram(text)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	var totalRunes int
	for i, c := range chunks {
		if escapedByteLen(c) > telegramChunkByteTarget {
			t.Fatalf("chunk %d exceeds byte budget: %d escaped bytes", i, escapedByteLen(c))
		}
		totalRunes += len([]rune(c))
	}
	if want := len([]rune(text)); totalRunes != want {
		t.Fatalf("expected no runes lost/duplicated across chunks, got %d want %d", totalRunes, want)
	}
}

// TestSplitForTelegramChunksSurviveEscapeAndCap is the regression test for
// the actual SendMessage path: every chunk splitForTelegram produces must
// pass through escapeAndCapForTelegram (as SendMessage does) WITHOUT being
// truncated. A rune-budgeted splitter can pass this file's other tests while
// still losing content at send time on multi-byte-heavy input — this test
// is what would have caught that.
func TestSplitForTelegramChunksSurviveEscapeAndCap(t *testing.T) {
	inputs := []string{
		strings.Repeat("€", 20000),                                   // dense multi-byte, no boundaries
		strings.Repeat("Saldo: €1.234,56 op rekening NL00INGB\n", 800), // realistic dense finance-report shape
		strings.Repeat("&", 10000),                                   // heaviest HTML-escape expansion (5 bytes each)
	}
	for _, text := range inputs {
		for i, chunk := range splitForTelegram(text) {
			// A chunk that already fits within telegramHardLimit before
			// escaping never needs escapeAndCapForTelegram's fallback
			// truncation — assert that directly, which is exactly what a
			// rune-budgeted (instead of byte-budgeted) splitter would fail.
			if got := len(escapeHTML(chunk)); got > telegramHardLimit {
				t.Fatalf("chunk %d of input len=%d needs truncation at send time: escaped %d bytes > hard limit %d", i, len(text), got, telegramHardLimit)
			}
		}
	}
}

func TestEscapeAndCapForTelegramNeverExceedsHardLimit(t *testing.T) {
	// Pathological input: every character expands 5x on escape (&amp;).
	// Old behavior (truncate raw, then escape) could push this over 4096.
	raw := strings.Repeat("&", telegramChunkByteTarget)
	got := escapeAndCapForTelegram(raw)
	if len(got) > telegramHardLimit {
		t.Fatalf("escaped+capped text exceeds telegramHardLimit: %d bytes", len(got))
	}
}

func TestEscapeAndCapForTelegramRuneSafeTruncation(t *testing.T) {
	raw := strings.Repeat("é", telegramHardLimit) // 2-byte rune in UTF-8
	got := escapeAndCapForTelegram(raw)
	if len(got) > telegramHardLimit {
		t.Fatalf("result exceeds hard limit: %d bytes", len(got))
	}
	if !isValidUTF8Tail(got) {
		t.Fatalf("truncation split a multi-byte rune: %q", got[len(got)-10:])
	}
}

func isValidUTF8Tail(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}
