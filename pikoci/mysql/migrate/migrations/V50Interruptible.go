package migrations

var V50Interruptible = Migration{
	Name: "Interruptible",
	SQL:  `ALTER TABLE jobs ADD COLUMN interruptible BOOLEAN NOT NULL DEFAULT FALSE;`,
}
