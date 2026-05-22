package migrations

// V19OnCancel adds an on_cancel column to the jobs table.
var V19OnCancel = Migration{
	Name: "OnCancel",
	SQL:  `ALTER TABLE jobs ADD COLUMN on_cancel TEXT;`,
}
