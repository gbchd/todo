package config

import (
	"cmp"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

const (
	// DefaultHostAddr binds `todo host` to loopback. Exposing the task API to
	// a network is therefore always a deliberate edit, never something a
	// default did on the operator's behalf.
	DefaultHostAddr = "127.0.0.1:8090"

	hostFileName = "host.toml"
)

// HostConfig is `todo host`'s persisted settings, kept in its own file so a
// machine that is both a host and a client keeps the two concerns apart: the
// database the host owns is not necessarily the one the local client reads.
//
// The file is written by the todo host subcommands rather than hand-edited,
// and carries owner-only permissions because Clients will hold credential
// material.
type HostConfig struct {
	ListenAddr string       `toml:"listen_addr"`
	DBPath     string       `toml:"db_path"`
	Clients    []HostClient `toml:"clients"`
}

// HostClient is one registered device credential. No command issues one yet —
// pairing is separate work — but the entry exists so the host has a place to
// record devices and Validate has something to count. SecretHash is a hash,
// never a usable secret: reading this file must not yield working credentials.
type HostClient struct {
	ID         string `toml:"id"`
	Name       string `toml:"name"`
	SecretHash string `toml:"secret_hash"`
	CreatedAt  string `toml:"created_at"`
}

// LoadHost reads host.toml from ~/.todo, creating it with defaults on first
// run.
func LoadHost() (HostConfig, error) {
	dir, err := Dir()
	if err != nil {
		return HostConfig{}, err
	}
	return LoadHostFrom(dir)
}

// LoadHostFrom reads host.toml from dir, creating it (and dir) with defaults
// if missing. Split from LoadHost so tests can point it at a temp directory.
func LoadHostFrom(dir string) (HostConfig, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return HostConfig{}, fmt.Errorf("create %s: %w", dir, err)
	}
	path := filepath.Join(dir, hostFileName)

	if _, err := os.Stat(path); os.IsNotExist(err) {
		cfg := HostConfig{
			ListenAddr: DefaultHostAddr,
			DBPath:     filepath.Join(dir, dbName),
		}
		if err := save(path, cfg); err != nil {
			return HostConfig{}, err
		}
		return cfg, nil
	}

	var cfg HostConfig
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return HostConfig{}, fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.ListenAddr = cmp.Or(cfg.ListenAddr, DefaultHostAddr)
	cfg.DBPath = cmp.Or(cfg.DBPath, filepath.Join(dir, dbName))
	return cfg, nil
}

// SaveHostTo writes cfg to host.toml in dir with owner-only permissions.
func SaveHostTo(dir string, cfg HostConfig) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	return save(filepath.Join(dir, hostFileName), cfg)
}

// Validate reports why the host must not start with these settings. A
// non-loopback listen address with no device credentials registered is an open
// door: anyone who can reach the address could read and write the task list,
// so it is refused rather than served. Registering a credential is what lifts
// the restriction.
func (c HostConfig) Validate() error {
	loopback, err := loopbackAddr(c.ListenAddr)
	if err != nil {
		return err
	}
	if loopback || len(c.Clients) > 0 {
		return nil
	}
	return fmt.Errorf(
		"refusing to listen on %s: no device credentials are registered, so anyone who can reach that address could read and write your tasks; listen on a loopback address such as %s instead",
		c.ListenAddr, DefaultHostAddr)
}

// loopbackAddr reports whether addr binds only the loopback interface. A bare
// port (":8090") binds every interface, and a hostname we would have to
// resolve to classify is treated as non-loopback: guessing wrong in the other
// direction is what opens the door.
func loopbackAddr(addr string) (bool, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false, fmt.Errorf("parse listen address %q: %w", addr, err)
	}
	if host == "localhost" {
		return true, nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false, nil
	}
	return ip.IsLoopback(), nil
}
