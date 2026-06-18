package migrations

var V38RetrySourceBuildID = Migration{
	Name: "RetrySourceBuildID",
	SQL:  "ALTER TABLE builds ADD COLUMN retry_source_build_id INTEGER NOT NULL DEFAULT 0;",
}
