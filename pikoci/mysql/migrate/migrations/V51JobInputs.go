package migrations

var V51JobInputs = Migration{
	Name: "JobInputs",
	SQL: `ALTER TABLE jobs ADD COLUMN inputs TEXT NULL;
ALTER TABLE builds ADD COLUMN input_values TEXT NULL;`,
}
