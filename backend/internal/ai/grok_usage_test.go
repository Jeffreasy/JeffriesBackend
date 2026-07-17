package ai

import "testing"

func TestAddUsageAccumulatesAllRounds(t *testing.T) {
	total := &Usage{}
	addUsage(total, Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12})
	addUsage(total, Usage{PromptTokens: 20, CompletionTokens: 3, TotalTokens: 23})
	addUsage(total, Usage{PromptTokens: 30, CompletionTokens: 4, TotalTokens: 34})
	if total.PromptTokens != 60 || total.CompletionTokens != 9 || total.TotalTokens != 69 {
		t.Fatalf("aggregated usage = %+v", total)
	}
}
