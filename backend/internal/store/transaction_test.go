package store

import (
	"errors"
	"testing"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

func validTransactionImport() model.TransactionImport {
	return model.TransactionImport{
		RekeningIban: "NL91ABNA0417164300",
		Volgnr:       "10",
		Datum:        "2026-07-17",
		Bedrag:       12.34,
		SaldoNaTrn:   56.78,
	}
}

func TestValidateTransactionImport(t *testing.T) {
	if _, err := validateTransactionImport(validTransactionImport()); err != nil {
		t.Fatalf("valid import rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*model.TransactionImport)
	}{
		{name: "missing iban", mutate: func(v *model.TransactionImport) { v.RekeningIban = " " }},
		{name: "missing sequence", mutate: func(v *model.TransactionImport) { v.Volgnr = "" }},
		{name: "non-numeric sequence", mutate: func(v *model.TransactionImport) { v.Volgnr = "12a" }},
		{name: "impossible date", mutate: func(v *model.TransactionImport) { v.Datum = "2026-02-30" }},
		{name: "timestamp is not a bank date", mutate: func(v *model.TransactionImport) { v.Datum = "2026-07-17T10:00:00Z" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			item := validTransactionImport()
			tc.mutate(&item)
			if _, err := validateTransactionImport(item); !errors.Is(err, ErrInvalidTransactionImport) {
				t.Fatalf("error = %v, want ErrInvalidTransactionImport", err)
			}
		})
	}
}

func TestVolgnrLessUsesNumericSemantics(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{a: "9", b: "10", want: true},
		{a: "10", b: "9", want: false},
		{a: "0009", b: "10", want: true},
		{a: "0010", b: "10", want: false},
	}
	for _, tc := range tests {
		if got := volgnrLess(tc.a, tc.b); got != tc.want {
			t.Errorf("volgnrLess(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
