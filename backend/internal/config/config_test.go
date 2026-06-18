package config

import "testing"

func TestValidate(t *testing.T) {
	tests := []struct {
		name      string
		env       string
		secret    string
		wantError bool
	}{
		{"dev allows default", "development", "change-me", false},
		{"dev allows empty", "development", "", false},
		{"prod rejects default", "production", "change-me", true},
		{"prod rejects empty", "production", "", true},
		{"prod rejects long-default", "production", "change-me-to-a-long-random-secret", true},
		{"prod allows strong", "production", "a-very-long-random-production-secret", false},
		{"staging rejects default", "staging", "change-me", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{AppEnv: tc.env, AppSecretKey: tc.secret}
			err := c.Validate()
			if tc.wantError && err == nil {
				t.Fatalf("expected error for env=%q secret=%q, got nil", tc.env, tc.secret)
			}
			if !tc.wantError && err != nil {
				t.Fatalf("expected no error for env=%q secret=%q, got %v", tc.env, tc.secret, err)
			}
		})
	}
}
