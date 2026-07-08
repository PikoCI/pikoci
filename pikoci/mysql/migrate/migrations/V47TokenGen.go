package migrations

var V47TokenGen = Migration{
	Name: "TokenGen",
	SQL:  `ALTER TABLE users ADD COLUMN token_gen INTEGER NOT NULL DEFAULT 0;`,
}
