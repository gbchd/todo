// Package config loads and persists ~/.todo/config.toml, the home for
// settings not passed per-invocation as flags.
package config

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

const (
	DefaultTUILayout = "list"
	DefaultWebPort   = 8080

	fileName = "config.toml"
	dbName   = "todo.db"
)

// Config is the persisted settings file. Flag values take precedence over
// these at the call site; these take precedence over the package defaults.
type Config struct {
	DBPath    string `toml:"db_path"`
	TUILayout string `toml:"tui_layout"`
	WebPort   int    `toml:"web_port"`
}

// Dir returns the app's config/data directory (~/.todo), creating it if
// it doesn't exist.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(home, ".todo"), nil
}

// Load reads config.toml from ~/.todo, creating it with defaults on first
// run.
func Load() (Config, error) {
	dir, err := Dir()
	if err != nil {
		return Config{}, err
	}
	return LoadFrom(dir)
}

// LoadFrom reads config.toml from dir, creating it (and dir) with defaults
// if missing. Split from Load so tests can point it at a temp directory.
func LoadFrom(dir string) (Config, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Config{}, fmt.Errorf("create %s: %w", dir, err)
	}
	path := filepath.Join(dir, fileName)

	if _, err := os.Stat(path); os.IsNotExist(err) {
		cfg := Config{
			DBPath:    filepath.Join(dir, dbName),
			TUILayout: DefaultTUILayout,
			WebPort:   DefaultWebPort,
		}
		if err := save(path, cfg); err != nil {
			return Config{}, err
		}
		return cfg, nil
	}

	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.TUILayout = cmp.Or(cfg.TUILayout, DefaultTUILayout)
	cfg.WebPort = cmp.Or(cfg.WebPort, DefaultWebPort)
	cfg.DBPath = cmp.Or(cfg.DBPath, filepath.Join(dir, dbName))
	return cfg, nil
}

// save writes cfg as TOML with owner-only permissions. It takes any so both
// config.toml and host.toml go through one writer.
func save(path string, cfg any) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()
	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
