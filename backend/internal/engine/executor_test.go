package engine

import "testing"

func TestParseArgsAcceptsEmptyToolArguments(t *testing.T) {
	executor := &HomeBotExecutor{}

	for _, input := range []string{"", "   ", "null"} {
		var args struct {
			Limit int `json:"limit"`
		}
		if err := executor.parseArgs(input, &args); err != nil {
			t.Fatalf("parseArgs(%q) error = %v", input, err)
		}
		if args.Limit != 0 {
			t.Fatalf("parseArgs(%q) Limit = %d, want default 0", input, args.Limit)
		}
	}
}

func TestParseArgsParsesProvidedToolArguments(t *testing.T) {
	var args struct {
		Limit int `json:"limit"`
	}

	if err := (&HomeBotExecutor{}).parseArgs(`{"limit":7}`, &args); err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	if args.Limit != 7 {
		t.Fatalf("Limit = %d, want 7", args.Limit)
	}
}

func TestParseToolDateRangeAcceptsNullArguments(t *testing.T) {
	start, end, hasRange, err := parseToolDateRange("null", true)
	if err != nil {
		t.Fatalf("parseToolDateRange() error = %v", err)
	}
	if !hasRange {
		t.Fatal("expected fallback today range")
	}
	if start == "" || end == "" || start != end {
		t.Fatalf("unexpected fallback range: start=%q end=%q", start, end)
	}
}
