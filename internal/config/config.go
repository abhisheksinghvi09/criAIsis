package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
)

// defaults cover every value that has a single sane setting, so a local developer
// only has to supply credentials and the database host.
var defaults = map[string]any{
	"primary.env":                 "local",
	"server.port":                 "8080",
	"server.read_timeout":         15,
	"server.write_timeout":        30,
	"server.idle_timeout":         60,
	"llm.provider":                "anthropic",
	"llm.specialist_model":        "claude-opus-5",
	"llm.synthesis_model":         "claude-opus-5",
	"llm.embedding_provider":      "openai",
	"llm.embedding_model":         "text-embedding-3-small",
	"llm.embedding_base_url":      "https://api.openai.com/v1",
	"database.ssl_mode":           "disable",
	"database.max_open_conns":     25,
	"database.max_idle_conns":     5,
	"database.conn_max_lifetime":  3600,
	"database.conn_max_idle_time": 900,
}

// EnvPrefix defines the global namespace for all environment variables.
// Following twelve-factor app design, namespacing prevents collision with system or container runtime variables.
const EnvPrefix = "CRIAISIS_"

// Config represents the validated, root application configuration tree.
// Sub-structs partition concerns so components only depend on what they need.
type Config struct {
	Primary  Primary        `koanf:"primary" validate:"required"`
	Server   ServerConfig   `koanf:"server" validate:"required"`
	Database DatabaseConfig `koanf:"database" validate:"required"`
	Security SecurityConfig `koanf:"security" validate:"required"`
	Slack    SlackConfig    `koanf:"slack"`
	LLM      LLMConfig      `koanf:"llm" validate:"required"`
}

// Primary defines runtime environment semantics (e.g. log levels, test bypasses).
type Primary struct {
	Env string `koanf:"env" validate:"required,oneof=local production test"`
}

// ServerConfig specifies HTTP server tuning parameters and CORS security headers.
type ServerConfig struct {
	Port               string   `koanf:"port" validate:"required"`
	ReadTimeout        int      `koanf:"read_timeout" validate:"required,gt=0"`
	WriteTimeout       int      `koanf:"write_timeout" validate:"required,gt=0"`
	IdleTimeout        int      `koanf:"idle_timeout" validate:"required,gt=0"`
	CORSAllowedOrigins []string `koanf:"cors_allowed_origins" validate:"required"`
}

// SecurityConfig holds the platform's own secrets.
//
// CredentialEncryptionKey protects every tenant credential at rest; rotating it
// makes previously stored keys unreadable. AdminAPIKey is the bootstrap
// credential that may create workspaces, and nothing else.
type SecurityConfig struct {
	CredentialEncryptionKey string `koanf:"credential_encryption_key" validate:"required,len=32"`
	AdminAPIKey             string `koanf:"admin_api_key" validate:"required,min=24"`
}

// SlackConfig holds OAuth application credentials for the Slack app. Left
// empty, the Slack surface mounts no routes (cmd/criaisis fails closed).
type SlackConfig struct {
	ClientID      string `koanf:"client_id"`
	ClientSecret  string `koanf:"client_secret"`
	SigningSecret string `koanf:"signing_secret"`
}

// LLMConfig carries platform defaults only.
//
// criAIsis is bring-your-own-key: each tenant supplies their own model and
// embedding credentials, stored encrypted against their workspace. There is
// deliberately no platform-wide API key here, so no tenant can ever spend on,
// rate-limit, or leak through a shared credential.
type LLMConfig struct {
	SpecialistModel  string `koanf:"specialist_model" validate:"required"`
	SynthesisModel   string `koanf:"synthesis_model" validate:"required"`
	EmbeddingModel   string `koanf:"embedding_model" validate:"required"`
	EmbeddingBaseURL string `koanf:"embedding_base_url" validate:"required,url"`
}

// LoadConfig reads the process environment, populates the Config struct, and enforces strict validation.
// It fails fast at boot time if any required configuration is missing or malformed.
func LoadConfig() (*Config, error) {
	return LoadConfigFromEnviron(os.Environ())
}

// LoadConfigFromEnviron allows deterministic testing by accepting an explicit environ slice.
func LoadConfigFromEnviron(environ []string) (*Config, error) {
	k := koanf.New(".")
	if err := k.Load(confmap.Provider(defaults, "."), nil); err != nil {
		return nil, fmt.Errorf("loading config defaults: %w", err)
	}

	for _, envStr := range environ {
		key, val, ok := parseEnvVar(envStr)
		if !ok {
			continue
		}
		if err := k.Set(key, val); err != nil {
			return nil, fmt.Errorf("failed setting config key %s: %w", key, err)
		}
	}

	var mainConfig Config
	if err := k.Unmarshal("", &mainConfig); err != nil {
		return nil, fmt.Errorf("could not unmarshal config: %w", err)
	}

	// Fail fast: execute validation rules across all struct tags
	if err := validator.New().Struct(&mainConfig); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &mainConfig, nil
}

// parseEnvVar maps CRIAISIS_<GROUP>_<FIELD> onto the koanf path <group>.<field>.
// The remainder after the group is kept verbatim because every koanf struct tag
// already uses snake_case (e.g. CRIAISIS_LLM_SPECIALIST_MODEL -> llm.specialist_model).
func parseEnvVar(envStr string) (key string, val any, ok bool) {
	name, raw, found := strings.Cut(envStr, "=")
	if !found || !strings.HasPrefix(name, EnvPrefix) {
		return "", nil, false
	}
	group, field, found := strings.Cut(strings.TrimPrefix(name, EnvPrefix), "_")
	if !found {
		return "", nil, false
	}
	key = strings.ToLower(group) + "." + strings.ToLower(field)

	if key == "server.cors_allowed_origins" {
		origins := strings.Split(raw, ",")
		for i := range origins {
			origins[i] = strings.TrimSpace(origins[i])
		}
		return key, origins, true
	}
	return key, raw, true
}
