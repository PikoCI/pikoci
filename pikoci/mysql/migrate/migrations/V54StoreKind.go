package migrations

// V54StoreKind adds the kind discriminator to the config store, so the same
// tables can hold encrypted secrets and plain (non-sensitive) configuration.
//
// The default backfills existing rows as secrets, which is correct: everything
// written before this migration went through the cipher.
//
// The tables keep their team_secrets/pipeline_secrets names. Renaming them
// would mean rewriting foreign key constraints across four backends for no
// user-visible gain.
var V54StoreKind = Migration{
	Name: "StoreKind",
	SQL: `ALTER TABLE team_secrets ADD COLUMN kind VARCHAR(16) NOT NULL DEFAULT 'secret';
ALTER TABLE pipeline_secrets ADD COLUMN kind VARCHAR(16) NOT NULL DEFAULT 'secret'`,
}
