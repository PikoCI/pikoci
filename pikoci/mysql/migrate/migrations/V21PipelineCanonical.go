package migrations

var V21PipelineCanonical = Migration{
	Name: "PipelineCanonical",
	SQL: `
		ALTER TABLE pipelines ADD COLUMN canonical VARCHAR(255) NOT NULL DEFAULT '';
		UPDATE pipelines SET canonical = name WHERE canonical = '' OR canonical IS NULL;
		CREATE UNIQUE INDEX uq__canonical__team_id ON pipelines (canonical, team_id);
	`,
}
