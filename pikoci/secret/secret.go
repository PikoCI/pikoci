// Package secret defines the domain model for PikoCI's configuration store.
// The store holds two kinds of entry: secrets, encrypted at rest with a
// server-held age identity, and plain configuration values stored verbatim.
// Both are managed through the API and resolved into pipeline variables at
// build time.
package secret

import "time"

// Scope identifies what an entry is attached to.
type Scope string

const (
	// TeamScope entries are readable by every pipeline in the team.
	TeamScope Scope = "team"

	// PipelineScope entries are readable only by the pipeline that owns them.
	// A pipeline entry shadows a team entry with the same canonical name.
	PipelineScope Scope = "pipeline"
)

// Kind distinguishes sensitive entries from plain configuration.
//
// Kind is fixed at creation. Changing it in place would silently expose a
// value that callers had been told was masked, so switching kind means
// deleting the entry and creating it again.
type Kind string

const (
	// KindSecret is encrypted at rest, masked in build logs, and never
	// returned by the API.
	KindSecret Kind = "secret"

	// KindPlain is stored verbatim, printed in build logs, and readable back.
	// It needs no master key, so a server with no encryption configured can
	// still serve plain configuration.
	KindPlain Kind = "plain"
)

// Valid reports whether k is a recognised kind.
func (k Kind) Valid() bool { return k == KindSecret || k == KindPlain }

// Entry is a named value in the store, referenced from a pipeline through the
// built-in "pikoci" secret type.
//
// Value is populated only for plain entries. Secret values are never carried
// on an Entry, so one can be logged or serialized into an API response without
// leaking anything.
type Entry struct {
	ID        uint32    `json:"id"`
	Name      string    `json:"name"`
	Canonical string    `json:"canonical"`
	Scope     Scope     `json:"scope"`
	Kind      Kind      `json:"kind"`
	Value     string    `json:"value,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// StoredValue is a value exactly as it sits in the database: age ciphertext
// for a secret, plaintext for a plain entry. Callers need the kind to know
// which of the two they are holding.
type StoredValue struct {
	Kind Kind
	Data []byte
}

// Resolved is the set of values a pipeline may use at build time.
type Resolved struct {
	// Values maps entry name to plaintext, for both kinds.
	Values map[string]string
	// Plain lists the names that came from plain entries. The worker must not
	// mask these in build logs, which is the whole point of the kind.
	Plain map[string]bool
}
