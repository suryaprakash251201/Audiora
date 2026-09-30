package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the fully-resolved runtime configuration. Every field is
// populated from the environment; nothing is read from disk at runtime so
// the container stays stateless apart from DATA_PATH.
type Config struct {
	ListenAddr string
	DataPath   string
	MusicPath  string

	Domain      string
	ACMEEmail   string
	CORSOrigins []string

	Secret            []byte
	AccessTokenTTL    time.Duration
	RefreshTokenTTL   time.Duration
	ScanInterval      time.Duration
	ExtractCoverArt   bool
	ExtractColors     bool
	PrewarmTranscode  bool
	TranscodeCacheDir string
	CoverArtDir       string
	DatabasePath      string

	BootstrapAdminEmail    string
	BootstrapAdminPassword string
	BootstrapAdminName     string
}

// Load reads configuration from the environment and validates it.
func Load() (*Config, error) {
	c := &Config{
		ListenAddr:             env("LISTEN_ADDR", ":8080"),
		DataPath:               env("DATA_PATH", "/srv/audiora-data"),
		MusicPath:              env("MUSIC_PATH", "/srv/music"),
		Domain:                 env("AUDIORA_DOMAIN", ""),
		ACMEEmail:              env("AUDIORA_ACME_EMAIL", ""),
		Secret:                 []byte(env("AUDIORA_SECRET", "")),
		BootstrapAdminEmail:    env("ADMIN_EMAIL", ""),
		BootstrapAdminPassword: env("ADMIN_PASSWORD", ""),
		BootstrapAdminName:     env("ADMIN_NAME", "Admin"),
	}

	origins := env("CORS_ORIGINS", "")
	for _, o := range strings.Split(origins, ",") {
		if o = strings.TrimSpace(o); o != "" {
			c.CORSOrigins = append(c.CORSOrigins, o)
		}
	}

	ttlMin, err := envInt("ACCESS_TOKEN_TTL_MIN", 15)
	if err != nil {
		return nil, err
	}
	c.AccessTokenTTL = time.Duration(ttlMin) * time.Minute
	c.RefreshTokenTTL = 30 * 24 * time.Hour

	scanMin, err := envInt("SCAN_INTERVAL_MIN", 30)
	if err != nil {
		return nil, err
	}
	if scanMin > 0 {
		c.ScanInterval = time.Duration(scanMin) * time.Minute
	}

	if c.ExtractCoverArt, err = envBool("EXTRACT_COVER_ART", true); err != nil {
		return nil, err
	}
	if c.ExtractColors, err = envBool("EXTRACT_COLORS", true); err != nil {
		return nil, err
	}
	if c.PrewarmTranscode, err = envBool("PREWARM_TRANSCODE", false); err != nil {
		return nil, err
	}

	c.DatabasePath = c.DataPath + "/audiora.db"
	c.CoverArtDir = c.DataPath + "/covers"
	c.TranscodeCacheDir = c.DataPath + "/transcode"

	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) validate() error {
	if len(c.Secret) < 32 {
		return fmt.Errorf("AUDIORA_SECRET must be at least 32 characters (got %d); generate one with `openssl rand -base64 48`", len(c.Secret))
	}
	if c.MusicPath == "" {
		return fmt.Errorf("MUSIC_PATH must be set")
	}
	if c.Domain == "" {
		return fmt.Errorf("AUDIORA_DOMAIN must be set so Caddy can request a certificate")
	}
	return nil
}

// EnsureDirs creates the data directory tree. Safe to call on every boot.
func (c *Config) EnsureDirs() error {
	for _, dir := range []string{c.DataPath, c.CoverArtDir, c.TranscodeCacheDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return nil
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) (int, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return fallback, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number: %w", key, err)
	}
	return v, nil
}

func envBool(key string, fallback bool) (bool, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return fallback, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false: %w", key, err)
	}
	return v, nil
}
