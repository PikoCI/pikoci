package migrations

var V52JobOrder = Migration{
	Name: "JobOrder",
	SQL:  `ALTER TABLE jobs ADD COLUMN ` + "`order`" + ` INT NOT NULL DEFAULT 0;`,
}
