package handler

import "testing"

func TestValidateTransactionDateRange(t *testing.T) {
	tests := []struct {
		name, year, from, to string
		wantError            bool
	}{
		{name: "empty", wantError: false},
		{name: "valid", year: "2026", from: "2026-01-01", to: "2026-12-31", wantError: false},
		{name: "bad year length", year: "26", wantError: true},
		{name: "bad year characters", year: "20x6", wantError: true},
		{name: "impossible from", from: "2026-02-30", wantError: true},
		{name: "timestamp", to: "2026-07-17T10:00:00Z", wantError: true},
		{name: "reversed", from: "2026-07-18", to: "2026-07-17", wantError: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := validateTransactionDateRange(tc.year, tc.from, tc.to)
			if (got != "") != tc.wantError {
				t.Fatalf("message = %q, wantError %v", got, tc.wantError)
			}
		})
	}
}
