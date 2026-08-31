package secret

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"filippo.io/age"
)

// ErrNotConfigured is returned by operations that need the master key when it
// is not set. Listing and deleting secrets do not need the key and never
// return this, so an operator who has lost the key can still see and clean up
// what is stored.
var ErrNotConfigured = errors.New("secret storage is not configured: set PIKOCI_SECRET_KEY (or --secret-key) to store and read secrets")

// ServerKey is the server's age identity, wrapped under the master key.
type ServerKey struct {
	// Wrapped is the X25519 identity encrypted with age's scrypt (passphrase)
	// recipient, so the database alone is useless without the master key.
	Wrapped []byte
	// Recipient is the public half, stored in the clear. It is what values are
	// encrypted to, and it is safe to publish.
	Recipient string
}

// Cipher encrypts and decrypts secret values with a server-held age identity.
//
// The identity is created on first use and cached for the process lifetime; it
// is never loaded at startup. A server with no master key configured therefore
// boots normally and only reports a problem when a secret is actually stored
// or read.
type Cipher struct {
	masterKey string
	repo      Repository

	mu       sync.Mutex
	identity *age.X25519Identity
}

// NewCipher returns a Cipher backed by repo. An empty masterKey is valid and
// leaves the Cipher unconfigured: every operation then fails with
// ErrNotConfigured rather than falling back to storing plaintext.
func NewCipher(masterKey string, repo Repository) *Cipher {
	return &Cipher{masterKey: masterKey, repo: repo}
}

// Configured reports whether a master key is available. Callers use it to give
// a clear error before attempting work that is bound to fail.
func (c *Cipher) Configured() bool { return c != nil && c.masterKey != "" }

// Encrypt seals a plaintext value for storage.
func (c *Cipher) Encrypt(ctx context.Context, plaintext string) ([]byte, error) {
	id, err := c.identityFor(ctx)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, id.Recipient())
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt secret: %w", err)
	}
	if _, err := io.WriteString(w, plaintext); err != nil {
		return nil, fmt.Errorf("failed to encrypt secret: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("failed to encrypt secret: %w", err)
	}

	return buf.Bytes(), nil
}

// Decrypt opens a stored value.
func (c *Cipher) Decrypt(ctx context.Context, ciphertext []byte) (string, error) {
	id, err := c.identityFor(ctx)
	if err != nil {
		return "", err
	}

	r, err := age.Decrypt(bytes.NewReader(ciphertext), id)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt secret: %w", err)
	}
	plaintext, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt secret: %w", err)
	}

	return string(plaintext), nil
}

// Recipient returns the public half of the server identity, generating one if
// this is the first use. It is exposed so a future client-side encryption flow
// can fetch it without needing the private key.
func (c *Cipher) Recipient(ctx context.Context) (string, error) {
	id, err := c.identityFor(ctx)
	if err != nil {
		return "", err
	}
	return id.Recipient().String(), nil
}

// identityFor returns the cached identity, loading or creating it on first use.
func (c *Cipher) identityFor(ctx context.Context) (*age.X25519Identity, error) {
	if !c.Configured() {
		return nil, ErrNotConfigured
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.identity != nil {
		return c.identity, nil
	}

	sk, err := c.repo.FindServerKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load the server secret key: %w", err)
	}

	if sk == nil {
		id, err := c.generate(ctx)
		if err != nil {
			return nil, err
		}
		c.identity = id
		return id, nil
	}

	id, err := unwrapIdentity(sk.Wrapped, c.masterKey)
	if err != nil {
		// Deliberately not regenerating: a fresh identity would orphan every
		// value already stored, turning a recoverable misconfiguration into
		// permanent data loss.
		return nil, fmt.Errorf("failed to unwrap the stored secret key, PIKOCI_SECRET_KEY may have changed: %w", err)
	}

	c.identity = id
	return id, nil
}

// generate creates and persists a new identity. If another server won the race
// to create it, the stored one is loaded instead.
func (c *Cipher) generate(ctx context.Context) (*age.X25519Identity, error) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, fmt.Errorf("failed to generate a secret key: %w", err)
	}

	wrapped, err := wrapIdentity(id, c.masterKey)
	if err != nil {
		return nil, err
	}

	err = c.repo.CreateServerKey(ctx, ServerKey{Wrapped: wrapped, Recipient: id.Recipient().String()})
	if err == nil {
		return id, nil
	}

	// Another server may have created the key between our read and write.
	sk, findErr := c.repo.FindServerKey(ctx)
	if findErr != nil || sk == nil {
		return nil, fmt.Errorf("failed to store the generated secret key: %w", err)
	}

	existing, unwrapErr := unwrapIdentity(sk.Wrapped, c.masterKey)
	if unwrapErr != nil {
		return nil, fmt.Errorf("failed to unwrap the stored secret key, PIKOCI_SECRET_KEY may have changed: %w", unwrapErr)
	}

	return existing, nil
}

// wrapIdentity encrypts an identity under the master key using age's scrypt
// passphrase mode, so no key derivation is hand-rolled here.
func wrapIdentity(id *age.X25519Identity, masterKey string) ([]byte, error) {
	r, err := age.NewScryptRecipient(masterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to derive a key from PIKOCI_SECRET_KEY: %w", err)
	}

	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, r)
	if err != nil {
		return nil, fmt.Errorf("failed to wrap the secret key: %w", err)
	}
	if _, err := io.WriteString(w, id.String()); err != nil {
		return nil, fmt.Errorf("failed to wrap the secret key: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("failed to wrap the secret key: %w", err)
	}

	return buf.Bytes(), nil
}

// unwrapIdentity reverses wrapIdentity.
func unwrapIdentity(wrapped []byte, masterKey string) (*age.X25519Identity, error) {
	i, err := age.NewScryptIdentity(masterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to derive a key from PIKOCI_SECRET_KEY: %w", err)
	}

	r, err := age.Decrypt(bytes.NewReader(wrapped), i)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	return age.ParseX25519Identity(strings.TrimSpace(string(data)))
}
