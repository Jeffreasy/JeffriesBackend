package google

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateRunesKeepsUTF8Valid(t *testing.T) {
	input := strings.Repeat("a", 499) + "€" + "tail"

	got := truncateRunes(input, 500)

	if !utf8.ValidString(got) {
		t.Fatalf("truncateRunes returned invalid UTF-8")
	}
	if utf8.RuneCountInString(got) != 500 {
		t.Fatalf("truncateRunes rune count = %d, want 500", utf8.RuneCountInString(got))
	}
	if !strings.HasSuffix(got, "€") {
		t.Fatalf("truncateRunes cut the multi-byte rune incorrectly")
	}
}

func TestTruncateRunesReplacesInvalidBytes(t *testing.T) {
	input := string([]byte{'o', 'k', 0xe2})

	got := truncateRunes(input, 10)

	if !utf8.ValidString(got) {
		t.Fatalf("truncateRunes returned invalid UTF-8")
	}
}
