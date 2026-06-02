package migrations

var V28JobTimeout = Migration{
	Name: "JobTimeout",
	SQL:  `ALTER TABLE jobs ADD COLUMN timeout BIGINT;`,
}
