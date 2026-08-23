package pikoci

import (
	"context"
	"fmt"
	"regexp"

	"github.com/pikoci/pikoci/pikoci/auditlog"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/secret"
	"github.com/pikoci/pikoci/pikoci/sectype"
)

// secretNameRe constrains secret names to the shape of an environment
// variable. Names are used verbatim as the lookup key from a pipeline's
// secret block, so unlike other entities they are not slugified: turning
// GITHUB_TOKEN into github-token would break `key = "GITHUB_TOKEN"`.
var secretNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,254}$`)

// EnableSecretStore wires up the encrypted secret store. An empty masterKey
// leaves the store present but unconfigured: reads and writes then fail with
// secret.ErrNotConfigured instead of silently storing plaintext.
//
// This is deliberately post-construction rather than a New parameter: the
// store is optional, so `pikoci run` and the pipeline editor never need it.
func (q *PikoCI) EnableSecretStore(sr secret.Repository, masterKey string) {
	q.Secrets = sr
	q.SecretCipher = secret.NewCipher(masterKey, sr)
}

// secretStoreReady reports whether the store was wired up at all.
func (q *PikoCI) secretStoreReady() error {
	if q.Secrets == nil || q.SecretCipher == nil {
		return fmt.Errorf("secret storage is not available on this server")
	}
	return nil
}

func validateSecretName(name string) error {
	if !secretNameRe.MatchString(name) {
		return fmt.Errorf("invalid secret name %q: must start with a letter or underscore and contain only letters, digits and underscores", name)
	}
	return nil
}

// SetTeamSecret stores an encrypted team-scoped secret, replacing any existing
// value with the same name.
func (q *PikoCI) SetTeamSecret(ctx context.Context, tc, name, value string) error {
	if err := q.secretStoreReady(); err != nil {
		return err
	}
	if err := validateSecretName(name); err != nil {
		return err
	}

	ciphertext, err := q.SecretCipher.Encrypt(ctx, value)
	if err != nil {
		return err
	}

	s := secret.Secret{Name: name, Canonical: name, Scope: secret.TeamScope}
	if _, err := q.Secrets.UpsertTeam(ctx, tc, s, ciphertext); err != nil {
		return fmt.Errorf("failed to store secret: %w", err)
	}

	q.audit(ctx, tc, auditlog.SecretCreated, "secret", name, nil)

	return nil
}

// SetPipelineSecret stores an encrypted pipeline-scoped secret, replacing any
// existing value with the same name.
func (q *PikoCI) SetPipelineSecret(ctx context.Context, tc, pn, name, value string) error {
	if err := q.secretStoreReady(); err != nil {
		return err
	}
	if err := validateSecretName(name); err != nil {
		return err
	}

	ciphertext, err := q.SecretCipher.Encrypt(ctx, value)
	if err != nil {
		return err
	}

	s := secret.Secret{Name: name, Canonical: name, Scope: secret.PipelineScope}
	if _, err := q.Secrets.UpsertPipeline(ctx, tc, pn, s, ciphertext); err != nil {
		return fmt.Errorf("failed to store secret: %w", err)
	}

	q.audit(ctx, tc, auditlog.SecretCreated, "secret", name, map[string]interface{}{"pipeline": pn})

	return nil
}

// ListTeamSecrets returns the team-scoped secrets, never their values.
func (q *PikoCI) ListTeamSecrets(ctx context.Context, tc string) ([]*secret.Secret, error) {
	if err := q.secretStoreReady(); err != nil {
		return nil, err
	}
	return q.Secrets.FilterTeam(ctx, tc)
}

// ListPipelineSecrets returns the pipeline-scoped secrets, never their values.
func (q *PikoCI) ListPipelineSecrets(ctx context.Context, tc, pn string) ([]*secret.Secret, error) {
	if err := q.secretStoreReady(); err != nil {
		return nil, err
	}
	return q.Secrets.FilterPipeline(ctx, tc, pn)
}

// DeleteTeamSecret removes a team-scoped secret. It does not need the master
// key, so a secret can still be cleaned up after the key is lost.
func (q *PikoCI) DeleteTeamSecret(ctx context.Context, tc, name string) error {
	if err := q.secretStoreReady(); err != nil {
		return err
	}
	if err := q.Secrets.DeleteTeam(ctx, tc, name); err != nil {
		return err
	}

	q.audit(ctx, tc, auditlog.SecretDeleted, "secret", name, nil)

	return nil
}

// DeletePipelineSecret removes a pipeline-scoped secret.
func (q *PikoCI) DeletePipelineSecret(ctx context.Context, tc, pn, name string) error {
	if err := q.secretStoreReady(); err != nil {
		return err
	}
	if err := q.Secrets.DeletePipeline(ctx, tc, pn, name); err != nil {
		return err
	}

	q.audit(ctx, tc, auditlog.SecretDeleted, "secret", name, map[string]interface{}{"pipeline": pn})

	return nil
}

// ResolvePipelineSecrets returns the decrypted values a pipeline may use,
// keyed by secret name.
//
// Only names the pipeline actually references through a `secret "pikoci"`
// block are returned, so a compromised worker cannot enumerate every secret in
// the team by asking for one build's values.
func (q *PikoCI) ResolvePipelineSecrets(ctx context.Context, tc, pn string) (map[string]string, error) {
	if err := q.secretStoreReady(); err != nil {
		return nil, err
	}

	pp, err := q.GetPipeline(ctx, tc, pn)
	if err != nil {
		return nil, fmt.Errorf("failed to find pipeline %q: %w", pn, err)
	}

	wanted := referencedStoreKeys(pp)
	if len(wanted) == 0 {
		return map[string]string{}, nil
	}

	encrypted, err := q.Secrets.EncryptedValues(ctx, tc, pn)
	if err != nil {
		return nil, fmt.Errorf("failed to load secrets: %w", err)
	}

	values := make(map[string]string, len(wanted))
	for name := range wanted {
		ciphertext, ok := encrypted[name]
		if !ok {
			// Left absent rather than erroring: the worker reports the missing
			// key against the variable that wanted it, which is a better message.
			continue
		}
		plaintext, err := q.SecretCipher.Decrypt(ctx, ciphertext)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt secret %q: %w", name, err)
		}
		values[name] = plaintext
	}

	return values, nil
}

// referencedStoreKeys returns the secret names a pipeline requests from the
// built-in store.
func referencedStoreKeys(pp *pipeline.Pipeline) map[string]struct{} {
	wanted := make(map[string]struct{})

	secretVars := pp.SecretVars
	if len(secretVars) == 0 && len(pp.Raw) > 0 {
		// SecretVars are not persisted, so recover them from the raw HCL the
		// same way the worker does.
		if parsed, err := pipeline.ParseSecretVarsFromRaw(pp.Raw, nil); err == nil {
			secretVars = parsed
		}
	}

	for _, sv := range secretVars {
		if sv.Type == sectype.StoreName {
			wanted[sv.Key] = struct{}{}
		}
	}

	return wanted
}
