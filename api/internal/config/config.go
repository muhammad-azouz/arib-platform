// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr      string
	PublicBaseURL string

	MongoURI string
	MongoDB  string

	PrivateKeyXML string

	JWTSecret       []byte
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration

	// Sync tokens authorize a bound device against the DMS gateway.
	SyncTokenTTL time.Duration
	// SyncGatewayURL is the fallback DMS gateway URL. Used only when SYNC_SHARDS
	// is unset (single-gateway dev/bootstrap); always synthesized into Shards[0].
	SyncGatewayURL string
	// Shards is the parsed shard registry seeded at startup. At least one entry
	// is always present (synthesized from SyncGatewayURL when SYNC_SHARDS unset).
	Shards []ShardConfig

	RevalidateAfter    time.Duration
	HardExpireAfter    time.Duration
	TrialDuration      time.Duration
	ReleaseCooldown    time.Duration
	ReleaseMaxPerMonth int

	SMTPHost       string
	SMTPPort       int
	SMTPUsername   string
	SMTPPassword   string
	SMTPFrom       string
	OTPTTL         time.Duration
	OTPMaxAttempts int

	GoogleClientID       string
	GoogleClientSecret   string
	FacebookClientID     string
	FacebookClientSecret string

	AdminEmails []string

	// DashboardOrigins are the browser origins allowed to call the API via CORS
	// (the admin dashboard). Bearer-token auth, so credentials are not used.
	DashboardOrigins []string

	// UpdatesDir is the root of the Velopack update feed served at /updates/*
	// (layout: {channel}/{rid}/... — see tasks/spec-app-updates.md in the
	// desktop repo).
	UpdatesDir string
	// UpdatesAuth enables the entitlement gate on the feed (token-filtered
	// manifest, gated packages). Default off until the gate ships; it will
	// then default to on with UPDATES_AUTH=off as the dev escape hatch.
	UpdatesAuth bool
}

// Load reads configuration from the process environment, applying defaults and
// validating required values. Call godotenv/Load beforehand for local dev.
func Load() (*Config, error) {
	c := &Config{
		HTTPAddr:             env("HTTP_ADDR", "127.0.0.1:8080"),
		PublicBaseURL:        strings.TrimRight(env("PUBLIC_BASE_URL", "http://127.0.0.1:8080"), "/"),
		MongoURI:             os.Getenv("MONGO_URI"),
		MongoDB:              env("MONGO_DB", "arib_license"),
		AccessTokenTTL:       dur("ACCESS_TOKEN_TTL", time.Hour),
		RefreshTokenTTL:      dur("REFRESH_TOKEN_TTL", 720*time.Hour),
		RevalidateAfter:      dur("REVALIDATE_AFTER", 14*24*time.Hour),
		HardExpireAfter:      dur("HARD_EXPIRE_AFTER", 28*24*time.Hour),
		TrialDuration:        dur("TRIAL_DURATION", 7*24*time.Hour),
		ReleaseCooldown:      dur("RELEASE_COOLDOWN", 72*time.Hour),
		ReleaseMaxPerMonth:   integer("RELEASE_MAX_PER_MONTH", 3),
		SMTPHost:             os.Getenv("SMTP_HOST"),
		SMTPPort:             integer("SMTP_PORT", 587),
		SMTPUsername:         os.Getenv("SMTP_USERNAME"),
		SMTPPassword:         os.Getenv("SMTP_PASSWORD"),
		SMTPFrom:             env("SMTP_FROM", "Arib POS <no-reply@arib.example>"),
		OTPTTL:               dur("OTP_TTL", 10*time.Minute),
		OTPMaxAttempts:       integer("OTP_MAX_ATTEMPTS", 5),
		GoogleClientID:       os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret:   os.Getenv("GOOGLE_CLIENT_SECRET"),
		FacebookClientID:     os.Getenv("FACEBOOK_CLIENT_ID"),
		FacebookClientSecret: os.Getenv("FACEBOOK_CLIENT_SECRET"),
		AdminEmails:          splitCSV(os.Getenv("ADMIN_EMAILS")),
		DashboardOrigins:     splitCSV(os.Getenv("DASHBOARD_ORIGINS")),
		UpdatesDir:           env("UPDATES_DIR", "/app/updates"),
		UpdatesAuth:          env("UPDATES_AUTH", "off") != "off",
	}

	secret := os.Getenv("JWT_SECRET")
	if len(secret) < 16 {
		return nil, fmt.Errorf("JWT_SECRET must be set (>=16 chars)")
	}
	c.JWTSecret = []byte(secret)

	// Sync tokens are RS256-signed with the license RSA key (the gateway holds
	// only the public key); SYNC_TOKEN_SECRET is gone with the HS256 scheme.
	c.SyncTokenTTL = dur("SYNC_TOKEN_TTL", time.Hour)
	c.SyncGatewayURL = strings.TrimRight(env("SYNC_GATEWAY_URL", "http://127.0.0.1:5310"), "/")
	c.Shards = parseShards(os.Getenv("SYNC_SHARDS"), c.SyncGatewayURL)

	if c.MongoURI == "" {
		return nil, fmt.Errorf("MONGO_URI is required")
	}

	xml, err := loadPrivateKeyXML()
	if err != nil {
		return nil, err
	}
	c.PrivateKeyXML = xml

	return c, nil
}

// IsAdmin reports whether the given email is on the admin allow-list.
func (c *Config) IsAdmin(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	for _, a := range c.AdminEmails {
		if strings.ToLower(strings.TrimSpace(a)) == email {
			return true
		}
	}
	return false
}

func loadPrivateKeyXML() (string, error) {
	if inline := os.Getenv("PRIVATE_KEY_XML"); strings.TrimSpace(inline) != "" {
		return inline, nil
	}
	path := env("PRIVATE_KEY_XML_PATH", "keys/PrivateKey.xml")
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read private key (%s): %w", path, err)
	}
	return string(b), nil
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func dur(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func integer(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// ShardConfig is one shard entry from SYNC_SHARDS.
type ShardConfig struct {
	ID         string
	GatewayURL string
}

// parseShards parses SYNC_SHARDS ("shd_a=http://gw-a:5310;shd_b=http://gw-b:5310").
// Falls back to a single "shd_default" entry pointing at fallbackURL when raw
// is empty, preserving single-gateway backwards compatibility.
func parseShards(raw, fallbackURL string) []ShardConfig {
	if strings.TrimSpace(raw) == "" {
		return []ShardConfig{{ID: "shd_default", GatewayURL: strings.TrimRight(fallbackURL, "/")}}
	}
	var out []ShardConfig
	for _, entry := range strings.Split(raw, ";") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		id := strings.TrimSpace(parts[0])
		url := strings.TrimRight(strings.TrimSpace(parts[1]), "/")
		if id != "" && url != "" {
			out = append(out, ShardConfig{ID: id, GatewayURL: url})
		}
	}
	if len(out) == 0 {
		return []ShardConfig{{ID: "shd_default", GatewayURL: strings.TrimRight(fallbackURL, "/")}}
	}
	return out
}
