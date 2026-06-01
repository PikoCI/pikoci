package migrations

var V27ResourcePin = Migration{
	Name: "ResourcePin",
	SQL: `
		ALTER TABLE resources ADD COLUMN pinned_version_id INT UNSIGNED;
	`,
}
