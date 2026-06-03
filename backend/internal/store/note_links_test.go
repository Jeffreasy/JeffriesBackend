package store

import (
	"reflect"
	"testing"
)

func TestExtractWikiLinkTitles(t *testing.T) {
	got := extractWikiLinkTitles("Bel [[Project A]] en daarna [[ Project A ]] of [[Agenda]]. Lege [[]] telt niet.")
	want := []string{"Project A", "Agenda"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}
