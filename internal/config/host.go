package config

import (
	"cmp"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/gbchd/todo/internal/credential"
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

// HostClient is one registered device: the credential the host checks tokens
// against, plus the two things an operator needs to recognise the device
// months later and decide whether to revoke it. SecretHash is a password hash,
// never a usable secret — reading this file must not yield working
// credentials, which is the whole reason the secret is not in it.
//
// CreatedAt is RFC 3339 text rather than a time.Time so the file stays
// readable and its round-trip through TOML stays exact.
type HostClient struct {
	ID         string `toml:"id"`
	Name       string `toml:"name"`
	SecretHash string `toml:"secret_hash"`
	CreatedAt  string `toml:"created_at"`
}

// AddClient registers cred under name and returns the stored record. Name is
// the operator's label for the device and is not required to be unique — two
// laptops may both be called "laptop" — so identity stays the credential id.
//
// This is the seam pairing mounts on: pairing calls credential.Issue, hands
// the record here, and saves the config.
func (c *HostConfig) AddClient(name string, cred credential.Credential) HostClient {
	client := HostClient{
		ID:         cred.ID,
		Name:       name,
		SecretHash: cred.SecretHash,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	c.Clients = append(c.Clients, client)
	return client
}

// RemoveClient revokes one device, named by its id or by its name, and
// returns the record it removed. Every other device is left untouched: losing
// one machine must not cost the others a re-pairing.
//
// A name shared by several devices is refused rather than guessed at, because
// the two plausible guesses — the first match, or all of them — are both
// wrong in a way the operator would only discover later.
func (c *HostConfig) RemoveClient(target string) (HostClient, error) {
	if i := slices.IndexFunc(c.Clients, func(x HostClient) bool { return x.ID == target }); i >= 0 {
		removed := c.Clients[i]
		c.Clients = slices.Delete(c.Clients, i, i+1)
		return removed, nil
	}

	var matches []int
	for i, client := range c.Clients {
		if client.Name == target {
			matches = append(matches, i)
		}
	}
	switch len(matches) {
	case 0:
		return HostClient{}, fmt.Errorf("no device named %q is registered with this host", target)
	case 1:
		i := matches[0]
		removed := c.Clients[i]
		c.Clients = slices.Delete(c.Clients, i, i+1)
		return removed, nil
	default:
		ids := make([]string, 0, len(matches))
		for _, i := range matches {
			ids = append(ids, c.Clients[i].ID)
		}
		return HostClient{}, fmt.Errorf(
			"%d devices are named %q; revoke by id instead, one of: %s",
			len(matches), target, strings.Join(ids, ", "))
	}
}

// Credential returns the credential registered under id, in the shape
// authentication checks a token against. It reports false for an id no device
// holds; the caller is required to answer that indistinguishably from a wrong
// secret.
func (c HostConfig) Credential(id string) (credential.Credential, bool) {
	for _, client := range c.Clients {
		if client.ID == id {
			return credential.Credential{ID: client.ID, SecretHash: client.SecretHash}, true
		}
	}
	return credential.Credential{}, false
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

// SaveHost writes cfg to host.toml in ~/.todo.
func SaveHost(cfg HostConfig) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	return SaveHostTo(dir, cfg)
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
