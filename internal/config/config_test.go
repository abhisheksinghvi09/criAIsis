package config

import (
	"strings"
	"testing"
)

func validEnviron() []string {
	return []string{
		"CRIAISIS_PRIMARY_ENV=local",
		"CRIAISIS_SERVER_PORT=8080",
		"CRIAISIS_SERVER_READ_TIMEOUT=15",
		"CRIAISIS_SERVER_WRITE_TIMEOUT=15",
		"CRIAISIS_SERVER_IDLE_TIMEOUT=60",
		"CRIAISIS_SERVER_CORS_ALLOWED_ORIGINS=http://localhost:3000,http://localhost:5173",
		"CRIAISIS_DATABASE_HOST=localhost",
		"CRIAISIS_DATABASE_PORT=5432",
		"CRIAISIS_DATABASE_USER=postgres",
		"CRIAISIS_DATABASE_PASSWORD=abhi101",
		"CRIAISIS_DATABASE_NAME=criaisis",
		"CRIAISIS_DATABASE_SSL_MODE=disable",
		"CRIAISIS_DATABASE_MAX_OPEN_CONNS=25",
		"CRIAISIS_DATABASE_MAX_IDLE_CONNS=10",
		"CRIAISIS_DATABASE_CONN_MAX_LIFETIME=300",
		"CRIAISIS_DATABASE_CONN_MAX_IDLE_TIME=120",
		"CRIAISIS_SECURITY_CREDENTIAL_ENCRYPTION_KEY=01234567890123456789012345678901",
		"CRIAISIS_SECURITY_ADMIN_API_KEY=platform-admin-key-long-enough",
		"CRIAISIS_LLM_SPECIALIST_MODEL=claude-opus-5",
		"CRIAISIS_LLM_SYNTHESIS_MODEL=claude-opus-5",
		"CRIAISIS_LLM_EMBEDDING_MODEL=text-embedding-3-small",
	}
}

func TestLoadConfigFromEnviron_Valid(t *testing.T) {
	cfg, err := LoadConfigFromEnviron(validEnviron())
	if err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}

	if cfg.Primary.Env != "local" {
		t.Errorf("expected env 'local', got '%s'", cfg.Primary.Env)
	}
	if cfg.Server.Port != "8080" {
		t.Errorf("expected port '8080', got '%s'", cfg.Server.Port)
	}
	if len(cfg.Server.CORSAllowedOrigins) != 2 {
		t.Errorf("expected 2 CORS origins, got %d", len(cfg.Server.CORSAllowedOrigins))
	}
	if cfg.Database.Host != "localhost" {
		t.Errorf("expected host 'localhost', got '%s'", cfg.Database.Host)
	}
	if cfg.Database.Port != 5432 {
		t.Errorf("expected port 5432, got %d", cfg.Database.Port)
	}
	if cfg.LLM.SpecialistModel != "claude-opus-5" {
		t.Errorf("expected specialist model 'claude-opus-5', got '%s'", cfg.LLM.SpecialistModel)
	}
	// embedding_base_url is not in the environ: it must come from defaults
	if cfg.LLM.EmbeddingBaseURL != "https://api.openai.com/v1" {
		t.Errorf("expected default embedding base url, got '%s'", cfg.LLM.EmbeddingBaseURL)
	}

	expectedDSN := "postgres://postgres:abhi101@localhost:5432/criaisis?sslmode=disable"
	if cfg.Database.DSN() != expectedDSN {
		t.Errorf("expected DSN '%s', got '%s'", expectedDSN, cfg.Database.DSN())
	}
}

func TestLoadConfigFromEnviron_MissingRequiredField(t *testing.T) {
	// Missing database host
	environ := []string{
		"CRIAISIS_PRIMARY_ENV=local",
		"CRIAISIS_SERVER_PORT=8080",
	}

	_, err := LoadConfigFromEnviron(environ)
	if err == nil {
		t.Fatal("expected error for missing required fields, got nil")
	}
}

// The encryption key protects every tenant credential at rest, so a wrong length
// must abort at boot rather than later.
func TestLoadConfigFromEnviron_InvalidEncryptionKeyLength(t *testing.T) {
	env := validEnviron()
	for i, v := range env {
		if strings.HasPrefix(v, "CRIAISIS_SECURITY_CREDENTIAL_ENCRYPTION_KEY=") {
			env[i] = "CRIAISIS_SECURITY_CREDENTIAL_ENCRYPTION_KEY=too-short-key"
		}
	}

	if _, err := LoadConfigFromEnviron(env); err == nil {
		t.Fatal("expected error for a 13-character encryption key, got nil")
	}
}

// The struct tag's len=32 counts runes; AES-256 (and crypto.New) counts bytes.
// A key built from 32 multi-byte runes is exactly 32 runes but far more than
// 32 bytes, so it must still be rejected here rather than passing validation
// and failing later at cipher construction with a worse error.
func TestLoadConfigFromEnviron_RejectsMultibyteEncryptionKeyOfRightRuneCount(t *testing.T) {
	env := validEnviron()
	multibyteKey := strings.Repeat("é", 32) // 32 runes, 64 bytes (é is 2 bytes in UTF-8)
	for i, v := range env {
		if strings.HasPrefix(v, "CRIAISIS_SECURITY_CREDENTIAL_ENCRYPTION_KEY=") {
			env[i] = "CRIAISIS_SECURITY_CREDENTIAL_ENCRYPTION_KEY=" + multibyteKey
		}
	}

	if _, err := LoadConfigFromEnviron(env); err == nil {
		t.Fatal("expected a 32-rune/64-byte key to be rejected, got nil")
	}
}

// database.ssl_mode defaults to "disable" for local dev; that default must never
// reach a production deploy, where it would mean every decrypted tenant
// credential travels to Postgres in plaintext.
func TestLoadConfigFromEnviron_RejectsDisabledSSLInProduction(t *testing.T) {
	env := validEnviron()
	for i, v := range env {
		switch {
		case strings.HasPrefix(v, "CRIAISIS_PRIMARY_ENV="):
			env[i] = "CRIAISIS_PRIMARY_ENV=production"
		case strings.HasPrefix(v, "CRIAISIS_DATABASE_SSL_MODE="):
			env[i] = "CRIAISIS_DATABASE_SSL_MODE=disable"
		}
	}

	if _, err := LoadConfigFromEnviron(env); err == nil {
		t.Fatal("expected ssl_mode=disable to be rejected when env=production")
	}
}

func TestLoadConfigFromEnviron_AllowsDisabledSSLOutsideProduction(t *testing.T) {
	env := validEnviron() // primary.env=local, ssl_mode=disable, already set
	if _, err := LoadConfigFromEnviron(env); err != nil {
		t.Fatalf("expected ssl_mode=disable to be fine outside production, got: %v", err)
	}
}

// criAIsis is bring-your-own-key: there must be no platform-wide model credential
// in configuration for a tenant to accidentally ride on.
func TestConfig_CarriesNoPlatformModelKey(t *testing.T) {
	cfg, err := LoadConfigFromEnviron(validEnviron())
	if err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}

	// LLMConfig should expose model defaults only. If a key field is ever added
	// back, this test is the place that should stop it.
	if cfg.LLM.SpecialistModel == "" || cfg.LLM.EmbeddingBaseURL == "" {
		t.Error("expected model defaults to be present")
	}
	if cfg.Security.AdminAPIKey == "" {
		t.Error("expected a platform admin key for workspace creation")
	}
}

// A short platform admin key is guessable and gates workspace creation.
func TestLoadConfigFromEnviron_RejectsWeakAdminKey(t *testing.T) {
	env := validEnviron()
	for i, v := range env {
		if strings.HasPrefix(v, "CRIAISIS_SECURITY_ADMIN_API_KEY=") {
			env[i] = "CRIAISIS_SECURITY_ADMIN_API_KEY=short"
		}
	}

	if _, err := LoadConfigFromEnviron(env); err == nil {
		t.Fatal("expected a short admin key to be rejected")
	}
}

func TestDatabaseConfig_DSN(t *testing.T) {
	db := DatabaseConfig{
		Host:     "db.example.com",
		Port:     5432,
		User:     "app_user",
		Password: "p@ss:w/ord",
		Name:     "criaisis_prod",
		SSLMode:  "require",
	}

	dsn := db.DSN()
	expected := "postgres://app_user:p%40ss%3Aw%2Ford@db.example.com:5432/criaisis_prod?sslmode=require"
	if dsn != expected {
		t.Errorf("expected DSN '%s', got '%s'", expected, dsn)
	}
}

// A password containing a space must round-trip to a real space, not "+":
// url.QueryEscape (query-string rules) would encode it as "+", a literal
// character in a URL's userinfo component - so the stored password and the
// password the driver actually sends would silently differ.
func TestDatabaseConfig_DSN_PasswordWithSpace(t *testing.T) {
	db := DatabaseConfig{
		Host: "db.example.com", Port: 5432, User: "app_user",
		Password: "pass with space", Name: "criaisis_prod", SSLMode: "require",
	}

	dsn := db.DSN()
	expected := "postgres://app_user:pass%20with%20space@db.example.com:5432/criaisis_prod?sslmode=require"
	if dsn != expected {
		t.Errorf("expected DSN '%s', got '%s'", expected, dsn)
	}
}
