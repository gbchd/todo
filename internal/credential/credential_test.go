package credential

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestIssue_TokenVerifiesAgainstItsCredential(t *testing.T) {
	cred, token, err := Issue()
	require.NoError(t, err)

	id, secret, ok := SplitToken(token)
	require.True(t, ok, "an issued token must split")
	assert.Equal(t, cred.ID, id, "the token's first half names the credential it was issued with")
	assert.True(t, cred.Verify(secret))
}

// The stored record is what ends up in host.toml. Anyone who reads that file
// must come away with nothing they can present as a credential.
func TestIssue_StoresNothingUsableAsASecret(t *testing.T) {
	cred, token, err := Issue()
	require.NoError(t, err)
	_, secret, _ := SplitToken(token)

	assert.NotContains(t, cred.SecretHash, secret, "the secret itself must not be in the stored record")
	assert.False(t, cred.Verify(cred.SecretHash), "the stored hash must not work as the secret")
	assert.False(t, cred.Verify(cred.ID), "the id must not work as the secret")
}

func TestIssue_MintsADifferentCredentialEveryTime(t *testing.T) {
	first, firstToken, err := Issue()
	require.NoError(t, err)
	second, secondToken, err := Issue()
	require.NoError(t, err)

	assert.NotEqual(t, first.ID, second.ID)
	assert.NotEqual(t, firstToken, secondToken)

	_, secondSecret, _ := SplitToken(secondToken)
	assert.False(t, first.Verify(secondSecret), "one device's secret must not open another's credential")
}

func TestVerify_RejectsTheWrongSecret(t *testing.T) {
	cred, _, err := Issue()
	require.NoError(t, err)

	assert.False(t, cred.Verify(""))
	assert.False(t, cred.Verify("not the secret"))
}

// An unknown id runs a full verification against a hash nothing satisfies, so
// that "no such device" is not the answer the host reaches soonest.
func TestAbsent_AlwaysFails(t *testing.T) {
	_, token, err := Issue()
	require.NoError(t, err)
	_, secret, _ := SplitToken(token)

	absent := Absent()
	assert.False(t, absent.Verify(secret), "a real secret must not open a credential nobody holds")
	assert.False(t, absent.Verify(""))
}

// Absent hands back a finished hash, so a caller that builds one while
// assembling its handler has nothing left to pay when the first unknown id
// arrives. A per-installation hash also means one host's absent credential
// says nothing about another's.
func TestAbsent_IsAFinishedHashAndDifferentEveryTime(t *testing.T) {
	first, second := Absent(), Absent()

	require.NotEmpty(t, first.SecretHash, "the hash must be complete before the credential is returned")
	cost, err := bcrypt.Cost([]byte(first.SecretHash))
	require.NoError(t, err, "the hash must be a real bcrypt hash")
	assert.Equal(t, DefaultCost, cost, "an unknown id must cost what a known one costs")
	assert.NotEqual(t, first.SecretHash, second.SecretHash, "no two installations may share it")
}

func TestSplitToken(t *testing.T) {
	tests := []struct {
		name   string
		token  string
		id     string
		secret string
		ok     bool
	}{
		{name: "two halves", token: "abc.def", id: "abc", secret: "def", ok: true},
		{name: "splits on the first separator only", token: "abc.def.ghi", id: "abc", secret: "def.ghi", ok: true},
		{name: "no separator", token: "abcdef"},
		{name: "empty id", token: ".def"},
		{name: "empty secret", token: "abc."},
		{name: "empty", token: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, secret, ok := SplitToken(tc.token)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.id, id)
			assert.Equal(t, tc.secret, secret)
		})
	}
}

// The separator has to be a character neither half can contain, or a token
// would split in the wrong place.
func TestIssue_NeitherHalfContainsTheSeparator(t *testing.T) {
	for range 5 {
		cred, token, err := Issue()
		require.NoError(t, err)
		_, secret, _ := SplitToken(token)
		assert.False(t, strings.Contains(cred.ID, separator), "id %q", cred.ID)
		assert.False(t, strings.Contains(secret, separator), "secret %q", secret)
	}
}
