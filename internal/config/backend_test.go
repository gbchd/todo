package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func remoteConfig() Config {
	return Config{
		DBPath:  "/home/me/.todo/todo.db",
		Backend: Backend{Kind: BackendRemote, HostURL: "http://host.local:8090", Secret: "id.from-the-file"},
	}
}

func localConfig() Config {
	return Config{DBPath: "/home/me/.todo/todo.db", Backend: Backend{Kind: BackendLocal}}
}

func TestSelectBackend(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		over Override
		want Selection
	}{
		{
			name: "a local config reads its configured database",
			cfg:  localConfig(),
			want: Selection{DBPath: "/home/me/.todo/todo.db"},
		},
		{
			name: "an unset kind is local, as every config file written before backends existed is",
			cfg:  Config{DBPath: "/home/me/.todo/todo.db"},
			want: Selection{DBPath: "/home/me/.todo/todo.db"},
		},
		{
			name: "--db moves a local device to another file",
			cfg:  localConfig(),
			over: Override{DBPath: "/tmp/other.db", DBPathSet: true},
			want: Selection{DBPath: "/tmp/other.db"},
		},
		{
			name: "a paired device reads the host, with the secret from the file",
			cfg:  remoteConfig(),
			want: Selection{Remote: true, HostURL: "http://host.local:8090", Secret: "id.from-the-file"},
		},
		{
			name: "the environment overrides the file's secret",
			cfg:  remoteConfig(),
			over: Override{Secret: "id.from-the-environment"},
			want: Selection{Remote: true, HostURL: "http://host.local:8090", Secret: "id.from-the-environment"},
		},
		{
			name: "the environment supplies a secret the file does not carry",
			cfg: Config{
				DBPath:  "/home/me/.todo/todo.db",
				Backend: Backend{Kind: BackendRemote, HostURL: "http://host.local:8090"},
			},
			over: Override{Secret: "id.from-the-environment"},
			want: Selection{Remote: true, HostURL: "http://host.local:8090", Secret: "id.from-the-environment"},
		},
		{
			name: "--local overrides the file and reaches this machine's own database",
			cfg:  remoteConfig(),
			over: Override{Local: true},
			want: Selection{DBPath: "/home/me/.todo/todo.db"},
		},
		{
			name: "--local overrides the file and the environment together",
			cfg:  remoteConfig(),
			over: Override{Local: true, Secret: "id.from-the-environment"},
			want: Selection{DBPath: "/home/me/.todo/todo.db"},
		},
		{
			name: "--local takes --db with it",
			cfg:  remoteConfig(),
			over: Override{Local: true, DBPath: "/tmp/old.db", DBPathSet: true},
			want: Selection{DBPath: "/tmp/old.db"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SelectBackend(tc.cfg, tc.over)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestSelectBackend_DBPathAgainstARemoteBackend is the refusal the spec asks
// for by name: not a silently ignored flag, and not a guess between two task
// lists, but an error that says which fix to reach for.
func TestSelectBackend_DBPathAgainstARemoteBackend(t *testing.T) {
	_, err := SelectBackend(remoteConfig(), Override{DBPath: "/tmp/other.db", DBPathSet: true})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "--"+LocalFlag, "the message must name the fix")
	assert.Contains(t, err.Error(), "http://host.local:8090", "the message must name the host it is talking about")
}

func TestSelectBackend_RemoteWithoutACredential(t *testing.T) {
	cfg := remoteConfig()
	cfg.Backend.Secret = ""

	_, err := SelectBackend(cfg, Override{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), SecretEnv, "the message must name the environment variable")
	assert.Contains(t, err.Error(), "todo pair", "the message must name the other way out")
}

func TestSelectBackend_RemoteWithoutAHost(t *testing.T) {
	_, err := SelectBackend(Config{Backend: Backend{Kind: BackendRemote, Secret: "id.secret"}}, Override{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "todo pair")
	assert.Contains(t, err.Error(), "--"+LocalFlag)
}
