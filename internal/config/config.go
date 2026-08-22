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
//
// Backend is last because it encodes as a TOML table, and a table in the
// middle would swallow every scalar written after it.
type Config struct {
	DBPath    string  `toml:"db_path"`
	TUILayout string  `toml:"tui_layout"`
	WebPort   int     `toml:"web_port"`
	Backend   Backend `toml:"backend"`
}

// Backend kinds. Local is the default and means what todo has always meant: the
// tasks are in the SQLite file DBPath names, on this machine.
const (
	BackendLocal  = "local"
	BackendRemote = "remote"
)

// Backend says where this machine'"'"'s tasks actually live. It is durable
// configuration rather than a flag because the answer is a property of the
// machine, not of one invocation.
//
// HostURL and Secret are meaningful only when Kind is BackendRemote. Secret is
// the whole token credential.Issue handed the device during pairing — id and
// secret in one opaque string — so that a device stores one value and cannot
// separate the halves by accident.
//
// The file is written by `todo pair` rather than hand-edited, which is why
// nothing here needs to be guessed at or partially filled in.
type Backend struct {
	Kind    string `toml:"kind"`
	HostURL string `toml:"host_url,omitempty"`
	Secret  string `toml:"secret,omitempty"`
}

// Remote reports whether this machine reads and writes its tasks through a
// host. It is a method rather than a comparison at the call site so that an
// unset Kind — every config file written before backends existed — reads as
// local, which is what those machines have always been.
func (b Backend) Remote() bool {
	return b.Kind == BackendRemote
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
			Backend:   Backend{Kind: BackendLocal},
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
	cfg.Backend.Kind = cmp.Or(cfg.Backend.Kind, BackendLocal)
	return cfg, nil
}

// Save writes cfg to config.toml in ~/.todo.
func Save(cfg Config) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	return SaveTo(dir, cfg)
}

// SaveTo writes cfg to config.toml in dir. The permissions are owner-only for
// the same reason host.toml'"'"'s are: once a backend block is present the file
// holds this device'"'"'s credential.
func SaveTo(dir string, cfg Config) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	return save(filepath.Join(dir, fileName), cfg)
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
