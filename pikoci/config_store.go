package pikoci

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/pikoci/pikoci/pikoci/auditlog"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/secret"
	"github.com/pikoci/pikoci/pikoci/sectype"
)

// configNameRe constrains entry names to the shape of an environment
// variable. Names are used verbatim as the lookup key from a pipeline's
// secret block, so unlike other entities they are not slugified: turning
// GITHUB_TOKEN into github-token would break `key = "GITHUB_TOKEN"`.
var configNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,254}$`)

// EnableConfigStore wires up the configuration store. An empty masterKey
// leaves the store fully usable for plain entries but unconfigured for
// secrets, which then fail with secret.ErrNotConfigured instead of silently
// storing plaintext.
//
// This is deliberately post-construction rather than a New parameter: the
// store is optional, so `pikoci run` and the pipeline editor never need it.
func (q *PikoCI) EnableConfigStore(sr secret.Repository, masterKey string) {
	q.Config = sr
	q.Cipher = secret.NewCipher(masterKey, sr)
}

// ErrConfigStoreUnavailable reports that this server has no configuration
// store wired up, so nothing can be stored or read.
var ErrConfigStoreUnavailable = errors.New("configuration storage is not available on this server")

// ErrConfigInvalidRequest reports a caller mistake rather than a server-side
// failure: a name that is not a valid identifier, or an unknown kind.
var ErrConfigInvalidRequest = errors.New("invalid configuration request")

// configStoreReady reports whether the store was wired up at all.
func (q *PikoCI) configStoreReady() error {
	if q.Config == nil || q.Cipher == nil {
		return ErrConfigStoreUnavailable
	}
	return nil
}

func validateConfigName(name string) error {
	if !configNameRe.MatchString(name) {
		return fmt.Errorf("%w: name %q must start with a letter or underscore and contain only letters, digits and underscores", ErrConfigInvalidRequest, name)
	}
	return nil
}

// prepareEntry validates the request and produces the entry plus the bytes to
// store: ciphertext for a secret, the value itself for a plain entry.
func (q *PikoCI) prepareEntry(ctx context.Context, name, value string, kind secret.Kind, scope secret.Scope) (secret.Entry, []byte, error) {
	if err := q.configStoreReady(); err != nil {
		return secret.Entry{}, nil, err
	}
	if !kind.Valid() {
		return secret.Entry{}, nil, fmt.Errorf("%w: kind %q must be %q or %q", ErrConfigInvalidRequest, kind, secret.KindSecret, secret.KindPlain)
	}
	if err := validateConfigName(name); err != nil {
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

// ErrConfigEntryNotFound reports that no entry by that name is stored in the
// scope that was asked about.
var ErrConfigEntryNotFound = errors.New("configuration entry not found")

// auditCreated returns the audit action for storing an entry of this kind.
func auditCreated(kind secret.Kind) auditlog.Action {
	if kind == secret.KindPlain {
		return auditlog.ConfigCreated
	}
	return auditlog.SecretCreated
}

// auditDeleted returns the audit action for removing an entry of this kind.
func auditDeleted(kind secret.Kind) auditlog.Action {
	if kind == secret.KindPlain {
		return auditlog.ConfigDeleted
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
	return "", fmt.Errorf("%q: %w", name, ErrConfigEntryNotFound)
}

// SetTeamConfig stores a team-scoped entry, replacing any existing entry with
// the same name.
func (q *PikoCI) SetTeamConfig(ctx context.Context, tc, name, value string, kind secret.Kind) error {
	e, data, err := q.prepareEntry(ctx, name, value, kind, secret.TeamScope)
	if err != nil {
		return err
	}

	if _, err := q.Config.UpsertTeam(ctx, tc, e, data); err != nil {
		return fmt.Errorf("failed to store %q: %w", name, err)
	}

	q.audit(ctx, tc, auditCreated(kind), "config", name, nil)

	return nil
}

// SetPipelineConfig stores a pipeline-scoped entry, replacing any existing
// entry with the same name.
func (q *PikoCI) SetPipelineConfig(ctx context.Context, tc, pn, name, value string, kind secret.Kind) error {
	e, data, err := q.prepareEntry(ctx, name, value, kind, secret.PipelineScope)
	if err != nil {
		return err
	}

	if _, err := q.Config.UpsertPipeline(ctx, tc, pn, e, data); err != nil {
		return fmt.Errorf("failed to store %q: %w", name, err)
	}

	q.audit(ctx, tc, auditCreated(kind), "config", name, map[string]interface{}{"pipeline": pn})

	return nil
}

// ListTeamConfig returns the team-scoped entries. Plain entries carry their
// value; secret values are never returned.
func (q *PikoCI) ListTeamConfig(ctx context.Context, tc string) ([]*secret.Entry, error) {
	if err := q.configStoreReady(); err != nil {
		return nil, err
	}
	return q.Config.FilterTeam(ctx, tc)
}

// ListPipelineConfig returns the pipeline-scoped entries. Plain entries carry
// their value; secret values are never returned.
func (q *PikoCI) ListPipelineConfig(ctx context.Context, tc, pn string) ([]*secret.Entry, error) {
	if err := q.configStoreReady(); err != nil {
		return nil, err
	}
	return q.Config.FilterPipeline(ctx, tc, pn)
}

// DeleteTeamConfig removes a team-scoped entry. It does not need the master
// key, so an entry can still be cleaned up after the key is lost.
func (q *PikoCI) DeleteTeamConfig(ctx context.Context, tc, name string) error {
	if err := q.configStoreReady(); err != nil {
		return err
	}
	entries, err := q.Config.FilterTeam(ctx, tc)
	if err != nil {
		return err
	}
	kind, err := entryKind(entries, name)
	if err != nil {
		return err
	}

	if err := q.Config.DeleteTeam(ctx, tc, name); err != nil {
		return err
	}

	q.audit(ctx, tc, auditDeleted(kind), "config", name, nil)

	return nil
}

// DeletePipelineConfig removes a pipeline-scoped entry.
func (q *PikoCI) DeletePipelineConfig(ctx context.Context, tc, pn, name string) error {
	if err := q.configStoreReady(); err != nil {
		return err
	}
	entries, err := q.Config.FilterPipeline(ctx, tc, pn)
	if err != nil {
		return err
	}
	kind, err := entryKind(entries, name)
	if err != nil {
		return err
	}

	if err := q.Config.DeletePipeline(ctx, tc, pn, name); err != nil {
		return err
	}

	q.audit(ctx, tc, auditDeleted(kind), "config", name, map[string]interface{}{"pipeline": pn})

	return nil
}

// ResolvePipelineValues returns the values a pipeline may use, keyed by name,
// along with which of them are plain and so must not be masked in build logs.
//
// Only names the pipeline actually references through a `secret "pikoci"`
// block are returned, so a compromised worker cannot enumerate everything the
// team has stored.
func (q *PikoCI) ResolvePipelineValues(ctx context.Context, tc, pn string) (*secret.Resolved, error) {
	if err := q.configStoreReady(); err != nil {
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

	stored, err := q.Config.StoredValues(ctx, tc, pn)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	for name := range wanted {
		sv, ok := stored[name]
		if !ok {
			// Left absent rather than erroring: the worker reports the missing
			// key against the variable that wanted it, which is a better message.
			continue
		}

		// Plain entries resolve without the cipher, so a pipeline using only
		// plain configuration works on a server with no master key.
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
