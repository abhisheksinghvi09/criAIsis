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
// Passwords with special characters (e.g. '@', ':', '/', a literal space) are
// safely escaped using the URL userinfo encoding rules to prevent connection
// parse failures or, worse, a silently wrong password.
func (d DatabaseConfig) DSN() string {
	hostPort := net.JoinHostPort(d.Host, strconv.Itoa(d.Port))
	// url.QueryEscape encodes a space as "+", which is a literal character (not
	// a space) in a URL's userinfo component: a password containing a space
	// would authenticate with the wrong string instead of failing loudly.
	// url.UserPassword applies the correct userinfo percent-encoding instead.
	userinfo := url.UserPassword(d.User, d.Password)

	return fmt.Sprintf("postgres://%s@%s/%s?sslmode=%s",
		userinfo,
		hostPort,
		d.Name,
		d.SSLMode,
	)
}
