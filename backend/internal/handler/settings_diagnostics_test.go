package handler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestCachedGrokDiagnosticsUsesCooldown(t *testing.T) {
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	var chatCalls, webCalls atomic.Int32
	h := &SettingsHandler{
		diagnosticsNow: func() time.Time { return now },
		checkGrokChatFn: func(context.Context) aiDiagnosticCheck {
			chatCalls.Add(1)
			return aiDiagnosticCheck{OK: true, Status: "ok", Label: "chat"}
		},
		checkGrokWebFn: func(context.Context) aiDiagnosticCheck {
			webCalls.Add(1)
			return aiDiagnosticCheck{OK: true, Status: "ok", Label: "web"}
		},
	}
	h.cachedGrokDiagnostics(context.Background())
	h.cachedGrokDiagnostics(context.Background())
	if chatCalls.Load() != 1 || webCalls.Load() != 1 {
		t.Fatalf("calls inside cooldown: chat=%d web=%d", chatCalls.Load(), webCalls.Load())
	}
	now = now.Add(aiDiagnosticsCooldown + time.Second)
	h.cachedGrokDiagnostics(context.Background())
	if chatCalls.Load() != 2 || webCalls.Load() != 2 {
		t.Fatalf("calls after expiry: chat=%d web=%d", chatCalls.Load(), webCalls.Load())
	}
}
