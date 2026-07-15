package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// clearEnv unsets every variable Load reads, so tests start from a known state.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"CONFIG_FILE", "GOCD_BASE_URL", "LISTEN_ADDR", "TLS_CERT_FILE", "TLS_KEY_FILE",
		"MCP_ENDPOINT_PATH", "GOCD_REQUEST_TIMEOUT", "TOKEN_CACHE_TTL", "TOOLSET", "LOG_LEVEL",
	} {
		t.Setenv(k, "") // registers cleanup; empty is treated as unset by env()
	}
}

func TestLoad_EnvOnly(t *testing.T) {
	clearEnv(t)
	t.Setenv("GOCD_BASE_URL", "https://gocd.example.com")
	t.Setenv("TOOLSET", "actions")
	t.Setenv("GOCD_REQUEST_TIMEOUT", "5s")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.GoCDBaseURL != "https://gocd.example.com" {
		t.Errorf("GoCDBaseURL = %q", c.GoCDBaseURL)
	}
	if c.Toolset != ToolsetActions {
		t.Errorf("Toolset = %q", c.Toolset)
	}
	if c.GoCDTimeout != 5*time.Second {
		t.Errorf("GoCDTimeout = %v", c.GoCDTimeout)
	}
	// Untouched fields keep their defaults.
	if c.ListenAddr != ":8443" {
		t.Errorf("ListenAddr default = %q", c.ListenAddr)
	}
}

func TestLoad_MissingBaseURL(t *testing.T) {
	clearEnv(t)
	if _, err := Load(); err == nil {
		t.Fatal("expected error when GOCD_BASE_URL is not set")
	}
}

func TestLoad_FileOverridesEnv(t *testing.T) {
	clearEnv(t)
	// Env values that the file must override (and one the file omits).
	t.Setenv("GOCD_BASE_URL", "https://from-env")
	t.Setenv("TOOLSET", "readonly")
	t.Setenv("LOG_LEVEL", "warn") // not in file -> must survive

	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := "" +
		"gocd_base_url: https://from-file\n" +
		"toolset: full\n" +
		"token_cache_ttl: 2m\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_FILE", path)

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.GoCDBaseURL != "https://from-file" {
		t.Errorf("file should win: GoCDBaseURL = %q", c.GoCDBaseURL)
	}
	if c.Toolset != ToolsetFull {
		t.Errorf("file should win: Toolset = %q", c.Toolset)
	}
	if c.TokenCacheTTL != 2*time.Minute {
		t.Errorf("TokenCacheTTL = %v", c.TokenCacheTTL)
	}
	if c.LogLevel != "warn" {
		t.Errorf("env value omitted by file should survive: LogLevel = %q", c.LogLevel)
	}
}

func TestLoad_BadDurationInFile(t *testing.T) {
	clearEnv(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("gocd_request_timeout: nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_FILE", path)

	if _, err := Load(); err == nil {
		t.Fatal("expected error for invalid duration in file")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	clearEnv(t)
	t.Setenv("CONFIG_FILE", filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if _, err := Load(); err == nil {
		t.Fatal("expected error for missing CONFIG_FILE")
	}
}
