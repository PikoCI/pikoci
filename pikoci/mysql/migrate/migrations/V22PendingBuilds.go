package migrations

// V22PendingBuilds adds version_id and resource_canonical columns to the builds table
// to store trigger context for pending builds.
var V22PendingBuilds = Migration{
	Name: "PendingBuilds",
	SQL: `ALTER TABLE builds ADD COLUMN version_id INTEGER;
ALTER TABLE builds ADD COLUMN resource_canonical VARCHAR(255);`,
}
