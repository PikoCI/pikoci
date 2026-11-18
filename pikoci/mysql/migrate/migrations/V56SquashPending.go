package migrations

// V56SquashPending stores the squash_pending job flag: a new build cancels the
// job's older pending builds, so a backlog collapses to the newest one.
var V56SquashPending = Migration{
	Name: "SquashPending",
	SQL:  `ALTER TABLE jobs ADD COLUMN squash_pending BOOLEAN NOT NULL DEFAULT FALSE;`,
}
