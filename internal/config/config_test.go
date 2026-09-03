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
		"CRIAISIS_SLACK_CLIENT_ID=123.456",
		"CRIAISIS_SLACK_CLIENT_SECRET=secret123",
		"CRIAISIS_SLACK_SIGNING_SECRET=signing123",
		"CRIAISIS_SLACK_BOT_TOKEN_ENCRYPTION_KEY=01234567890123456789012345678901",
		"CRIAISIS_LLM_PROVIDER=openai",
		"CRIAISIS_LLM_API_KEY=sk-test123",
		"CRIAISIS_LLM_SPECIALIST_MODEL=gpt-4o-mini",
		"CRIAISIS_LLM_SYNTHESIS_MODEL=gpt-4o",
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
	if cfg.LLM.SpecialistModel != "gpt-4o-mini" {
		t.Errorf("expected specialist model 'gpt-4o-mini', got '%s'", cfg.LLM.SpecialistModel)
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

func TestLoadConfigFromEnviron_InvalidEncryptionKeyLength(t *testing.T) {
	env := validEnviron()
	// Replace key with a 13-character string instead of 32
	for i, v := range env {
		if strings.HasPrefix(v, "CRIAISIS_SLACK_BOT_TOKEN_ENCRYPTION_KEY=") {
			env[i] = "CRIAISIS_SLACK_BOT_TOKEN_ENCRYPTION_KEY=too-short-key"
		}
	}

	_, err := LoadConfigFromEnviron(env)
	if err == nil {
		t.Fatal("expected error for 13-char encryption key, got nil")
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
