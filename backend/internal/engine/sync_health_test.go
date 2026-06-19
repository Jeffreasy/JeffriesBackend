package engine

import (
	"testing"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

func TestShouldFireAlert(t *testing.T) {
	e := &Engine{}
	if !e.shouldFireAlert("google-reauth", time.Hour) {
		t.Fatal("first alert should fire")
	}
	if e.shouldFireAlert("google-reauth", time.Hour) {
		t.Fatal("second alert within window must be suppressed")
	}
	if !e.shouldFireAlert("other-key", time.Hour) {
		t.Fatal("a different key should fire independently")
	}
}

func TestEmailSyncStatusHelpers(t *testing.T) {
	if got := emailSyncStatus(nil); got != "unknown" {
		t.Fatalf("nil meta status = %q, want unknown", got)
	}
	if got := emailSyncStatus(&model.EmailSyncMeta{}); got != "unknown" {
		t.Fatalf("empty status = %q, want unknown", got)
	}
	if got := emailSyncStatus(&model.EmailSyncMeta{SyncStatus: "failed"}); got != "failed" {
		t.Fatalf("status = %q, want failed", got)
	}
	if got := emailSyncStatus(&model.EmailSyncMeta{SyncStatus: "ok"}); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	if got := emailSyncLastError(nil); got != "" {
		t.Fatalf("nil last error = %q, want empty", got)
	}
	if got := emailSyncLastError(&model.EmailSyncMeta{LastError: "invalid_grant"}); got != "invalid_grant" {
		t.Fatalf("last error = %q, want invalid_grant", got)
	}
}
