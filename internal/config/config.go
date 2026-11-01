package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Addr         string
	DataDir      string
	SessionTTL   time.Duration
	CookieSecure bool
	TLSCertFile  string
	TLSKeyFile   string
	PublicURL    string
	TrustProxy   bool
}

func Load() (Config, error) {
	c := Config{Addr: env("HOST_ADDR", "0.0.0.0:8080"), DataDir: env("DATA_DIR", "data"), SessionTTL: 12 * time.Hour}
	if _, _, err := net.SplitHostPort(c.Addr); err != nil {
		return c, fmt.Errorf("HOST_ADDR: %w", err)
	}
	var err error
	c.SessionTTL, err = time.ParseDuration(env("SESSION_TTL", "12h"))
	if err != nil || c.SessionTTL < time.Second || c.SessionTTL > 30*24*time.Hour {
		return c, fmt.Errorf("SESSION_TTL must be between 1s and 720h")
	}
	c.CookieSecure, err = strconv.ParseBool(env("COOKIE_SECURE", "false"))
	if err != nil {
		return c, fmt.Errorf("COOKIE_SECURE: %w", err)
	}
	c.TLSCertFile, c.TLSKeyFile = os.Getenv("TLS_CERT_FILE"), os.Getenv("TLS_KEY_FILE")
	if (c.TLSCertFile == "") != (c.TLSKeyFile == "") {
		return c, fmt.Errorf("set both TLS_CERT_FILE and TLS_KEY_FILE")
	}
	c.PublicURL = os.Getenv("PUBLIC_URL")
	c.TrustProxy, err = strconv.ParseBool(env("TRUST_PROXY", "false"))
	if err != nil {
		return c, fmt.Errorf("TRUST_PROXY: %w", err)
	}
	if c.TrustProxy && c.PublicURL == "" {
		return c, fmt.Errorf("TRUST_PROXY requires an explicit PUBLIC_URL")
	}
	return c, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
