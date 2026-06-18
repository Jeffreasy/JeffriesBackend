package ai

import "testing"

// TestEveryToolHasPolicy guards against schema/policy drift: a tool advertised
// to the model but missing from Policies is unreachable (IsToolAllowed returns
// false), so the model is told about a tool it can never use.
func TestEveryToolHasPolicy(t *testing.T) {
	for _, tool := range AllTools {
		if _, ok := Policies[tool.Function.Name]; !ok {
			t.Errorf("tool %q is in AllTools but has no Policies entry", tool.Function.Name)
		}
	}
}

// TestEveryPolicyHasTool guards the other direction: a policy with no matching
// tool definition is dead configuration.
func TestEveryPolicyHasTool(t *testing.T) {
	names := make(map[string]bool, len(AllTools))
	for _, tool := range AllTools {
		names[tool.Function.Name] = true
	}
	for name := range Policies {
		if !names[name] {
			t.Errorf("policy %q has no matching tool definition in AllTools", name)
		}
	}
}
