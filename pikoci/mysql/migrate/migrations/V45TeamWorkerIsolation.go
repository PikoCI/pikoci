package migrations

var V45TeamWorkerIsolation = Migration{
	Name: "TeamWorkerIsolation",
	SQL: `ALTER TABLE teams ADD COLUMN worker_token_salt VARCHAR(36) NOT NULL DEFAULT '';
ALTER TABLE workers ADD COLUMN team_canonical VARCHAR(255) NOT NULL DEFAULT '';`,
}
