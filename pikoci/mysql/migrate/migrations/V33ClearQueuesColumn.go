package migrations

var V33ClearQueuesColumn = Migration{
	Name: "DropQueuesColumn",
	SQL:  `ALTER TABLE workers DROP COLUMN queues;`,
}
