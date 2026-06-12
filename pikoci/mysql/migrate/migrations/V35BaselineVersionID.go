package migrations

var V35BaselineVersionID = Migration{
	Name: "BaselineVersionID",
	SQL:  `ALTER TABLE jobs ADD COLUMN baseline_version_id INT UNSIGNED;`,
}
