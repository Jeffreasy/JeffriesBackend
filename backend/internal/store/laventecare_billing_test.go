package store

import (
	"testing"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

func TestQuoteLineConversionKeepsFixedPriceSemantics(t *testing.T) {
	line := model.LCQuoteLine{
		Description:     "Twee vaste deliverables",
		Quantity:        2,
		UnitAmountCents: 5000,
		TotalCents:      10000,
	}
	got := quoteLineToFlatInvoiceLine(line, 2100, 1)
	if got.QuantityMinutes != 0 {
		t.Fatalf("quote quantity was mislabeled as %d minutes", got.QuantityMinutes)
	}
	if got.UnitAmountCents != line.TotalCents || got.TotalCents != line.TotalCents {
		t.Fatalf("flat amount changed: %#v", got)
	}
}
