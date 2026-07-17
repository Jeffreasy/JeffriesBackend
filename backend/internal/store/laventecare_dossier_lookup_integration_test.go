package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestListDossierDocumentsByKeyIsExactLatestAndOwnerScoped(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	db, err := New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := EnsureRuntimeSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	userID := "dossier-lookup-" + uuid.NewString()
	otherUser := "other-" + uuid.NewString()
	key := "pilot.quickscan-" + uuid.NewString()
	olderID, newestID, otherID := uuid.New(), uuid.New(), uuid.New()
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO lc_dossier_documents (id,user_id,document_key,titel,pdf_url,created_at)
		VALUES ($1,$2,$3,'older','/older.pdf',$4),
		       ($5,$2,$3,'newest','/newest.pdf',$6),
		       ($7,$8,$3,'other owner','/other.pdf',$6)`,
		olderID, userID, key, time.Now().Add(-time.Hour), newestID, time.Now(), otherID, otherUser)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool.Exec(context.Background(), `DELETE FROM lc_dossier_documents WHERE id=ANY($1)`, []uuid.UUID{olderID, newestID, otherID})
	})

	docs, err := NewLaventeCareStore(db).ListDossierDocumentsByKey(ctx, userID, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].ID != newestID {
		t.Fatalf("lookup = %+v, want only newest owner-scoped match %s", docs, newestID)
	}
	none, err := NewLaventeCareStore(db).ListDossierDocumentsByKey(ctx, userID, key+"-not-exact")
	if err != nil || len(none) != 0 {
		t.Fatalf("non-exact lookup = %+v, err=%v", none, err)
	}
}
