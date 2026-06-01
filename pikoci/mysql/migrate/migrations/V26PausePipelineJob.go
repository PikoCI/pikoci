package migrations

var V26PausePipelineJob = Migration{
	Name: "PausePipelineJob",
	SQL: `
		ALTER TABLE jobs ADD COLUMN paused BOOLEAN NOT NULL DEFAULT FALSE;
	`,
}
