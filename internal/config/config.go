// Package config loads and validates server configuration from the environment.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Toolset controls which group of MCP tools is registered.
type Toolset string

const (
	ToolsetReadOnly Toolset = "readonly" // read-only queries only
	ToolsetActions  Toolset = "actions"  // readonly + safe mutations (trigger/pause/...)
	ToolsetFull     Toolset = "full"     // actions + config editing
)

// Config holds all runtime configuration. Populated from environment variables.
type Config struct {
	GoCDBaseURL     string
	ListenAddr      string
	TLSCertFile     string
	TLSKeyFile      string
	MCPEndpointPath string
	GoCDTimeout     time.Duration
	TokenCacheTTL   time.Duration
	Toolset         Toolset
	LogLevel        string
	LogFile         string
}

// Load builds the configuration from defaults, then environment variables, then —
// if CONFIG_FILE points at a YAML file — that file, which takes priority. Only keys
// actually present in the file override the env/default values. Finally it validates.
func Load() (*Config, error) {
	c := &Config{
		GoCDBaseURL:     env("GOCD_BASE_URL", ""), // required: no sensible default
		ListenAddr:      env("LISTEN_ADDR", ":8443"),
		TLSCertFile:     env("TLS_CERT_FILE", ""),
		TLSKeyFile:      env("TLS_KEY_FILE", ""),
		MCPEndpointPath: env("MCP_ENDPOINT_PATH", "/mcp"),
		GoCDTimeout:     envDuration("GOCD_REQUEST_TIMEOUT", 30*time.Second),
		TokenCacheTTL:   envDuration("TOKEN_CACHE_TTL", 60*time.Second),
		Toolset:         Toolset(env("TOOLSET", string(ToolsetFull))),
		LogLevel:        env("LOG_LEVEL", "info"),
		LogFile:         env("LOG_FILE", ""),
	}
	if path := env("CONFIG_FILE", ""); path != "" {
		if err := c.applyFile(path); err != nil {
			return nil, err
		}
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// fileConfig mirrors Config for YAML decoding. Pointer fields let us tell an absent
// key (leave the env/default value) from one explicitly set to its zero value.
type fileConfig struct {
	GoCDBaseURL     *string `yaml:"gocd_base_url"`
	ListenAddr      *string `yaml:"listen_addr"`
	TLSCertFile     *string `yaml:"tls_cert_file"`
	TLSKeyFile      *string `yaml:"tls_key_file"`
	MCPEndpointPath *string `yaml:"mcp_endpoint_path"`
	GoCDTimeout     *string `yaml:"gocd_request_timeout"`
	TokenCacheTTL   *string `yaml:"token_cache_ttl"`
	Toolset         *string `yaml:"toolset"`
	LogLevel        *string `yaml:"log_level"`
	LogFile         *string `yaml:"log_file"`
}

// applyFile overrides set fields from a YAML config file. The file has priority over env.
func (c *Config) applyFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading CONFIG_FILE %q: %w", path, err)
	}
	var f fileConfig
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parsing CONFIG_FILE %q: %w", path, err)
	}
	if f.GoCDBaseURL != nil {
		c.GoCDBaseURL = *f.GoCDBaseURL
	}
	if f.ListenAddr != nil {
		c.ListenAddr = *f.ListenAddr
	}
	if f.TLSCertFile != nil {
		c.TLSCertFile = *f.TLSCertFile
	}
	if f.TLSKeyFile != nil {
		c.TLSKeyFile = *f.TLSKeyFile
	}
	if f.MCPEndpointPath != nil {
		c.MCPEndpointPath = *f.MCPEndpointPath
	}
	if f.GoCDTimeout != nil {
		d, err := time.ParseDuration(*f.GoCDTimeout)
		if err != nil {
			return fmt.Errorf("gocd_request_timeout: %w", err)
		}
		c.GoCDTimeout = d
	}
	if f.TokenCacheTTL != nil {
		d, err := time.ParseDuration(*f.TokenCacheTTL)
		if err != nil {
			return fmt.Errorf("token_cache_ttl: %w", err)
		}
		c.TokenCacheTTL = d
	}
	if f.Toolset != nil {
		c.Toolset = Toolset(*f.Toolset)
	}
	if f.LogLevel != nil {
		c.LogLevel = *f.LogLevel
	}
	if f.LogFile != nil {
		c.LogFile = *f.LogFile
	}
	return nil
}

// TLSEnabled reports whether both cert and key files are configured.
func (c *Config) TLSEnabled() bool {
	return c.TLSCertFile != "" && c.TLSKeyFile != ""
}

func (c *Config) validate() error {
	if c.GoCDBaseURL == "" {
		return fmt.Errorf("GOCD_BASE_URL is required")
	}
	if !strings.HasPrefix(c.GoCDBaseURL, "http://") && !strings.HasPrefix(c.GoCDBaseURL, "https://") {
		return fmt.Errorf("GOCD_BASE_URL must start with http:// or https://")
	}
	c.GoCDBaseURL = strings.TrimRight(c.GoCDBaseURL, "/")
	if !strings.HasPrefix(c.MCPEndpointPath, "/") {
		return fmt.Errorf("MCP_ENDPOINT_PATH must start with /")
	}
	switch c.Toolset {
	case ToolsetReadOnly, ToolsetActions, ToolsetFull:
	default:
		return fmt.Errorf("TOOLSET must be one of readonly|actions|full, got %q", c.Toolset)
	}
	// TLS files, if provided, must come as a pair.
	if (c.TLSCertFile == "") != (c.TLSKeyFile == "") {
		return fmt.Errorf("TLS_CERT_FILE and TLS_KEY_FILE must be set together")
	}
	return nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
