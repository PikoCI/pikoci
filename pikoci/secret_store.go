package pikoci

import (
	"context"
	"errors"
	"fmt"

	"github.com/pikoci/pikoci/pikoci/auditlog"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/secret"
	"github.com/pikoci/pikoci/pikoci/sectype"
	"github.com/pikoci/pikoci/pikoci/unitwork"
)

// EnableSecretStore wires up the secret store. An empty masterKey
// leaves the store fully usable for plain entries but unconfigured for
// secrets, which then fail with secret.ErrNotConfigured instead of silently
// storing plaintext.
//
// This is deliberately post-construction rather than a New parameter: the
// store is optional, so `pikoci run` and the pipeline editor never need it.
func (q *PikoCI) EnableSecretStore(sr secret.Repository, masterKey string) {
	q.Secrets = sr
	q.Cipher = secret.NewCipher(masterKey, sr)
}

// ErrSecretStoreUnavailable reports that this server has no secret store
// wired up, so nothing can be stored or read.
var ErrSecretStoreUnavailable = errors.New("secret storage is not available on this server")

// ErrSecretInvalidRequest reports a caller mistake rather than a server-side
// failure: a name that is not a valid identifier, or an unknown kind.
var ErrSecretInvalidRequest = errors.New("invalid secret request")

// secretStoreReady reports whether the store was wired up at all.
func (q *PikoCI) secretStoreReady() error {
	if q.Secrets == nil || q.Cipher == nil {
		return ErrSecretStoreUnavailable
	}
	return nil
}

// validateSecretName constrains entry names to the shape of an environment
// variable, reusing the same character-class rule pipeline input names use
// (validInputName, in helper.go) plus the VARCHAR(255) bound the secrets
// tables enforce. Names are used verbatim as the lookup key from a pipeline's
// secret block, so unlike other entities they are not slugified: turning
// GITHUB_TOKEN into github-token would break `key = "GITHUB_TOKEN"`.
func validateSecretName(name string) error {
	if !validInputName.MatchString(name) || len(name) > 255 {
		return fmt.Errorf("%w: name %q must start with a letter or underscore and contain only letters, digits and underscores", ErrSecretInvalidRequest, name)
	}
	return nil
}

// prepareEntry validates the request and produces the entry plus the bytes to
// store: ciphertext for a secret, the value itself for a plain entry.
func (q *PikoCI) prepareEntry(ctx context.Context, name, value string, kind secret.Kind, scope secret.Scope) (secret.Entry, []byte, error) {
	if err := q.secretStoreReady(); err != nil {
		return secret.Entry{}, nil, err
	}
	if !kind.Valid() {
		return secret.Entry{}, nil, fmt.Errorf("%w: kind %q must be %q or %q", ErrSecretInvalidRequest, kind, secret.KindSecret, secret.KindPlain)
	}
	if err := validateSecretName(name); err != nil {
		return secret.Entry{}, nil, err
	}

	e := secret.Entry{Name: name, Canonical: name, Scope: scope, Kind: kind}

	// Plain entries never touch the cipher, so they work on a server with no
	// master key configured at all.
	if kind == secret.KindPlain {
		return e, []byte(value), nil
	}

	ciphertext, err := q.Cipher.Encrypt(ctx, value)
	if err != nil {
		return secret.Entry{}, nil, err
	}

	return e, ciphertext, nil
}

// ErrSecretEntryNotFound reports that no entry by that name is stored in the
// scope that was asked about.
var ErrSecretEntryNotFound = errors.New("secret entry not found")

// auditCreated returns the audit action for storing an entry of this kind.
func auditCreated(kind secret.Kind) auditlog.Action {
	if kind == secret.KindPlain {
		return auditlog.PlainCreated
	}
	return auditlog.SecretCreated
}

// auditDeleted returns the audit action for removing an entry of this kind.
func auditDeleted(kind secret.Kind) auditlog.Action {
	if kind == secret.KindPlain {
		return auditlog.PlainDeleted
	}
	return auditlog.SecretDeleted
}

// entryKind finds the kind of the named entry among entries. A delete has to
// ask before removing the row, because afterwards there is nothing left to
// tell a secret from a plain value.
func entryKind(entries []*secret.Entry, name string) (secret.Kind, error) {
	for _, e := range entries {
		if e.Canonical == name {
			return e.Kind, nil
		}
	}
	return "", fmt.Errorf("%q: %w", name, ErrSecretEntryNotFound)
}

// checkKindUnchanged rejects a set that would change the kind of an entry that
// already exists. A set is a replace, which is how a value is updated without
// a window in which the entry is absent; the storage layer would happily
// overwrite the kind along with it, silently turning a secret into a plain
// value that is then printed in build logs.
//
// Switching kind deliberately stays a delete followed by a create.
func checkKindUnchanged(entries []*secret.Entry, name string, kind secret.Kind) error {
	stored, err := entryKind(entries, name)
	if err != nil {
		// Nothing stored under that name yet, so there is no kind to keep.
		return nil
	}
	if stored == kind {
		return nil
	}
	return fmt.Errorf("%w: %q is already stored as %s; delete it before storing it as %s", ErrSecretInvalidRequest, name, stored, kind)
}

// wrapStoreErr translates a scope-not-found failure from the storage layer
// into ErrSecretEntryNotFound, matching the sentinel the HTTP layer maps to
// 404 rather than 500. Any other storage failure is wrapped plainly.
func wrapStoreErr(name string, err error) error {
	if errors.Is(err, secret.ErrScopeNotFound) {
		return fmt.Errorf("%w: %v", ErrSecretEntryNotFound, err)
	}
	return fmt.Errorf("failed to store %q: %w", name, err)
}

// SetTeamSecret stores a team-scoped entry, replacing the value of any
// existing entry with the same name.
func (q *PikoCI) SetTeamSecret(ctx context.Context, tc, name, value string, kind secret.Kind) error {
	// Outside the transaction on purpose: encryption is expensive, and the
	// cipher reads the server identity through its own repository, so it would
	// escape the transaction anyway.
	e, data, err := q.prepareEntry(ctx, name, value, kind, secret.TeamScope)
	if err != nil {
		return err
	}

	err = q.StartUoW(ctx, func(uow unitwork.UnitOfWork) error {
		entries, err := uow.Secrets().FilterTeam(ctx, tc)
		if err != nil {
			return err
		}
		if err := checkKindUnchanged(entries, e.Canonical, kind); err != nil {
			return err
		}

		if _, err := uow.Secrets().UpsertTeam(ctx, tc, e, data); err != nil {
			return wrapStoreErr(name, err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	q.audit(ctx, tc, auditCreated(kind), "secret", name, nil)

	return nil
}

// SetPipelineSecret stores a pipeline-scoped entry, replacing the value of any
// existing entry with the same name.
func (q *PikoCI) SetPipelineSecret(ctx context.Context, tc, pn, name, value string, kind secret.Kind) error {
	e, data, err := q.prepareEntry(ctx, name, value, kind, secret.PipelineScope)
	if err != nil {
		return err
	}

	err = q.StartUoW(ctx, func(uow unitwork.UnitOfWork) error {
		entries, err := uow.Secrets().FilterPipeline(ctx, tc, pn)
		if err != nil {
			return err
		}
		if err := checkKindUnchanged(entries, e.Canonical, kind); err != nil {
			return err
		}

		if _, err := uow.Secrets().UpsertPipeline(ctx, tc, pn, e, data); err != nil {
			return wrapStoreErr(name, err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	q.audit(ctx, tc, auditCreated(kind), "secret", name, map[string]interface{}{"pipeline": pn})

	return nil
}

// ListTeamSecrets returns the team-scoped entries. Plain entries carry their
// value; secret values are never returned.
func (q *PikoCI) ListTeamSecrets(ctx context.Context, tc string) ([]*secret.Entry, error) {
	if err := q.secretStoreReady(); err != nil {
		return nil, err
	}
	return q.Secrets.FilterTeam(ctx, tc)
}

// ListPipelineSecrets returns the pipeline-scoped entries. Plain entries carry
// their value; secret values are never returned.
func (q *PikoCI) ListPipelineSecrets(ctx context.Context, tc, pn string) ([]*secret.Entry, error) {
	if err := q.secretStoreReady(); err != nil {
		return nil, err
	}
	return q.Secrets.FilterPipeline(ctx, tc, pn)
}

// DeleteTeamSecret removes a team-scoped entry. It does not need the master
// key, so an entry can still be cleaned up after the key is lost.
func (q *PikoCI) DeleteTeamSecret(ctx context.Context, tc, name string) error {
	if err := q.secretStoreReady(); err != nil {
		return err
	}

	// The kind is read inside the transaction because it decides which audit
	// action is written, and the row is gone once the delete lands.
	var kind secret.Kind
	err := q.StartUoW(ctx, func(uow unitwork.UnitOfWork) error {
		entries, err := uow.Secrets().FilterTeam(ctx, tc)
		if err != nil {
			return err
		}
		kind, err = entryKind(entries, name)
		if err != nil {
			return err
		}

		return uow.Secrets().DeleteTeam(ctx, tc, name)
	})
	if err != nil {
		return err
	}

	q.audit(ctx, tc, auditDeleted(kind), "secret", name, nil)

	return nil
}

// DeletePipelineSecret removes a pipeline-scoped entry.
func (q *PikoCI) DeletePipelineSecret(ctx context.Context, tc, pn, name string) error {
	if err := q.secretStoreReady(); err != nil {
		return err
	}

	var kind secret.Kind
	err := q.StartUoW(ctx, func(uow unitwork.UnitOfWork) error {
		entries, err := uow.Secrets().FilterPipeline(ctx, tc, pn)
		if err != nil {
			return err
		}
		kind, err = entryKind(entries, name)
		if err != nil {
			return err
		}

		return uow.Secrets().DeletePipeline(ctx, tc, pn, name)
	})
	if err != nil {
		return err
	}

	q.audit(ctx, tc, auditDeleted(kind), "secret", name, map[string]interface{}{"pipeline": pn})

	return nil
}

// ResolvePipelineValues returns the values a pipeline may use, keyed by name,
// along with which of them are plain and so must not be masked in build logs.
//
// Only names the pipeline actually references through a `secret "pikoci"`
// block are returned, so a compromised worker cannot enumerate everything the
// team has stored.
func (q *PikoCI) ResolvePipelineValues(ctx context.Context, tc, pn string) (*secret.Resolved, error) {
	if err := q.secretStoreReady(); err != nil {
		return nil, err
	}

	pp, err := q.GetPipeline(ctx, tc, pn)
	if err != nil {
		return nil, fmt.Errorf("failed to find pipeline %q: %w", pn, err)
	}

	resolved := &secret.Resolved{
		Values: map[string]string{},
		Plain:  map[string]bool{},
	}

	wanted, err := referencedStoreKeys(pp)
	if err != nil {
		return nil, err
	}
	if len(wanted) == 0 {
		return resolved, nil
	}

	stored, err := q.Secrets.StoredValues(ctx, tc, pn)
	if err != nil {
		return nil, fmt.Errorf("failed to load stored secrets: %w", err)
	}

	for name := range wanted {
		sv, ok := stored[name]
		if !ok {
			// Left absent rather than erroring: the worker reports the missing
			// key against the variable that wanted it, which is a better message.
			continue
		}

		// Plain entries resolve without the cipher, so a pipeline using only
		// plain values work on a server with no master key.
		if sv.Kind == secret.KindPlain {
			resolved.Values[name] = string(sv.Data)
			resolved.Plain[name] = true
			continue
		}

		plaintext, err := q.Cipher.Decrypt(ctx, sv.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt %q: %w", name, err)
		}
		resolved.Values[name] = plaintext
	}

	return resolved, nil
}

// referencedStoreKeys returns the names a pipeline requests from the store.
func referencedStoreKeys(pp *pipeline.Pipeline) (map[string]struct{}, error) {
	wanted := make(map[string]struct{})

	secretVars := pp.SecretVars
	if len(secretVars) == 0 && len(pp.Raw) > 0 {
		// SecretVars are not persisted, so recover them from the raw HCL the
		// same way the worker does. A parse failure has to surface here:
		// swallowing it leaves the pipeline looking like it references
		// nothing, and the build fails later reporting every key as missing.
		parsed, err := pipeline.ParseSecretVarsFromRaw(pp.Raw, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to parse the pipeline configuration: %w", err)
		}
		secretVars = parsed
	}

	for _, sv := range secretVars {
		if sv.Type == sectype.StoreName {
			wanted[sv.Key] = struct{}{}
		}
	}

	return wanted, nil
}
