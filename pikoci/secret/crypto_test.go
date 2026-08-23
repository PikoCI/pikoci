package secret_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pikoci/pikoci/pikoci/secret"
)

// keyStore is a minimal in-memory stand-in for the parts of secret.Repository
// that Cipher actually uses.
type keyStore struct {
	key    *secret.ServerKey
	writes int
}

func (k *keyStore) FindServerKey(ctx context.Context) (*secret.ServerKey, error) {
	return k.key, nil
}

func (k *keyStore) CreateServerKey(ctx context.Context, sk secret.ServerKey) error {
	k.writes++
	if k.key != nil {
		return errors.New("server key already exists")
	}
	k.key = &sk
	return nil
}

func (k *keyStore) UpsertTeam(context.Context, string, secret.Entry, []byte) (uint32, error) {
	return 0, nil
}
func (k *keyStore) UpsertPipeline(context.Context, string, string, secret.Entry, []byte) (uint32, error) {
	return 0, nil
}
func (k *keyStore) FilterTeam(context.Context, string) ([]*secret.Entry, error) { return nil, nil }
func (k *keyStore) FilterPipeline(context.Context, string, string) ([]*secret.Entry, error) {
	return nil, nil
}
func (k *keyStore) DeleteTeam(context.Context, string, string) error             { return nil }
func (k *keyStore) DeletePipeline(context.Context, string, string, string) error { return nil }
func (k *keyStore) StoredValues(context.Context, string, string) (map[string]secret.StoredValue, error) {
	return nil, nil
}

func TestCipherRoundTrip(t *testing.T) {
	ctx := context.Background()
	ks := &keyStore{}
	c := secret.NewCipher("master-passphrase", ks)

	require.True(t, c.Configured())

	ciphertext, err := c.Encrypt(ctx, "ghp_supersecret")
	require.NoError(t, err)
	assert.NotContains(t, string(ciphertext), "ghp_supersecret", "plaintext must not survive in the ciphertext")

	plaintext, err := c.Decrypt(ctx, ciphertext)
	require.NoError(t, err)
	assert.Equal(t, "ghp_supersecret", plaintext)

	assert.Equal(t, 1, ks.writes, "identity should be generated exactly once")
	assert.NotEmpty(t, ks.key.Recipient)
}

func TestCipherNotConfigured(t *testing.T) {
	ctx := context.Background()
	c := secret.NewCipher("", &keyStore{})

	assert.False(t, c.Configured())

	_, err := c.Encrypt(ctx, "value")
	assert.ErrorIs(t, err, secret.ErrNotConfigured)

	_, err = c.Decrypt(ctx, []byte("whatever"))
	assert.ErrorIs(t, err, secret.ErrNotConfigured)
}

// A restarted server with the same master key must recover the same identity,
// so previously stored values remain readable.
func TestCipherSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	ks := &keyStore{}

	ciphertext, err := secret.NewCipher("master-passphrase", ks).Encrypt(ctx, "value")
	require.NoError(t, err)

	plaintext, err := secret.NewCipher("master-passphrase", ks).Decrypt(ctx, ciphertext)
	require.NoError(t, err)
	assert.Equal(t, "value", plaintext)
	assert.Equal(t, 1, ks.writes, "restart must reuse the stored identity, not generate a new one")
}

// The critical safety property: a changed master key must fail loudly and
// leave the stored identity untouched, rather than silently regenerating and
// orphaning every stored value.
func TestCipherWrongKeyDoesNotRegenerate(t *testing.T) {
	ctx := context.Background()
	ks := &keyStore{}

	_, err := secret.NewCipher("right-passphrase", ks).Encrypt(ctx, "value")
	require.NoError(t, err)
	original := ks.key.Recipient

	_, err = secret.NewCipher("wrong-passphrase", ks).Encrypt(ctx, "value")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PIKOCI_SECRET_KEY may have changed")

	assert.Equal(t, original, ks.key.Recipient, "stored identity must be left intact")
	assert.Equal(t, 1, ks.writes, "must not write a replacement identity")
}
