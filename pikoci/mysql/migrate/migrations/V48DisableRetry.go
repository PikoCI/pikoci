package migrations

var V48DisableRetry = Migration{
	Name: "DisableRetry",
	SQL:  `ALTER TABLE jobs ADD COLUMN disable_retry BOOLEAN NOT NULL DEFAULT FALSE;`,
}
