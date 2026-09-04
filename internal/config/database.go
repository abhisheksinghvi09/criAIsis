package config

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
)

// DatabaseConfig encapsulates PostgreSQL connection and pool parameters.
// Struct tags declare koanf unmarshal keys and validation requirements.
type DatabaseConfig struct {
	Host            string `koanf:"host" validate:"required"`
	Port            int    `koanf:"port" validate:"required"`
	User            string `koanf:"user" validate:"required"`
	Password        string `koanf:"password"`
	Name            string `koanf:"name" validate:"required"`
	SSLMode         string `koanf:"ssl_mode" validate:"required"`
	MaxOpenConns    int    `koanf:"max_open_conns" validate:"required"`
	MaxIdleConns    int    `koanf:"max_idle_conns" validate:"required"`
	ConnMaxLifetime int    `koanf:"conn_max_lifetime" validate:"required"`
	ConnMaxIdleTime int    `koanf:"conn_max_idle_time" validate:"required"`
}

// DSN formats a standard, URL-safe PostgreSQL connection string.
// Passwords with special characters (e.g. '@', ':', '/') are safely escaped to prevent connection parse failures.
func (d DatabaseConfig) DSN() string {
	hostPort := net.JoinHostPort(d.Host, strconv.Itoa(d.Port))
	encodedPassword := url.QueryEscape(d.Password)

	return fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=%s",
		d.User,
		encodedPassword,
		hostPort,
		d.Name,
		d.SSLMode,
	)
}
