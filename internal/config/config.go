package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const DefaultVersion = "v2.0.0"

// Config holds GoLab v2 Control-Plane Daemon and CLI configuration.
type Config struct {
	Addr         string `json:"addr"`
	DBPath       string `json:"db_path"`
	ProfilesRoot string `json:"profiles_root"`
	Token        string `json:"token,omitempty"`
	OpenBrowser  bool   `json:"open_browser"`
	Headless     bool   `json:"headless"`
	ChromeBinary string `json:"chrome_binary,omitempty"`
	Version      string `json:"version"`
}

// DefaultHomeDir returns ~/.golab (or GOLAB_HOME if overridden).
func DefaultHomeDir() string {
	if h := strings.TrimSpace(os.Getenv("GOLAB_HOME")); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".golab"
	}
	return filepath.Join(home, ".golab")
}

// Load reads environment defaults for GoLab v2.
func Load() *Config {
	baseDir := DefaultHomeDir()
	cfg := &Config{
		Addr:         "127.0.0.1:9120",
		DBPath:       filepath.Join(baseDir, "state.db"),
		ProfilesRoot: filepath.Join(baseDir, "profiles"),
		Version:      DefaultVersion,
	}

	if v := strings.TrimSpace(os.Getenv("GOLAB_ADDR")); v != "" {
		cfg.Addr = v
	} else if v := strings.TrimSpace(os.Getenv("GOLAB_UI_PORT")); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			cfg.Addr = fmt.Sprintf("127.0.0.1:%d", p)
		}
	}
	if v := strings.TrimSpace(os.Getenv("GOLAB_DB_PATH")); v != "" {
		cfg.DBPath = v
	} else if v := strings.TrimSpace(os.Getenv("GOLAB_DB")); v != "" {
		cfg.DBPath = v
	}
	if v := strings.TrimSpace(os.Getenv("GOLAB_SESSION_TOKEN")); v != "" {
		cfg.Token = v
	}
	if v := strings.TrimSpace(os.Getenv("CHROME_PATH")); v != "" {
		cfg.ChromeBinary = v
	}
	return cfg
}
