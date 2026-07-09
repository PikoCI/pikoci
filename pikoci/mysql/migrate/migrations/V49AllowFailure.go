package migrations

var V49AllowFailure = Migration{
	Name: "AllowFailure",
	SQL:  `ALTER TABLE jobs ADD COLUMN allow_failure BOOLEAN NOT NULL DEFAULT FALSE;`,
}
