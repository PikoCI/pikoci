// Package secret defines the domain model for stored secrets in PikoCI.
// Secrets hold sensitive values (credentials, tokens) encrypted at rest with a
// server-held age identity, managed through the API rather than declared in a
// pipeline definition.
package secret

import "time"

// Scope identifies what a secret is attached to.
type Scope string

const (
	// TeamScope secrets are readable by every pipeline in the team.
	TeamScope Scope = "team"

	// PipelineScope secrets are readable only by the pipeline that owns them.
	// A pipeline secret shadows a team secret with the same canonical name.
	PipelineScope Scope = "pipeline"
)

// Secret is a named value stored encrypted and referenced from a pipeline
// through the built-in "pikoci" secret type.
//
// The plaintext value is deliberately absent from this struct. Values exist
// only transiently inside Cipher during encryption and decryption, so a Secret
// can be logged or serialized to an API response without leaking anything.
type Secret struct {
	ID        uint32    `json:"id"`
	Name      string    `json:"name"`
	Canonical string    `json:"canonical"`
	Scope     Scope     `json:"scope"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
