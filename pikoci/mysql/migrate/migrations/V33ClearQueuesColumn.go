package migrations

var V33ClearQueuesColumn = Migration{
	Name: "ClearQueuesColumn",
	SQL:  `UPDATE workers SET queues = '';`,
}
