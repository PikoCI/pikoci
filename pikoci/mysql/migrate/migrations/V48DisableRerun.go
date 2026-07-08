package migrations

var V48DisableRerun = Migration{
	Name: "DisableRerun",
	SQL:  `ALTER TABLE jobs ADD COLUMN disable_rerun BOOLEAN NOT NULL DEFAULT FALSE;`,
}
