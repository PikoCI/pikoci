package migrations

// V25Cache adds cache columns to resource_types and resources tables.
var V25Cache = Migration{
	Name: "Cache",
	SQL: `
		ALTER TABLE resource_types ADD COLUMN cache BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE resources ADD COLUMN cache BOOLEAN DEFAULT NULL;
	`,
}
