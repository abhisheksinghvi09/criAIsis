package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/knadh/koanf/v2"
)

const EnvPrefix = "CRIAISIS_"

type Config struct {
	Primary  Primary        `koanf:"primary" validate:"required"`
	Server   ServerConfig   `koanf:"server" validate:"required"`
	Database DatabaseConfig `koanf:"database" validate:"required"`
	Slack    SlackConfig    `koanf:"slack" validate:"required"`
	LLM      LLMConfig      `koanf:"llm" validate:"required"`
}

type Primary struct {
	Env string `koanf:"env" validate:"required,oneof=local production test"`
}

type ServerConfig struct {
	Port               string   `koanf:"port" validate:"required"`
	ReadTimeout        int      `koanf:"read_timeout" validate:"required,gt=0"`
	WriteTimeout       int      `koanf:"write_timeout" validate:"required,gt=0"`
	IdleTimeout        int      `koanf:"idle_timeout" validate:"required,gt=0"`
	CORSAllowedOrigins []string `koanf:"cors_allowed_origins" validate:"required"`
}

type SlackConfig struct {
	ClientID              string `koanf:"client_id" validate:"required"`
	ClientSecret          string `koanf:"client_secret" validate:"required"`
	SigningSecret         string `koanf:"signing_secret" validate:"required"`
	BotTokenEncryptionKey string `koanf:"bot_token_encryption_key" validate:"required,len=32"`
}

type LLMConfig struct {
	Provider        string `koanf:"provider" validate:"required"`
	APIKey          string `koanf:"api_key" validate:"required"`
	SpecialistModel string `koanf:"specialist_model" validate:"required"`
	SynthesisModel  string `koanf:"synthesis_model" validate:"required"`
	EmbeddingModel  string `koanf:"embedding_model" validate:"required"`
}

func LoadConfig() (*Config, error) {
	return LoadConfigFromEnviron(os.Environ())
}

func LoadConfigFromEnviron(environ []string) (*Config, error) {
	k := koanf.New(".")

	envMap := make(map[string]any)
	for _, envStr := range environ {
		parts := strings.SplitN(envStr, "=", 2)
		if len(parts) == 2 && strings.HasPrefix(parts[0], EnvPrefix) {
			rawKey := strings.TrimPrefix(parts[0], EnvPrefix)
			// Convert CRIAISIS_PRIMARY_ENV -> primary.env
			key := strings.ToLower(strings.ReplaceAll(rawKey, "_", "."))
			val := parts[1]

			// Handle comma-separated slices (e.g. CORSAllowedOrigins)
			if strings.Contains(key, "cors.allowed.origins") {
				origins := strings.Split(val, ",")
				for i := range origins {
					origins[i] = strings.TrimSpace(origins[i])
				}
				envMap["server.cors_allowed_origins"] = origins
				continue
			}

			// Re-normalize key for known underscores in sub-struct fields
			normalizedKey := normalizeConfigKey(key)
			envMap[normalizedKey] = val
		}
	}

	for key, value := range envMap {
		if err := k.Set(key, value); err != nil {
			return nil, fmt.Errorf("failed setting config key %s: %w", key, err)
		}
	}

	var mainConfig Config
	if err := k.Unmarshal("", &mainConfig); err != nil {
		return nil, fmt.Errorf("could not unmarshal config: %w", err)
	}

	validate := validator.New()
	if err := validate.Struct(&mainConfig); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &mainConfig, nil
}

func normalizeConfigKey(key string) string {
	replacements := map[string]string{
		"server.read.timeout":            "server.read_timeout",
		"server.write.timeout":           "server.write_timeout",
		"server.idle.timeout":            "server.idle_timeout",
		"database.ssl.mode":              "database.ssl_mode",
		"database.max.open.conns":        "database.max_open_conns",
		"database.max.idle.conns":        "database.max_idle_conns",
		"database.conn.max.lifetime":     "database.conn_max_lifetime",
		"database.conn.max.idle.time":    "database.conn_max_idle_time",
		"slack.client.id":                "slack.client_id",
		"slack.client.secret":            "slack.client_secret",
		"slack.signing.secret":           "slack.signing_secret",
		"slack.bot.token.encryption.key": "slack.bot_token_encryption_key",
		"llm.api.key":                    "llm.api_key",
		"llm.specialist.model":           "llm.specialist_model",
		"llm.synthesis.model":            "llm.synthesis_model",
		"llm.embedding.model":            "llm.embedding_model",
	}

	if normalized, ok := replacements[key]; ok {
		return normalized
	}
	return key
}
