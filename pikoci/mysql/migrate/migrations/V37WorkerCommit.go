package migrations

var V37WorkerCommit = Migration{
	Name: "WorkerCommit",
	SQL:  "ALTER TABLE workers ADD COLUMN `commit` VARCHAR(50) NOT NULL DEFAULT '';",
}
