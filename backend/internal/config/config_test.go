package config

import "testing"

func validTestConfig(env, secret string) *Config {
	return &Config{
		AppEnv:               env,
		AppSecretKey:         secret,
		HomeappUserID:        "user_test_owner_123456",
		DatabaseURL:          "postgres://homeapp:strong-test-password@localhost:5432/homeapp?sslmode=disable",
		LaventeCareSecretKey: "vault-only-secret-0123456789abcdef",
	}
}
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
			c := validTestConfig(tc.env, tc.secret)
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
func TestValidateRequiresOwnerAndDatabase(t *testing.T) {
	t.Run("missing database", func(t *testing.T) {
		c := validTestConfig("development", "change-me")
		c.DatabaseURL = ""
		if err := c.Validate(); err == nil {
			t.Fatal("expected missing DATABASE_URL to fail")
		}
	})

	t.Run("short owner", func(t *testing.T) {
		c := validTestConfig("development", "change-me")
		c.HomeappUserID = "short"
		if err := c.Validate(); err == nil {
			t.Fatal("expected short HOMEAPP_USER_ID to fail")
		}
	})

	t.Run("weak database is development-only", func(t *testing.T) {
		dev := validTestConfig("development", "change-me")
		dev.DatabaseURL = "postgres://homeapp:change-me@localhost:5432/homeapp"
		if err := dev.Validate(); err != nil {
			t.Fatalf("development should warn rather than fail: %v", err)
		}

		prod := validTestConfig("production", "strong-app-secret")
		prod.DatabaseURL = dev.DatabaseURL
		if err := prod.Validate(); err == nil {
			t.Fatal("production must reject weak database password")
		}
	})
}

func TestValidateProductionSecretLength(t *testing.T) {
	short := validTestConfig("production", "x")
	if err := short.Validate(); err == nil {
		t.Fatal("production must reject a one-character APP_SECRET_KEY")
	}
	strong := validTestConfig("production", "app-secret-0123456789abcdef0123456789")
	if err := strong.Validate(); err != nil {
		t.Fatalf("production should accept a separate 32+ character app secret: %v", err)
	}
}

func TestValidateBridgeTrustBoundary(t *testing.T) {
	c := validTestConfig("production", "strong-app-secret-0123456789abcdef")
	c.BridgeAPIURL = "https://backend.example.test/api/v1"
	if err := c.Validate(); err == nil {
		t.Fatal("bridge mode without BRIDGE_API_KEY must fail")
	}
	c.BridgeAPIKey = "short-but-separate"
	if err := c.Validate(); err == nil {
		t.Fatal("active bridge key shorter than 32 characters must fail")
	}
	c.BridgeAPIKey = c.AppSecretKey
	if err := c.Validate(); err == nil {
		t.Fatal("bridge key equal to app key must fail")
	}
	c.BridgeAPIKey = "separate-bridge-secret-0123456789abcd"
	if err := c.Validate(); err != nil {
		t.Fatalf("separate bridge key should pass: %v", err)
	}
}
func TestValidateLaventeCareVaultTrustBoundary(t *testing.T) {
	strongApp := "app-only-secret-0123456789abcdef"
	strongVault := "vault-only-secret-0123456789abcdef"

	missing := validTestConfig("production", strongApp)
	missing.LaventeCareSecretKey = ""
	if err := missing.Validate(); err == nil {
		t.Fatal("production must reject a missing LAVENTECARE_SECRET_KEY")
	}

	short := validTestConfig("production", strongApp)
	short.LaventeCareSecretKey = "too-short"
	if err := short.Validate(); err == nil {
		t.Fatal("production must reject a short LAVENTECARE_SECRET_KEY")
	}

	same := validTestConfig("production", strongApp)
	same.LaventeCareSecretKey = same.AppSecretKey
	if err := same.Validate(); err == nil {
		t.Fatal("vault key equal to app key must fail")
	}

	valid := validTestConfig("production", strongApp)
	valid.LaventeCareSecretKey = strongVault
	if err := valid.Validate(); err != nil {
		t.Fatalf("separate strong vault key should pass: %v", err)
	}
}

func TestValidateLaventeCareIntakeTrustBoundary(t *testing.T) {
	const strongIntakeSecret = "intake-only-secret-0123456789abcdef"

	tests := []struct {
		name         string
		intakeSecret string
		appSecret    string
		bridgeSecret string
		wantError    bool
	}{
		{name: "empty keeps endpoint disabled", wantError: false},
		{name: "short secret", intakeSecret: "too-short", wantError: true},
		{name: "known placeholder", intakeSecret: "change-me", wantError: true},
		{name: "same as app secret", intakeSecret: strongIntakeSecret, appSecret: strongIntakeSecret, wantError: true},
		{name: "same as bridge secret", intakeSecret: strongIntakeSecret, bridgeSecret: strongIntakeSecret, wantError: true},
		{name: "separate strong secret", intakeSecret: strongIntakeSecret, wantError: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			appSecret := tc.appSecret
			if appSecret == "" {
				appSecret = "separate-strong-app-secret"
			}
			c := validTestConfig("development", appSecret)
			c.LaventeCareIntakeSecret = tc.intakeSecret
			c.BridgeAPIKey = tc.bridgeSecret
			if err := c.Validate(); (err != nil) != tc.wantError {
				t.Fatalf("Validate() error = %v, wantError %v", err, tc.wantError)
			}
		})
	}
}

func TestWeakDatabaseURLRejectsSchemeHostAndNormalizedDefaults(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"case variant placeholder", "postgres://homeapp:%20Change-Me%20@localhost:5432/homeapp"},
		{"missing host", "postgres://homeapp:strong-password@/homeapp"},
		{"wrong scheme", "mysql://homeapp:strong-password@localhost:3306/homeapp"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if weak, _ := weakDatabaseURL(tc.url); !weak {
				t.Fatalf("expected unsafe database URL: %s", tc.url)
			}
		})
	}
	if weak, reason := weakDatabaseURL("postgresql://homeapp:strong-password@localhost:5432/homeapp"); weak {
		t.Fatalf("valid postgresql URL rejected: %s", reason)
	}
}
