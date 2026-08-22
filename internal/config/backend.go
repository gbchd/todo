package config

import "fmt"

// SecretEnv is the environment variable that supplies this device's host
// credential, overriding the one in config.toml.
//
// It exists for the machine where the config file itself is the exposure — a
// shared box, a container image, a dotfile repository — so that the file can
// carry the backend without carrying the secret. Only the secret can be
// supplied this way: a host URL in an environment variable would let a
// mistyped export point a command at a different task list without saying so.
const SecretEnv = "TODO_HOST_SECRET"

// LocalFlag is the one-off override that puts a paired device back on its own
// database for a single command. It is named here so the error that recommends
// it and the flag that implements it cannot drift apart.
const LocalFlag = "local"

// Override is what one invocation says about the backend, on top of the
// config file: the environment's secret and the flags the user typed.
type Override struct {
	// Local is the --local flag: use this machine's own database this once,
	// whatever the file says.
	Local bool

	// DBPath and DBPathSet are the --db flag. The flag is tracked separately
	// from its value because a --db that merely repeats the configured default
	// is indistinguishable from an absent one by value alone, and the two mean
	// different things to a device that is paired.
	DBPath    string
	DBPathSet bool

	// Secret is the value of SecretEnv, empty when it is unset.
	Secret string
}

// Selection is where one invocation's tasks live: either a SQLite file on this
// machine, or a host. Exactly one group of fields is meaningful, which Remote
// selects between.
type Selection struct {
	Remote  bool
	HostURL string
	Secret  string
	DBPath  string
}

// SelectBackend resolves where this invocation reads and writes its tasks.
//
// The three sources are consulted in one order, each narrower than the last:
// the config file says what this machine is, the environment may replace the
// secret it uses to say so, and --local overrides the whole decision for one
// command. That is the order of durability — a file outlives a shell, a shell
// outlives a command — and it is why the flag can override the file but the
// file can never override the flag.
//
// Nothing here opens, creates, or migrates anything. A device that switches to
// a host leaves its local database exactly where it was, and --local is how it
// is read again.
func SelectBackend(cfg Config, over Override) (Selection, error) {
	dbPath := cfg.DBPath
	if over.DBPathSet {
		dbPath = over.DBPath
	}

	if over.Local || !cfg.Backend.Remote() {
		return Selection{DBPath: dbPath}, nil
	}

	// A database path against a remote backend is refused rather than ignored:
	// the two answers it could be given — read the file, or read the host —
	// are different task lists, and silently choosing either is how someone
	// ends up editing the wrong one for a week.
	if over.DBPathSet {
		return Selection{}, fmt.Errorf(
			"this device is paired with the todo host at %s, so its tasks are not in a local database and --db has nothing to point at; to work on this machine's own database just this once, run the same command with --%s",
			cfg.Backend.HostURL, LocalFlag)
	}

	if cfg.Backend.HostURL == "" {
		return Selection{}, fmt.Errorf(
			"config.toml selects the remote backend but names no host: run `todo pair <host url> <code>` to pair this device, or run this command with --%s to use this machine's own database",
			LocalFlag)
	}

	// The environment overrides the file, so a machine that keeps its secret
	// out of config.toml still works, and one that has both is told by the
	// narrower of the two.
	secret := cfg.Backend.Secret
	if over.Secret != "" {
		secret = over.Secret
	}
	if secret == "" {
		return Selection{}, fmt.Errorf(
			"this device is configured for the todo host at %s but has no credential: set %s in the environment, or run `todo pair %s <code>` to pair it again",
			cfg.Backend.HostURL, SecretEnv, cfg.Backend.HostURL)
	}

	return Selection{Remote: true, HostURL: cfg.Backend.HostURL, Secret: secret}, nil
}
