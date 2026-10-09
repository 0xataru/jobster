package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadExample(t *testing.T) {
	cfg, err := Load("../../config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Filter.MaxAge.D() != 7*24*time.Hour {
		t.Errorf("max_age = %v", cfg.Filter.MaxAge.D())
	}
	if cfg.Scoring.Title["=Go"] != 5 || len(cfg.Companies) == 0 {
		t.Errorf("example config not loaded: %+v", cfg.Scoring.Title)
	}
}

func TestDefaultsFillOmittedFields(t *testing.T) {
	cfg := load(t, "scoring: {threshold: 3}\n")
	if cfg.Workers != 4 || cfg.Filter.MaxAge.D() != 7*24*time.Hour || cfg.Scoring.Threshold != 3 {
		t.Errorf("unexpected config: %+v", cfg)
	}
	if !cfg.Sources.RemoteOK.Enabled || cfg.Telegram.TokenEnv != "TELEGRAM_BOT_TOKEN" {
		t.Errorf("defaults missing: %+v", cfg)
	}
}

func TestDuration(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"7d":   7 * 24 * time.Hour,
		"1.5d": 36 * time.Hour,
		"12h":  12 * time.Hour,
	} {
		if got := load(t, "http_timeout: "+in+"\n").HTTPTimeout.D(); got != want {
			t.Errorf("%s = %v, want %v", in, got, want)
		}
	}
}

func TestValidation(t *testing.T) {
	for yaml, wantErr := range map[string]string{
		"workers: 0":           "workers",
		"http_timeout: 7 days": "invalid duration",
		"companies: [{name: X, ats: workday, slug: x}]": "unknown ats",
		"companies: [{name: X, ats: greenhouse}]":       "set together",
	} {
		path := filepath.Join(t.TempDir(), "c.yaml")
		os.WriteFile(path, []byte(yaml), 0o600)
		if _, err := Load(path); err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("%q: err = %v, want %q", yaml, err, wantErr)
		}
	}
}

func load(t *testing.T, yaml string) *Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
