package migrations

// V55ResourceRetriggerVersion records the version a user asked to re-trigger
// from the UI, so the request survives until a worker claims the resource's
// check and runs the on_trigger hooks and builds for it. 0 means none.
var V55ResourceRetriggerVersion = Migration{
	Name: "ResourceRetriggerVersion",
	SQL:  `ALTER TABLE resources ADD COLUMN retrigger_version_id INTEGER NOT NULL DEFAULT 0;`,
}
