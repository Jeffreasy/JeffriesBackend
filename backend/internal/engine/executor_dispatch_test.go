package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/Jeffreasy/JeffriesBackend/internal/ai"
)

// TestEveryToolHasExecutorCase guards against a tool being advertised + policied
// but missing from the executor switch, which would return the
// "niet geïmplementeerd" default as a tool result the model has to explain away.
//
// The executor is built with a nil pool: an implemented tool will reach real
// work and may panic on the nil DB (recovered here) — that still proves a case
// exists. Only an unknown tool name hits the default branch, which is what we
// assert against.
func TestEveryToolHasExecutorCase(t *testing.T) {
	exec := NewHomeBotExecutor(nil, "test-user")
	for _, tool := range ai.AllTools {
		name := tool.Function.Name
		t.Run(name, func(t *testing.T) {
			var result string
			func() {
				defer func() { _ = recover() }()
				result = exec.Execute(context.Background(), name, "{}")
			}()
			if strings.Contains(result, "niet geïmplementeerd") {
				t.Errorf("tool %q has a schema/policy but no executor case", name)
			}
		})
	}
}
