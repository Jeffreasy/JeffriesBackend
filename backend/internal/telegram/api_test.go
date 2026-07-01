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
	// Build a message well over telegramChunkTarget, with a newline placed
	// so the first break should land there rather than mid-word.
	para := strings.Repeat("a", telegramChunkTarget-10) + "\n" + strings.Repeat("b", telegramChunkTarget)
	chunks := splitForTelegram(para)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if len([]rune(c)) > telegramChunkTarget {
			t.Fatalf("chunk %d exceeds target size: %d runes", i, len([]rune(c)))
		}
	}
	if strings.Contains(chunks[0], "b") {
		t.Fatalf("expected first chunk to break at the newline before the b's, got tail: %q", chunks[0][len(chunks[0])-20:])
	}
}

func TestSplitForTelegramNoGoodBoundaryHardCutsRuneSafe(t *testing.T) {
	// No newlines at all: must still split into rune-safe chunks under the target.
	text := strings.Repeat("€", telegramChunkTarget*2) // multi-byte rune, stresses byte-vs-rune handling
	chunks := splitForTelegram(text)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	var total int
	for _, c := range chunks {
		total += len([]rune(c))
		if len([]rune(c)) > telegramChunkTarget {
			t.Fatalf("chunk exceeds target size: %d runes", len([]rune(c)))
		}
	}
	if total != telegramChunkTarget*2 {
		t.Fatalf("expected no runes lost/duplicated across chunks, got %d want %d", total, telegramChunkTarget*2)
	}
}

func TestEscapeAndCapForTelegramNeverExceedsHardLimit(t *testing.T) {
	// Pathological input: every character expands 4x on escape (&amp;).
	// Old behavior (truncate raw, then escape) could push this over 4096.
	raw := strings.Repeat("&", telegramChunkTarget)
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
