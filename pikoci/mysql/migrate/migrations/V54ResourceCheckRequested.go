package migrations

// V54ResourceCheckRequested flags resources whose check was asked for by a
// webhook or the trigger endpoint, so FilterDueResources can hand them out
// ahead of the overdue backlog instead of behind it.
var V54ResourceCheckRequested = Migration{
	Name: "ResourceCheckRequested",
	SQL:  `ALTER TABLE resources ADD COLUMN check_requested BOOLEAN NOT NULL DEFAULT FALSE;`,
}
