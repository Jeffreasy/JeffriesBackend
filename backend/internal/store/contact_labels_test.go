package store

import "testing"

func TestNormalizeLabelColor(t *testing.T) {
	cases := map[string]string{
		"amber":   "amber",
		"  Sky  ": "sky",
		"EMERALD": "emerald",
		"":        "slate",
		"hotpink": "slate", // not in palette → default
		"#ff0000": "slate", // raw CSS rejected
	}
	for in, want := range cases {
		if got := NormalizeLabelColor(in); got != want {
			t.Errorf("NormalizeLabelColor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeLabelName(t *testing.T) {
	cases := map[string]string{
		"  VIP  klant ":  "VIP klant",
		"hardloopmaatje": "hardloopmaatje",
		"a\t\tb  c":      "a b c",
		"   ":            "",
	}
	for in, want := range cases {
		if got := normalizeLabelName(in); got != want {
			t.Errorf("normalizeLabelName(%q) = %q, want %q", in, got, want)
		}
	}
}
