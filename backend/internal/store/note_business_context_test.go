package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type fakeBusinessContextQuerier struct {
	title string
	err   error
	query string
	args  []any
}

func (q *fakeBusinessContextQuerier) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	q.query = query
	q.args = args
	return fakeBusinessContextRow{title: q.title, err: q.err}
}

type fakeBusinessContextRow struct {
	title string
	err   error
}

func (r fakeBusinessContextRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*string)) = r.title
	return nil
}

func TestNormalizeBusinessContextResolvesOwnedTitle(t *testing.T) {
	id := uuid.New()
	contextType, contextID, suppliedTitle := " Contact ", " "+id.String()+" ", "verzonnen naam"
	querier := &fakeBusinessContextQuerier{title: "  Canonieke naam  "}

	gotType, gotID, gotTitle, err := normalizeBusinessContext(
		context.Background(), querier, "owner", &contextType, &contextID, &suppliedTitle,
	)
	if err != nil {
		t.Fatalf("normalizeBusinessContext() error = %v", err)
	}
	if gotType == nil || *gotType != "contact" || gotID == nil || *gotID != id.String() || gotTitle == nil || *gotTitle != "Canonieke naam" {
		t.Fatalf("normalized triplet = %v/%v/%v", gotType, gotID, gotTitle)
	}
	if !strings.Contains(querier.query, "FROM contacts") || len(querier.args) != 2 || querier.args[0] != "owner" || querier.args[1] != id {
		t.Fatalf("ownership query = %q args=%v", querier.query, querier.args)
	}
}

func TestNormalizeBusinessContextRejectsMalformedOrUnownedReference(t *testing.T) {
	id := uuid.NewString()
	cases := []struct {
		name         string
		contextType  *string
		contextID    *string
		contextTitle *string
		querier      *fakeBusinessContextQuerier
		want         error
	}{
		{name: "unknown type", contextType: stringPointer("crm"), contextID: &id, querier: &fakeBusinessContextQuerier{}, want: ErrInvalidBusinessContext},
		{name: "missing id", contextType: stringPointer("contact"), querier: &fakeBusinessContextQuerier{}, want: ErrInvalidBusinessContext},
		{name: "id without type", contextID: &id, querier: &fakeBusinessContextQuerier{}, want: ErrInvalidBusinessContext},
		{name: "other owner looks missing", contextType: stringPointer("contact"), contextID: &id, querier: &fakeBusinessContextQuerier{err: pgx.ErrNoRows}, want: ErrBusinessContextNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := normalizeBusinessContext(context.Background(), tc.querier, "owner", tc.contextType, tc.contextID, tc.contextTitle)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestNormalizeBusinessContextCanonicalizesGenericLaventeCare(t *testing.T) {
	contextType, suppliedTitle := "LAVENTECARE", "niet vertrouwen"
	typ, id, title, err := normalizeBusinessContext(
		context.Background(), &fakeBusinessContextQuerier{}, "owner", &contextType, nil, &suppliedTitle,
	)
	if err != nil {
		t.Fatalf("normalizeBusinessContext() error = %v", err)
	}
	if typ == nil || *typ != "laventecare" || id != nil || title == nil || *title != "LaventeCare" {
		t.Fatalf("normalized triplet = %v/%v/%v", typ, id, title)
	}
}

func TestMergedBusinessContextTreatsTripletAtomically(t *testing.T) {
	oldType, oldID, oldTitle := "contact", uuid.NewString(), "Oude naam"

	changedType, changedID, _ := mergedBusinessContext(&oldType, &oldID, &oldTitle, map[string]any{
		"business_context_type": "laventecare_project",
	})
	if changedType == nil || *changedType != "laventecare_project" || changedID != nil {
		t.Fatalf("type-only change reused stale id: type=%v id=%v", changedType, changedID)
	}

	clearedType, clearedID, clearedTitle := mergedBusinessContext(&oldType, &oldID, &oldTitle, map[string]any{
		"business_context_type": nil,
	})
	if clearedType != nil || clearedID != nil || clearedTitle != nil {
		t.Fatalf("clear retained context: %v/%v/%v", clearedType, clearedID, clearedTitle)
	}
}

func TestListWithOptionsRejectsMalformedContextFilterBeforeQuery(t *testing.T) {
	store := &NoteStore{}
	for _, opts := range []NoteListOptions{
		{ContextID: uuid.NewString()},
		{ContextType: "onbekend"},
		{ContextType: "laventecare", ContextID: uuid.NewString()},
	} {
		if _, err := store.ListWithOptions(context.Background(), "owner", opts); !errors.Is(err, ErrInvalidBusinessContext) {
			t.Fatalf("ListWithOptions(%+v) error = %v, want ErrInvalidBusinessContext", opts, err)
		}
	}
}

func stringPointer(value string) *string { return &value }
