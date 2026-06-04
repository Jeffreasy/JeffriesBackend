package engine

import (
	"testing"

	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

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

func TestParseArgsCoercesAIToolArgumentTypes(t *testing.T) {
	var habitArgs struct {
		Dagen int `json:"dagen"`
	}
	if err := (&HomeBotExecutor{}).parseArgs(`{"dagen":"30"}`, &habitArgs); err != nil {
		t.Fatalf("parseArgs() habit error = %v", err)
	}
	if habitArgs.Dagen != 30 {
		t.Fatalf("Dagen = %d, want 30", habitArgs.Dagen)
	}

	var financeArgs struct {
		Jaar  string `json:"jaar"`
		Maand string `json:"maand"`
		Limit int    `json:"limit"`
	}
	if err := (&HomeBotExecutor{}).parseArgs(`{"jaar":2026,"maand":6,"limit":"5"}`, &financeArgs); err != nil {
		t.Fatalf("parseArgs() finance error = %v", err)
	}
	if financeArgs.Jaar != "2026" || financeArgs.Maand != "6" || financeArgs.Limit != 5 {
		t.Fatalf("financeArgs = %+v, want jaar=2026 maand=6 limit=5", financeArgs)
	}

	var progressArgs struct {
		Waarde *float64 `json:"waarde"`
	}
	if err := (&HomeBotExecutor{}).parseArgs(`{"waarde":"1.5"}`, &progressArgs); err != nil {
		t.Fatalf("parseArgs() progress error = %v", err)
	}
	if progressArgs.Waarde == nil || *progressArgs.Waarde != 1.5 {
		t.Fatalf("Waarde = %v, want 1.5", progressArgs.Waarde)
	}

	var customDaysArgs struct {
		AangepasteDagen []int32 `json:"aangepaste_dagen"`
	}
	if err := (&HomeBotExecutor{}).parseArgs(`{"aangepaste_dagen":["1","3"]}`, &customDaysArgs); err != nil {
		t.Fatalf("parseArgs() custom days error = %v", err)
	}
	if len(customDaysArgs.AangepasteDagen) != 2 || customDaysArgs.AangepasteDagen[0] != 1 || customDaysArgs.AangepasteDagen[1] != 3 {
		t.Fatalf("AangepasteDagen = %+v, want [1 3]", customDaysArgs.AangepasteDagen)
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

func TestApplyFinancePeriodFilterAcceptsYearAndNumericMonth(t *testing.T) {
	var filter store.TransactionFilter
	if err := applyFinancePeriodFilter(&filter, "2026", "6"); err != nil {
		t.Fatalf("applyFinancePeriodFilter() error = %v", err)
	}
	if filter.DatumVan != "2026-06-01" || filter.DatumTot != "2026-06-30" {
		t.Fatalf("range = %s..%s, want 2026-06-01..2026-06-30", filter.DatumVan, filter.DatumTot)
	}
}
