package handler

import (
	"net/http/httptest"
	"testing"
)

func TestQueryIntBoundsListLimits(t *testing.T) {
	tests := []struct {
		query    string
		key      string
		fallback int
		want     int
	}{
		{query: "", key: "limit", fallback: 30, want: 30},
		{query: "?limit=0", key: "limit", fallback: 30, want: 30},
		{query: "?limit=-1", key: "limit", fallback: 30, want: 30},
		{query: "?limit=not-a-number", key: "limit", fallback: 30, want: 30},
		{query: "?limit=150", key: "limit", fallback: 30, want: 150},
		{query: "?limit=100000", key: "limit", fallback: 30, want: 200},
		{query: "?skip=999", key: "skip", fallback: 0, want: 999},
		{query: "?skip=100000", key: "skip", fallback: 0, want: 10000},
		{query: "?offset=0", key: "offset", fallback: 0, want: 0},
		{query: "?offset=10000", key: "offset", fallback: 0, want: 10000},
		{query: "?offset=10001", key: "offset", fallback: 0, want: 10000},
	}
	for _, tc := range tests {
		r := httptest.NewRequest("GET", "/items"+tc.query, nil)
		if got := queryInt(r, tc.key, tc.fallback); got != tc.want {
			t.Errorf("queryInt(%q, %q) = %d, want %d", tc.query, tc.key, got, tc.want)
		}
	}
}

func TestContactListOptionsContract(t *testing.T) {
	defaults := contactListOptions(httptest.NewRequest("GET", "/contacts", nil))
	if defaults.Limit != 200 || defaults.Offset != 0 {
		t.Fatalf("defaults = limit %d offset %d, want 200/0", defaults.Limit, defaults.Offset)
	}

	bounded := contactListOptions(httptest.NewRequest("GET", "/contacts?q=Alice&limit=999&offset=10001&type=friend&includeArchived=TRUE", nil))
	if bounded.Query != "Alice" || bounded.RelationshipType != "friend" || !bounded.IncludeArchived {
		t.Fatalf("filters were not preserved: %+v", bounded)
	}
	if bounded.Limit != 200 || bounded.Offset != 10000 {
		t.Fatalf("bounds = limit %d offset %d, want 200/10000", bounded.Limit, bounded.Offset)
	}
}
